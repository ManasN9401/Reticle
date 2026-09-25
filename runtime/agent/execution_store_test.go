package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutionStoreSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewExecutionStore(filepath.Join(dir, "state.json"))
	wf := &WorkflowDefinition{Nodes: map[string]WorkflowNode{"a": {ID: "a"}}}
	exec := NewWorkflowExecution("exec-1", wf)
	if err := store.Save(map[string]*WorkflowExecution{"exec-1": exec}, nil, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := snapshot.Executions["exec-1"]; !ok {
		t.Fatal("saved execution was not recoverable")
	}
}

// A Windows antivirus/indexer can hold state.json open without
// FILE_SHARE_DELETE for a moment, making the rename that replaces it fail
// with "Access is denied" even though the file is about to be free again.
// Save() retries that replace a few times before giving up; this proves the
// retry actually recovers once the transient lock clears, rather than only
// existing in the code without ever succeeding in practice.
func TestExecutionStoreSaveRecoversFromTransientLock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	store := NewExecutionStore(target)
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatalf("seed state.json: %v", err)
	}
	// Simulate a lock that clears shortly after Save() starts retrying: hold
	// the file open exclusively on Windows (blocking the rename) for a short
	// window, then release it. On platforms where an open handle doesn't
	// block rename, this goroutine is harmless and the test still passes.
	f, err := os.OpenFile(target, os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("open state.json: %v", err)
	}
	release := make(chan struct{})
	go func() {
		<-release
	}()
	go func() {
		time.Sleep(75 * time.Millisecond)
		f.Close()
		close(release)
	}()

	wf := &WorkflowDefinition{Nodes: map[string]WorkflowNode{"a": {ID: "a"}}}
	exec := NewWorkflowExecution("exec-1", wf)
	if err := store.Save(map[string]*WorkflowExecution{"exec-1": exec}, nil, nil); err != nil {
		t.Fatalf("Save did not recover from a transient lock: %v", err)
	}
}

func TestExecutionStoreSaveFailsWhenLockNeverClears(t *testing.T) {
	dir := t.TempDir()
	// A lock that outlasts every retry attempt is a permanent failure from
	// Save()'s perspective — the bounded retry loop must still terminate and
	// surface the error rather than retrying forever.
	target := filepath.Join(dir, "state.json")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatalf("seed state.json: %v", err)
	}
	f, err := os.OpenFile(target, os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("open state.json: %v", err)
	}
	defer f.Close()

	store := NewExecutionStore(target)
	wf := &WorkflowDefinition{Nodes: map[string]WorkflowNode{"a": {ID: "a"}}}
	exec := NewWorkflowExecution("exec-1", wf)
	done := make(chan error, 1)
	go func() { done <- store.Save(map[string]*WorkflowExecution{"exec-1": exec}, nil, nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Save to fail while the lock never clears")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save did not terminate its bounded retry loop")
	}
}
