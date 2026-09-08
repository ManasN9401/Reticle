package main

import (
	"os"
	"path/filepath"
	"testing"
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
