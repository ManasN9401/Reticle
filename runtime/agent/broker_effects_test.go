package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reticle/runtime/toolbroker"
)

func TestBrokerEffectsSurviveRestartAndRequireReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.json")
	m, err := NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := toolbroker.CallIntent{ID: "a/c", AttemptID: "a", ExecutionID: "run", TaskID: "run|task", Adapter: "mcp", Tool: "mcp.write", RequestHash: "digest"}
	if err := m.BeginCall(intent); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("intent not durable: %v", err)
	}
	restarted, err := NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := restarted.Get(intent.ID)
	if old.State != EffectInterrupted {
		t.Fatalf("restart guessed success: %#v", old)
	}
	next := intent
	next.ID = "b/c"
	next.AttemptID = "b"
	if err := restarted.BeginCall(next); err == nil {
		t.Fatal("unresolved effect replayed under fresh attempt")
	}
	if err := restarted.Reconcile(intent.ID, EffectFailed, "remote-receipt", "observer confirmed no operation"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.BeginCall(next); err != nil {
		t.Fatal(err)
	}
	if err := restarted.FinishCall(next.ID, toolbroker.CallOutcome{OK: true, Effect: toolbroker.Uncertain}); err != nil {
		t.Fatal(err)
	}
	uncertain, _ := restarted.Get(next.ID)
	if uncertain.State != EffectInterrupted {
		t.Fatal("transport success erased uncertainty")
	}
	third := next
	third.ID = "c/c"
	third.AttemptID = "c"
	if err := restarted.BeginCall(third); err == nil {
		t.Fatal("uncertain success blindly repeated")
	}
	third.TaskID = "run|different"
	if err := restarted.BeginCall(third); err != nil {
		t.Fatalf("independent task blocked: %v", err)
	}
	if err := restarted.FinishCall(third.ID, toolbroker.CallOutcome{OK: true, Effect: toolbroker.NoEffect}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.BeginCall(third); err == nil {
		t.Fatal("durable call identity reused")
	}
}

func TestBrokerEffectsFailBeforeAdmissionWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	m, err := NewPersistentEffectManager(filepath.Join(dir, "effects.json"))
	if err != nil {
		t.Fatal(err)
	}
	// An existing directory cannot be replaced by the snapshot file.
	m.path = dir
	intent := toolbroker.CallIntent{ID: "a/c", AttemptID: "a", ExecutionID: "run", TaskID: "task", Adapter: "adapter", Tool: "tool", RequestHash: "hash"}
	if err := m.BeginCall(intent); err == nil {
		t.Fatal("persistence failure admitted effect")
	}
	if _, exists := m.Get(intent.ID); exists {
		t.Fatal("failed intent left only in memory")
	}
}

func TestBrokerEffectsRecoverPreviousSnapshotAndRejectCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.json")
	m, err := NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := toolbroker.CallIntent{ID: "a/c", AttemptID: "a", ExecutionID: "run", TaskID: "task", Adapter: "mcp", Tool: "write", RequestHash: "digest"}
	if err := m.BeginCall(intent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".previous"); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := recovered.Get(intent.ID)
	if record.State != EffectInterrupted {
		t.Fatalf("previous snapshot erased uncertainty: %#v", record)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recovery did not republish snapshot: %v", err)
	}
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentEffectManager(path); err == nil {
		t.Fatal("null snapshot accepted")
	}
}
