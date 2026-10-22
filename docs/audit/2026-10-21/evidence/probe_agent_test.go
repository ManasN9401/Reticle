package agent

// Audit-only tests loaded with go -overlay; production sources are unchanged.
import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/reticle/runtime/events"
    "github.com/reticle/runtime/logger"
    "github.com/reticle/runtime/memory"
)

func TestAuditEmptyCapabilities(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "worker.py"), []byte("# fixture"), 0600)
    manifest := filepath.Join(dir, "agent.yaml")
    os.WriteFile(manifest, []byte("id: audit\nname: Audit\nversion: 1\nruntime: python\nentrypoint: worker.py\ncapabilities: []\n"), 0600)
    definition, err := ReadAgentDefinition(manifest)
    if err != nil { t.Fatal(err) }
    t.Logf("explicit empty capabilities resolves to %v", definition.Capabilities)
}

func TestAuditWorkerHelper(t *testing.T) {
    if os.Getenv("RETICLE_AUDIT_HELPER") != "1" { return }
    var req Task
    if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil { os.Exit(2) }
    json.NewEncoder(os.Stdout).Encode(TaskResponse{
        ID: req.ID,
        Memory: []MemoryMutation{{Key: "audit-fact", Value: "fixture"}},
        GraphMutation: &GraphMutation{Action: "delegate", TargetAgent: "delegate"},
    })
    os.Exit(0)
}

func TestAuditProtocolCapabilitiesAndVerification(t *testing.T) {
    bus := events.NewBus("audit")
    memory.NewManager(memory.NewArtifactStore(), memory.NewRuntimeState(), memory.NewSessionState("audit"), bus)
    engine := NewGraphEngine(&logger.Logger{}, bus)
    engine.SetWorkerValidator(func(_, worker string) bool { return worker == "delegate" })
    engine.Start()
    wf := &WorkflowDefinition{ID:"fixture", Nodes:map[string]WorkflowNode{"source": {ID:"source", WorkerID:"source"}}, Roots:[]string{"source"}, Parents:map[string][]string{}, Children:map[string][]string{}}
    if err := engine.SubmitWorkflow(wf, "audit-run"); err != nil { t.Fatal(err) }
    bus.Publish("AttemptStarted", "test", map[string]any{"task_id":"audit-run|source", "attempt_id":"audit-attempt"})
    executable, err := os.Executable()
    if err != nil { t.Fatal(err) }
    worker := NewWorker("source", executable, []string{"-test.run=^TestAuditWorkerHelper$"}, []string{"RETICLE_AUDIT_HELPER=1"}, &logger.Logger{}, bus)
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    resp, failure := worker.Execute(ctx, Task{ID:"audit-run|source", AgentID:"source", ExecutionID:"audit-run", AttemptID:"audit-attempt", Capabilities:[]Capability{CapabilityWorkspaceRead}})
    bus.Close()
    if failure != nil { t.Fatalf("probe failed: %v", failure) }
    t.Logf("read-only grant accepted memory=%d, graph mutation=%v, verification count=%d, graph nodes=%d", len(resp.Memory), resp.GraphMutation != nil, len(resp.Verification), len(engine.Executions["audit-run"].Workflow.Nodes))
}
