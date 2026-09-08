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

func TestRetriesRequireNoEffectProof(t *testing.T) {
	for _, fixture := range []struct {
		text string
		want bool
	}{
		{"RateLimitError", false},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] RateLimitError", true},
		{"[RETICLE_RETRY_SAFE: NO_EFFECTS] command failed", false},
	} {
		if retryableProviderFailure(&WorkerFailure{Reason: WorkerExitedNonZero, Stderr: fixture.text}) != fixture.want {
			t.Fatal("unsafe retry classification")
		}
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
		if req.Memory["shared"] != "expected" || len(req.Inputs) != 1 || req.Inputs[0].Data != "output" {
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
