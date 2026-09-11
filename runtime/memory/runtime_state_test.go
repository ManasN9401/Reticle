package memory

import (
	"errors"
	"fmt"
	"github.com/reticle/runtime/events"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRetentionConflictAndExpiry(t *testing.T) {
	policy := DefaultRetentionPolicy()
	policy.MaxEntries = 2
	policy.MaxBytes = 4096
	policy.ExecutionTTL = time.Hour
	s := NewRuntimeStateWithPolicy(policy)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	created, err := s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "counter", Value: 1})
	if err != nil || created.Version != 1 {
		t.Fatalf("create: %v %#v", err, created)
	}
	stale := uint64(0)
	if _, err = s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "counter", Value: 2, ExpectedVersion: &stale}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	expected := uint64(1)
	updated, err := s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "counter", Value: 2, ExpectedVersion: &expected})
	if err != nil || updated.Version != 2 {
		t.Fatalf("update: %v %#v", err, updated)
	}
	s.set(MemoryEntry{Scope: ScopeGlobal, ScopeID: "global", Key: "kept", Value: true})
	if _, err = s.set(MemoryEntry{Scope: ScopeAgent, ScopeID: "agent", Key: "full", Value: true}); !errors.Is(err, ErrMemoryCapacity) {
		t.Fatalf("expected capacity rejection: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, ok := s.Get(ScopeExecution, "run", "counter"); ok {
		t.Fatal("expired execution memory survived")
	}
	if _, ok := s.Get(ScopeGlobal, "global", "kept"); !ok {
		t.Fatal("global memory expired")
	}
}

func TestSnapshotRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory", "state.json")
	runtimeOne := NewRuntimeState()
	artifactsOne := NewArtifactStoreWithRetention(2)
	runtimeOne.set(MemoryEntry{Scope: ScopeGlobal, ScopeID: "global", Key: "fact", Value: map[string]any{"answer": 42}})
	for i := 0; i < 3; i++ {
		artifactsOne.save(&Artifact{ID: "report", Type: "json", Producer: "worker", Execution: "run", Data: i})
	}
	file := NewSnapshotFile(path)
	if err := file.Save(runtimeOne, artifactsOne); err != nil {
		t.Fatal(err)
	}
	runtimeTwo := NewRuntimeState()
	artifactsTwo := NewArtifactStoreWithRetention(2)
	if err := file.Load(runtimeTwo, artifactsTwo); err != nil {
		t.Fatal(err)
	}
	fact, ok := runtimeTwo.Get(ScopeGlobal, "global", "fact")
	if !ok || fact.Version != 1 || fact.Value.(map[string]any)["answer"] != float64(42) {
		t.Fatalf("bad restored fact: %#v", fact)
	}
	versions, ok := artifactsTwo.GetAllVersions("report")
	if !ok || len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 3 {
		t.Fatalf("bad retained versions: %#v", versions)
	}
	latest, err := artifactsTwo.UpdateVersion("report", 3)
	if err != nil || latest.Version != 4 {
		t.Fatalf("version did not continue: %#v %v", latest, err)
	}
}

func TestPersistentManagerAcknowledgesConflictsAndReleasesExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	b := events.NewBus("one")
	runtimeOne := NewRuntimeState()
	artifactsOne := NewArtifactStore()
	if _, err := NewPersistentManager(artifactsOne, runtimeOne, NewSessionState("one"), b, path); err != nil {
		t.Fatal(err)
	}
	write := func(entry MemoryEntry) error {
		result := make(chan error, 1)
		b.Publish("MemoryWriteRequested", "test", MemoryWriteRequest{Entry: entry, Result: result})
		select {
		case err := <-result:
			return err
		case <-time.After(time.Second):
			t.Fatal("write acknowledgement timed out")
			return nil
		}
	}
	if err := write(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "fact", Value: "one"}); err != nil {
		t.Fatal(err)
	}
	artifactResult := make(chan error, 1)
	b.Publish("ArtifactWriteRequested", "test", ArtifactWriteRequest{Artifact: &Artifact{ID: "final", Type: "json", Producer: "worker", Execution: "run", Data: "retained"}, Result: artifactResult})
	if err := <-artifactResult; err != nil {
		t.Fatal(err)
	}
	stale := uint64(0)
	if err := write(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "fact", Value: "two", ExpectedVersion: &stale}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected acknowledged conflict: %v", err)
	}
	released := make(chan struct{}, 1)
	b.Subscribe("MemoryScopeReleased", func(events.RuntimeEvent) { released <- struct{}{} })
	b.Publish("WorkflowCompleted", "test", map[string]any{"execution": "run"})
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("scope release timed out")
	}
	b.Close()
	b2 := events.NewBus("two")
	defer b2.Close()
	runtimeTwo := NewRuntimeState()
	artifactsTwo := NewArtifactStore()
	if _, err := NewPersistentManager(artifactsTwo, runtimeTwo, NewSessionState("two"), b2, path); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtimeTwo.Get(ScopeExecution, "run", "fact"); ok {
		t.Fatal("released execution recovered after restart")
	}
	if artifact, ok := artifactsTwo.Get("final"); !ok || artifact.Data != "retained" {
		t.Fatalf("final artifact was not retained: %#v", artifact)
	}
}

func TestPersistenceFailureRollsBackAcknowledgedWrite(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	b := events.NewBus("failure")
	defer b.Close()
	state := NewRuntimeState()
	if _, err := NewPersistentManager(NewArtifactStore(), state, NewSessionState("failure"), b, filepath.Join(blocker, "state.json")); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	b.Publish("MemoryWriteRequested", "test", MemoryWriteRequest{Entry: MemoryEntry{Scope: ScopeGlobal, ScopeID: "global", Key: "fact", Value: 1}, Result: result})
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("persistence failure acknowledged as success")
		}
	case <-time.After(time.Second):
		t.Fatal("write acknowledgement timed out")
	}
	if _, ok := state.Get(ScopeGlobal, "global", "fact"); ok {
		t.Fatal("failed durable write remained in memory")
	}
}

func TestWorkerResultCommitIsAtomic(t *testing.T) {
	b := events.NewBus("atomic")
	defer b.Close()
	state := NewRuntimeState()
	artifacts := NewArtifactStore()
	NewManager(artifacts, state, NewSessionState("atomic"), b)
	state.set(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "existing", Value: "original"})
	stale := uint64(0)
	result := make(chan error, 1)
	b.Publish("TaskResultCommitRequested", "test", ResultCommitRequest{Entries: []MemoryEntry{{Scope: ScopeExecution, ScopeID: "run", Key: "new", Value: true}, {Scope: ScopeExecution, ScopeID: "run", Key: "existing", Value: "wrong", ExpectedVersion: &stale}}, Artifact: &Artifact{ID: "partial", Type: "json", Producer: "worker", Data: true}, Result: result})
	select {
	case err := <-result:
		if !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("expected conflict: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("commit acknowledgement timed out")
	}
	if _, ok := state.Get(ScopeExecution, "run", "new"); ok {
		t.Fatal("partial memory commit survived")
	}
	existing, _ := state.Get(ScopeExecution, "run", "existing")
	if existing.Value != "original" || existing.Version != 1 {
		t.Fatalf("existing value changed: %#v", existing)
	}
	if artifacts.Exists("partial") {
		t.Fatal("partial artifact commit survived")
	}
}

func TestIsolationAndConcurrentScopes(t *testing.T) {
	s := NewRuntimeState()
	original := map[string]any{"items": []any{"original"}}
	s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: "one", Key: "fact", Value: original})
	original["items"].([]any)[0] = "mutated"
	got, _ := s.Get(ScopeExecution, "one", "fact")
	if got.Value.(map[string]any)["items"].([]any)[0] != "original" {
		t.Fatal("write aliases caller")
	}
	got.Value.(map[string]any)["items"].([]any)[0] = "mutated"
	scoped := s.GetAllByScope(ScopeExecution, "one")
	if scoped["fact"].(map[string]any)["items"].([]any)[0] != "original" {
		t.Fatal("read aliases store")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i)
			for j := 0; j < 100; j++ {
				s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: id, Key: "fact", Value: j})
				s.GetAllByScope(ScopeExecution, id)
			}
			got, _ := s.Get(ScopeExecution, id, "fact")
			if got.Value != 99 {
				t.Errorf("lost independent scope %s", id)
			}
		}(i)
	}
	wg.Wait()
	if len(s.GetAllByScope(ScopeExecution, "missing")) != 0 {
		t.Fatal("scope leaked")
	}
}

func TestReadRequestJSONScope(t *testing.T) {
	b := events.NewBus("memory-test")
	defer b.Close()
	s := NewRuntimeState()
	NewManager(NewArtifactStore(), s, &SessionState{}, b)
	s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: "run", Key: "fact", Value: 42})
	done := make(chan map[string]any, 1)
	b.Subscribe("MemoryReadCompleted", func(e events.RuntimeEvent) { done <- e.Payload.(map[string]any) })
	b.Publish("MemoryReadRequested", "test", map[string]any{"scope": "execution", "scope_id": "run", "key": "fact", "request_id": "reader-1"})
	select {
	case p := <-done:
		if p["found"] != true || p["value"] != 42 || p["request_id"] != "reader-1" {
			t.Fatalf("incorrect read response: %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("read timed out")
	}
}

func BenchmarkScopeRead(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			policy := DefaultRetentionPolicy()
			policy.MaxEntries = n + 1
			policy.MaxBytes = 64 << 20
			s := NewRuntimeStateWithPolicy(policy)
			for i := 0; i < n; i++ {
				s.set(MemoryEntry{Scope: ScopeExecution, ScopeID: fmt.Sprint(i / 10), Key: fmt.Sprint(i % 10), Value: "a shared fact"})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if len(s.GetAllByScope(ScopeExecution, "0")) != 10 {
					b.Fatal("wrong scope")
				}
			}
		})
	}
}

func TestArtifactConcurrentVersionsAndOwnership(t *testing.T) {
	s := NewArtifactStoreWithRetention(400)
	s.save(&Artifact{ID: "fact", Execution: "one", Producer: "worker", Type: "json", Data: map[string]any{"n": 0}})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := s.UpdateVersion("fact", map[string]any{"n": i*20 + j}); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	versions, ok := s.GetAllVersions("fact")
	if !ok || len(versions) != 321 {
		t.Fatalf("missing history: %d", len(versions))
	}
	for i, v := range versions {
		if v.Version != uint32(i+1) {
			t.Fatalf("non-contiguous version %d", v.Version)
		}
	}
	versions[0].Data.(map[string]any)["n"] = -1
	first, _ := s.GetVersion("fact", 1)
	if first.Data.(map[string]any)["n"] != 0 {
		t.Fatal("version aliases caller")
	}
	if len(s.GetByExecution("one")) != 1 || len(s.GetByExecution("other")) != 0 || len(s.FindByProducer("worker")) != 1 {
		t.Fatal("incorrect latest indexes")
	}
	s.Delete("fact")
	if _, err := s.UpdateVersion("fact", nil); err == nil {
		t.Fatal("update resurrected deleted series")
	}
	if len(s.FindByProducer("worker")) != 0 {
		t.Fatal("deleted index survived")
	}
}
