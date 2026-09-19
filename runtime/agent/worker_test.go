package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "worker.py"), []byte("# fixture"), 0600)
	os.WriteFile(filepath.Join(dir, "definition.yml"), []byte("id: fixture\nruntime: python\nentrypoint: worker.py\nunsupported_capability: true\n"), 0600)
	if NewRegistry().LoadAgents(dir) == nil {
		t.Fatal("unknown manifest field silently ignored")
	}
}

func TestRegistryRequiresSchemaIdentityFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "worker.py"), []byte("# fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "definition.yml"), []byte("id: fixture\nruntime: python\nentrypoint: worker.py\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewRegistry().LoadAgents(dir); err == nil || !strings.Contains(err.Error(), "name and version") {
		t.Fatalf("manifest without schema identity fields accepted: %v", err)
	}
}

func TestSerializedTaskFieldsAreDeclaredBySchema(t *testing.T) {
	schemaBytes, err := os.ReadFile("../../schemas/task.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}
	taskBytes, err := json.Marshal(Task{
		ID: "execution|node", AttemptID: "execution/attempt", AgentID: "fixture", ExecutionID: "execution",
		Workflow: "workflow", Origin: "test", Inputs: []TaskInput{{ArtifactID: "input", Version: 1}},
		Parameters: map[string]any{"effort": "high"}, Memory: map[string]any{"fact": true},
		MemoryMetadata: map[string]MemoryReference{"fact": {Scope: memory.ScopeExecution, ScopeID: "execution", Version: 1}},
		Instructions:   []string{"verify"}, Modality: "coding", Capabilities: []Capability{CapabilityWorkspaceRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	var serialized map[string]any
	if err := json.Unmarshal(taskBytes, &serialized); err != nil {
		t.Fatal(err)
	}
	for key := range serialized {
		if _, declared := schema.Properties[key]; !declared {
			t.Errorf("serialized task field %q is absent from task.schema.json", key)
		}
	}
}

func TestLLMStreamFilterDoesNotHideOrdinaryDiagnostics(t *testing.T) {
	if !isLLMStreamLine("  [LLM_STREAM] chunk") {
		t.Fatal("structured stream line was not recognized")
	}
	if isLLMStreamLine("protocol error: unexpected [LLM_STREAM] marker") {
		t.Fatal("ordinary diagnostic containing the marker was hidden")
	}
}

func TestProvisioningEventCarriesOperationAndExecutionIdentity(t *testing.T) {
	bus := events.NewBus("environment")
	defer bus.Close()
	manager := NewEnvironmentManager(&logger.Logger{}, t.TempDir(), bus)
	received := make(chan map[string]any, 1)
	bus.Subscribe("EnvironmentProvisioningStarted", func(event events.RuntimeEvent) { received <- event.Payload.(map[string]any) })
	manager.publishProvisioning("EnvironmentProvisioningStarted", "coder", []string{"fixture==1"}, true, false, 0, nil, provisioningContext{
		executionID: "run", taskID: "run|node", attemptID: "run/attempt",
	})
	select {
	case payload := <-received:
		if payload["execution"] != "run" || payload["task_id"] != "run|node" || payload["operation_id"] != "run/attempt:environment:agent:coder" {
			t.Fatalf("provisioning identity missing: %#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("provisioning event was not delivered")
	}
}

func TestRetriesRequireNoEffectProof(t *testing.T) {
	for _, fixture := range []struct {
		text string
		want bool
	}{
		{"RateLimitError", false},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] RateLimitError", true},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] MidStreamFallbackError: A Timeout Occurred", true},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] 403 Forbidden", true},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] BadRequestError", true},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] command failed", false},
	} {
		if retryableProviderFailure(&WorkerFailure{Reason: WorkerExitedNonZero, Stderr: fixture.text}) != fixture.want {
			t.Fatal("unsafe retry classification")
		}
	}
}

func TestProviderFailureDisposition(t *testing.T) {
	tests := []struct {
		name            string
		stderr          string
		providerPenalty bool
		disableModel    bool
		penalizeFamily  bool
		category        string
	}{
		{"forbidden key", "403 Forbidden", true, false, false, "provider_access"},
		{"wrapped forbidden key", "BadRequestError: request failed with 403 Forbidden", true, false, false, "provider_access"},
		{"authentication", "AuthenticationError: 401 Unauthorized", true, false, false, "provider_access"},
		{"rate limit", "RateLimitError", true, false, false, "provider_transient"},
		{"account quota", "RateLimitError: openrouter_free_tier_daily", true, false, true, "provider_account_quota"},
		{"server error", "InternalServerError: 500 Internal Server Error", true, false, false, "provider_transient"},
		{"timeout", "MidStreamFallbackError: A Timeout Occurred", false, false, false, "timeout"},
		{"bad request", "BadRequestError: unsupported parameter", false, false, false, "model_request"},
		{"missing model", "NotFoundError: 404 Not Found", false, false, false, "model_request"},
		{"tool support", "tool calling is not supported", false, true, false, "model_incompatible"},
		{"harness gate", "403: model is only available on agentic harnesses", false, true, false, "model_incompatible"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := &WorkerFailure{Reason: WorkerExitedNonZero, Stderr: "[RETICLE_RETRY_SAFE: NO_EFFECTS] " + test.stderr}
			got := classifyProviderFailure(failure)
			if !got.retryable || got.penalizeProvider != test.providerPenalty || got.disableModel != test.disableModel || got.penalizeFamily != test.penalizeFamily || got.category != test.category {
				t.Fatalf("unexpected disposition: %#v", got)
			}
		})
	}
}

func TestCancellationBeforeSubmission(t *testing.T) {
	b := events.NewBus("fixture")
	defer b.Close()
	engine := NewGraphEngine(&logger.Logger{}, b)
	engine.Start()
	observed := make(chan struct{})
	b.Subscribe("ExecutionKilled", func(e events.RuntimeEvent) {
		if e.Payload.(map[string]string)["execution"] == "fixture" {
			close(observed)
		}
	})
	b.Publish("ExecutionKilled", "fixture", map[string]string{"execution": "fixture"})
	<-observed
	wf := &WorkflowDefinition{ID: "fixture", Nodes: map[string]WorkflowNode{"node": {ID: "node", WorkerID: "fixture"}}, Roots: []string{"node"}}
	for _, id := range []string{"fixture", "compile-fixture"} {
		if engine.SubmitWorkflow(wf, id) == nil {
			t.Fatal("cancelled execution submitted", id)
		}
	}
}

func TestWorkerChild(t *testing.T) {
	mode := os.Getenv("RETICLE_TEST_CHILD")
	if mode == "" {
		return
	}
	b, _ := io.ReadAll(os.Stdin)
	var req Task
	json.Unmarshal(b, &req)
	switch mode {
	case "produce":
		json.NewEncoder(os.Stdout).Encode(TaskResponse{ID: req.ID, Artifact: &memory.Artifact{ID: "fixture", Name: "fixture", Type: "text/plain", Data: "output"}, Memory: []MemoryMutation{{Key: "shared", Value: "expected"}}})
	case "consume":
		meta := req.MemoryMetadata["shared"]
		if req.Memory["shared"] != "expected" || meta.Scope != memory.ScopeExecution || meta.Version != 1 || len(req.Inputs) != 1 || req.Inputs[0].Data != "output" {
			fmt.Fprintln(os.Stderr, "missing committed input or memory")
			os.Exit(2)
		}
		json.NewEncoder(os.Stdout).Encode(TaskResponse{ID: req.ID, Result: "verified"})
	case "wrong":
		fmt.Println(`{"id":"wrong","result":"x"}`)
	case "large":
		fmt.Print(string(make([]byte, 11*1024*1024)))
	default:
		json.NewEncoder(os.Stdout).Encode(TaskResponse{ID: req.ID, Result: "ok"})
	}
	os.Exit(0)
}

func TestResultCommitBeforeDependentDispatch(t *testing.T) {
	bus := events.NewBus("integration")
	defer bus.Close()
	l := &logger.Logger{}
	state := memory.NewRuntimeState()
	memory.NewManager(memory.NewArtifactStore(), state, memory.NewSessionState("integration"), bus)
	dispatcher := NewDispatcher(l, bus, nil, nil, state)
	dispatcher.Start()
	exe, _ := os.Executable()
	for _, mode := range []string{"produce", "consume"} {
		w := NewWorker(WorkerID(mode), exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=" + mode}, l, bus)
		w.RequiredMemory = []string{"shared"}
		dispatcher.RegisterWorker(w)
	}
	engine := NewGraphEngine(l, bus)
	engine.Start()
	finished := make(chan string, 2)
	bus.Subscribe("WorkflowCompleted", func(events.RuntimeEvent) { finished <- "completed" })
	bus.Subscribe("WorkflowFailed", func(events.RuntimeEvent) { finished <- "failed" })
	wf := &WorkflowDefinition{ID: "fixture", Nodes: map[string]WorkflowNode{"a": {ID: "a", WorkerID: "produce"}, "b": {ID: "b", WorkerID: "consume"}}, Roots: []string{"a"}, Parents: map[string][]string{"b": {"a"}}, Children: map[string][]string{"a": {"b"}}, Edges: []WorkflowEdge{{From: "a", To: "b"}}}
	if err := engine.SubmitWorkflow(wf, "integration"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-finished:
		if result != "completed" {
			t.Fatal("workflow failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workflow did not complete")
	}
}

func TestBuiltinAndExampleManifests(t *testing.T) {
	r := NewRegistry()
	if err := r.LoadAgents("../../cmd/forge/compiler/agents"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []WorkerID{"devops-agent", "ml-agent", "hitl-agent", "rag-agent"} {
		if _, ok := r.Definitions[id]; !ok {
			t.Fatalf("missing agent %s", id)
		}
	}
	for _, dir := range []string{"../../cmd/forge/compiler/workflows", "../../examples/05_advanced_statistics/workflows", "../../examples/06_ml_training_pipeline/workflows"} {
		if err := r.LoadWorkflows(dir); err != nil {
			t.Fatal(err)
		}
	}
}
func TestWorkerContract(t *testing.T) {
	for _, mode := range []string{"optional", "wrong", "large"} {
		t.Run(mode, func(t *testing.T) {
			bus := events.NewBus("test")
			defer bus.Close()
			memory.NewManager(memory.NewArtifactStore(), memory.NewRuntimeState(), memory.NewSessionState("test"), bus)
			exe, _ := os.Executable()
			w := NewWorker("test", exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=" + mode}, &logger.Logger{}, bus)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, failure := w.Execute(ctx, Task{ID: "test|node", ExecutionID: "test"})
			if mode == "optional" {
				if failure != nil || result.Result != "ok" {
					t.Fatalf("EOF/optional response: %v", failure)
				}
			} else if failure == nil {
				t.Fatal("invalid output accepted")
			}
			if ctx.Err() != nil {
				t.Fatal("pipe handling hung")
			}
		})
	}
}
func TestExecutionDefinitionIsolation(t *testing.T) {
	wf := &WorkflowDefinition{Nodes: map[string]WorkflowNode{"a": {ID: "a", Parameters: map[string]any{"effort": "high"}}}}
	a, b := NewWorkflowExecution("a", wf), NewWorkflowExecution("b", wf)
	a.Workflow.Nodes["a"].Parameters["effort"] = "low"
	if b.Workflow.Nodes["a"].Parameters["effort"] != "high" {
		t.Fatal("shared mutable parameters")
	}
}

func TestPersistentExecutionRecoveryRequiresReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executions", "state.json")
	bus := events.NewBus("first")
	engine, err := NewPersistentGraphEngine(&logger.Logger{}, bus, path)
	if err != nil {
		t.Fatal(err)
	}
	engine.Start()
	wf := &WorkflowDefinition{ID: "fixture", Nodes: map[string]WorkflowNode{"node": {ID: "node", WorkerID: "fixture"}}, Roots: []string{"node"}, Parents: map[string][]string{}, Children: map[string][]string{}}
	if err := engine.SubmitWorkflow(wf, "durable"); err != nil {
		t.Fatal(err)
	}
	bus.Close()

	bus2 := events.NewBus("second")
	defer bus2.Close()
	recovered, err := NewPersistentGraphEngine(&logger.Logger{}, bus2, path)
	if err != nil {
		t.Fatal(err)
	}
	execution := recovered.Executions["durable"]
	if execution == nil || execution.Status != ExecutionInterrupted || execution.NodeStates["node"] != NodeInterrupted {
		t.Fatalf("in-flight work was not recovered as interrupted: %#v", execution)
	}
}

func TestCompletionPersistenceFailureInterruptsExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executions", "state.json")
	bus := events.NewBus("persistence-failure")
	defer bus.Close()
	engine, err := NewPersistentGraphEngine(&logger.Logger{}, bus, path)
	if err != nil {
		t.Fatal(err)
	}
	engine.Start()
	wf := &WorkflowDefinition{ID: "fixture", Nodes: map[string]WorkflowNode{"node": {ID: "node", WorkerID: "fixture"}}, Roots: []string{"node"}, Parents: map[string][]string{}, Children: map[string][]string{}}
	if err := engine.SubmitWorkflow(wf, "durable"); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.Executions["durable"].ActiveAttempt["node"] = "durable/attempt"
	engine.mu.Unlock()
	// A regular file used as the parent directory deterministically fails the next save.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	engine.store.path = filepath.Join(blocker, "state.json")
	failed := make(chan map[string]any, 1)
	completed := make(chan struct{}, 1)
	bus.Subscribe("RuntimePersistenceFailed", func(e events.RuntimeEvent) { failed <- e.Payload.(map[string]any) })
	bus.Subscribe("WorkflowCompleted", func(events.RuntimeEvent) { completed <- struct{}{} })
	bus.Publish("WorkerCompleted", "test", map[string]any{"task_id": "durable|node", "attempt_id": "durable/attempt"})
	select {
	case payload := <-failed:
		if payload["execution"] != "durable" || payload["phase"] != "worker_completion" {
			t.Fatalf("persistence failure lacks identity: %#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("persistence failure was not published")
	}
	if execution := engine.Executions["durable"]; execution.Status != ExecutionInterrupted || execution.NodeStates["node"] != NodeInterrupted {
		t.Fatalf("unpersisted completion was not interrupted: %#v", execution)
	}
	select {
	case <-completed:
		t.Fatal("unpersisted completion was published as successful")
	default:
	}
}

func TestEffectRecoveryAndReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects", "state.json")
	manager, err := NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Begin(EffectRecord{ID: "apply-1", AttemptID: "run/attempt", Adapter: "fake-cloud", Target: "account/region", RequestHash: "abc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Begin(EffectRecord{ID: "apply-1", AttemptID: "run/other-attempt", Adapter: "fake-cloud", Target: "account/region", RequestHash: "abc"}); err == nil {
		t.Fatal("effect identity was reused across attempts")
	}
	restarted, err := NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := restarted.Get("apply-1")
	if record.State != EffectInterrupted {
		t.Fatalf("running effect was not interrupted: %#v", record)
	}
	if err := restarted.Reconcile("apply-1", EffectSucceeded, "external-42", "observed complete"); err != nil {
		t.Fatal(err)
	}
	record, _ = restarted.Get("apply-1")
	if record.State != EffectSucceeded || record.ExternalID != "external-42" {
		t.Fatalf("bad reconciliation: %#v", record)
	}
}

func TestDeviceLeaseSerializesOwners(t *testing.T) {
	manager := NewDeviceLeaseManager()
	release, err := manager.Acquire(context.Background(), "one", []string{"0"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := manager.Acquire(ctx, "two", []string{"0"}); err == nil {
		t.Fatal("contended device lease was granted")
	}
	release()
	releaseTwo, err := manager.Acquire(context.Background(), "two", []string{"0"})
	if err != nil {
		t.Fatal(err)
	}
	releaseTwo()
}

func TestMLHostDetectionAndWindowsWarning(t *testing.T) {
	if classifyMLHost("linux", "5.15.0-microsoft-standard-WSL2", "") != MLHostWSL2 || classifyMLHost("windows", "", "") != MLHostWindows {
		t.Fatal("ML host environment was not classified correctly")
	}
	warnings, err := AssessMLProfile("amd-rocm", MLHostWindows, []string{"0"})
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "Windows") {
		t.Fatalf("expected a native Windows AMD warning, got %v, %v", warnings, err)
	}
	if _, err := AssessMLProfile("amd-rocm", MLHostWindows, nil); err == nil {
		t.Fatal("accelerator profile accepted without an explicit device")
	}
}

func TestRegistryRejectsUnknownCapability(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "worker.py"), []byte("# fixture"), 0600)
	os.WriteFile(filepath.Join(dir, "definition.yml"), []byte("id: fixture\nname: Fixture\nversion: 1\nruntime: python\nentrypoint: worker.py\ncapabilities: [cloud.root]\n"), 0600)
	if NewRegistry().LoadAgents(dir) == nil {
		t.Fatal("unknown capability silently accepted")
	}
}

func TestRegistryRejectsUnpinnedSkillDependency(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yml"), []byte("id: unsafe\nname: Unsafe\nversion: 1\ndependency_policy: locked\ndependencies: [requests]\n"), 0600)
	if NewRegistry().LoadSkills(dir) == nil {
		t.Fatal("unpinned dependency silently accepted")
	}
}

func TestRegistryLoadsFloatingAndProjectSkills(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yml"), []byte("id: compatible\nname: Compatible\nversion: 1\ndependency_policy: floating\ndependencies: [requests]\n"), 0600)
	registry := NewRegistry()
	if err := registry.LoadSkills(dir); err != nil {
		t.Fatalf("explicit floating dependency was rejected: %v", err)
	}
	if err := NewRegistry().LoadSkills(filepath.Join("..", "..", "skills")); err != nil {
		t.Fatalf("project skill registry does not load: %v", err)
	}
}

func TestEnvironmentProvisioningPublishesStructuredStatus(t *testing.T) {
	bus := events.NewBus("environment-test")
	defer bus.Close()
	received := make(chan events.RuntimeEvent, 1)
	bus.Subscribe(events.EventType("EnvironmentProvisioningCompleted"), func(event events.RuntimeEvent) {
		received <- event
	})
	manager := &EnvironmentManager{Logger: &logger.Logger{}, Bus: bus}
	manager.publishProvisioning("EnvironmentProvisioningCompleted", "run__coder", []string{"requests==2.34.2"}, true, false, 750*time.Millisecond, nil, provisioningContext{})
	select {
	case event := <-received:
		payload, ok := event.Payload.(map[string]any)
		if !ok || payload["scope"] != "agent" || payload["agent_id"] != WorkerID("run__coder") || payload["duration_ms"] != int64(750) {
			t.Fatalf("unexpected provisioning payload: %#v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("provisioning event was not published")
	}
}

func TestRegistryRejectsProfileDependencies(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yml"), []byte("id: unsafe\nname: Unsafe\nversion: 1\ndependency_policy: profile\ndependencies: [torch]\n"), 0600)
	if NewRegistry().LoadSkills(dir) == nil {
		t.Fatal("profile-managed skill accepted a package dependency")
	}
}

func TestGraphMutationRequiresActiveAttemptAndBudget(t *testing.T) {
	bus := events.NewBus("mutation")
	engine := NewGraphEngine(&logger.Logger{}, bus)
	engine.SetWorkerValidator(func(_ string, worker string) bool { return worker == "delegate" })
	engine.Start()
	wf := &WorkflowDefinition{ID: "fixture", Nodes: map[string]WorkflowNode{"supervisor": {ID: "supervisor", WorkerID: "supervisor"}}, Roots: []string{"supervisor"}, Parents: map[string][]string{}, Children: map[string][]string{}}
	if err := engine.SubmitWorkflow(wf, "mutation-run"); err != nil {
		t.Fatal(err)
	}
	mutation := &GraphMutation{Action: "delegate", TargetAgent: "delegate"}
	bus.Publish("GraphMutationRequested", "test", map[string]any{"task_id": TaskID("mutation-run|supervisor"), "attempt_id": "stale", "mutation": mutation})
	bus.Publish("AttemptStarted", "test", map[string]any{"task_id": TaskID("mutation-run|supervisor"), "attempt_id": "active"})
	bus.Publish("GraphMutationRequested", "test", map[string]any{"task_id": TaskID("mutation-run|supervisor"), "attempt_id": "active", "mutation": mutation})
	bus.Close()
	execution := engine.Executions["mutation-run"]
	if len(execution.Workflow.Nodes) != 2 || execution.GraphRevision != 1 {
		t.Fatalf("expected exactly one committed mutation: nodes=%d revision=%d", len(execution.Workflow.Nodes), execution.GraphRevision)
	}
}
