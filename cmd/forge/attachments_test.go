package main

import (
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/orchestrator"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentIgnoresCallerPath(t *testing.T) {
	staging, workspace := t.TempDir(), t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("private"), 0600)
	_, err := moveAttachment(staging, workspace, Attachment{ID: "missing", Filename: "out", Path: secret})
	if err == nil {
		t.Fatal("unissued upload accepted")
	}
	if _, err = os.Stat(secret); err != nil {
		t.Fatal("caller file removed")
	}
	os.WriteFile(filepath.Join(staging, "issued"), []byte("upload"), 0600)
	_, err = moveAttachment(staging, workspace, Attachment{ID: "issued", Filename: "../escape", Path: secret})
	if err == nil {
		t.Fatal("traversal accepted")
	}
	dst, err := moveAttachment(staging, workspace, Attachment{ID: "issued", Filename: "safe", Path: secret})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "upload" {
		t.Fatal("wrong source copied")
	}
}

func TestQueueDoesNotAdmitUnpersistedWork(t *testing.T) {
	// A directory cannot be atomically replaced by the queue JSON file.
	wm := &WaitlistManager{filePath: t.TempDir(), maxWorkers: 1}
	wm.Enqueue("fixture", "", ModeParallel, "", "standard", 1, nil)
	if len(wm.items) != 0 {
		t.Fatal("work admitted despite persistence failure")
	}
}

func TestSessionCopyReportsMissingSource(t *testing.T) {
	if copySession(filepath.Join(t.TempDir(), "missing"), t.TempDir()) == nil {
		t.Fatal("missing output reported as success")
	}
}

func TestPersistenceFailureStopsAdmissionAndReleasesRunningItems(t *testing.T) {
	bus := events.NewBus("test")
	defer bus.Close()
	orch := &orchestrator.Orchestrator{Bus: bus, Logger: &logger.Logger{}}
	wm := NewWaitlistManager(filepath.Join(t.TempDir(), "waitlist.json"), 1, nil, orch, nil, nil, nil)
	wm.items = []*WaitlistItem{
		{ID: "exec-running", Status: StatusRunning},
		{ID: "exec-pending", Status: StatusPending},
	}
	bus.Publish("RuntimePersistenceFailed", "test", map[string]any{"execution_ids": []string{"compile-exec-running"}, "restart_required": true})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		wm.mu.Lock()
		failed := wm.runtimeFailed && wm.items[0].Status == StatusFailed
		pending := wm.items[1].Status
		wm.mu.Unlock()
		if failed {
			wm.Pump()
			if pending != StatusPending {
				t.Fatalf("unstarted work should remain pending for restart, got %s", pending)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("waitlist did not consume fatal persistence failure")
}
