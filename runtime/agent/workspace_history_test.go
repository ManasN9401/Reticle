package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureHistory(t *testing.T) (*WorkspaceHistory, string) {
	t.Helper()
	workspace := t.TempDir()
	source := filepath.Join(workspace, "src")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	h, err := OpenWorkspaceHistory(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.mu.Lock()
		h.active = map[string]*WorkspaceCheckpoint{}
		h.mu.Unlock()
		h.Close()
		// Git makes loose objects read-only; only this test-owned history is relaxed
		// so Windows TempDir cleanup can remove its fixtures under the sandbox.
		filepath.WalkDir(h.store, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				if entry.IsDir() {
					os.Chmod(path, 0700)
				} else {
					os.Chmod(path, 0600)
				}
			}
			return nil
		})
	})
	return h, source
}
func TestWorkspaceHistorySnapshotsRawFilesAndDoesNotRunFilters(t *testing.T) {
	h, source := fixtureHistory(t)
	if _, err := h.git(nil, "config", "filter.malicious.clean", "reticle-filter-command-that-does-not-exist"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.git(nil, "config", "filter.malicious.required", "true"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("before\r\n"), 0644)
	os.WriteFile(filepath.Join(source, "café.txt"), []byte("unicode path"), 0644)
	os.WriteFile(filepath.Join(source, "empty.txt"), []byte{}, 0644)
	os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("*.txt filter=malicious\n*.txt export-ignore\n"), 0644)
	os.WriteFile(filepath.Join(source, ".env"), []byte("excluded fixture"), 0600)
	os.Mkdir(filepath.Join(source, "node_modules"), 0755)
	os.WriteFile(filepath.Join(source, "node_modules", "skip"), []byte("excluded fixture"), 0644)
	record, err := h.Begin("task", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("after\r\n"), 0644)
	os.WriteFile(filepath.Join(source, "new.bin"), []byte{0, 1, 2, 3}, 0644)
	os.Remove(filepath.Join(source, "empty.txt"))
	if err = h.Finish(record, "failed", effectsFiles); err != nil {
		t.Fatal(err)
	}
	before, err := h.tree(record.Before)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := before["café.txt"]; !ok {
		t.Fatal("Unicode checkpoint path lost")
	}
	if _, ok := before[".env"]; ok {
		t.Fatal("environment file captured")
	}
	if _, ok := before["node_modules/skip"]; ok {
		t.Fatal("dependencies captured")
	}
	raw, err := h.blob(before["a.txt"])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "before\r\n" {
		t.Fatalf("raw data changed: %q", raw)
	}
	diff, err := h.Diff(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "a.txt") || !strings.Contains(diff, "new.bin") {
		t.Fatal(diff)
	}
	os.WriteFile(filepath.Join(source, "unrelated.txt"), []byte("keep"), 0644)
	if err = h.Rewind(record.ID, record.After); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(source, "a.txt"))
	if string(raw) != "before\r\n" {
		t.Fatalf("rewind=%q", raw)
	}
	if info, statErr := os.Stat(filepath.Join(source, "empty.txt")); statErr != nil || info.Size() != 0 {
		t.Fatal("empty file deletion was not restored")
	}
	if _, err = os.Stat(filepath.Join(source, "new.bin")); !os.IsNotExist(err) {
		t.Fatal("new attempt file not removed")
	}
	if raw, _ = os.ReadFile(filepath.Join(source, "unrelated.txt")); string(raw) != "keep" {
		t.Fatal("unrelated file lost")
	}
	// The receipt is deliberately consumable by the SDK's patch recovery validator.
	data, err := os.ReadFile(filepath.Join(h.workspace, "patches", "completed", record.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if json.Unmarshal(data, &receipt) != nil || receipt["state"] != "committed" {
		t.Fatal("bad rewind receipt")
	}
	logged, err := h.load(record.ID)
	if err != nil || logged.RewoundAt == nil {
		t.Fatal("rewind observation time was not persisted")
	}
	if err = h.Rewind(record.ID, record.After); err == nil {
		t.Fatal("double rewind accepted")
	}
}
func TestWorkspaceRewindRefusesLaterEditsUnsafeEffectsAndOverlap(t *testing.T) {
	for _, mode := range []string{"later", "unsafe", "overlap", "expected", "success"} {
		t.Run(mode, func(t *testing.T) {
			h, source := fixtureHistory(t)
			os.WriteFile(filepath.Join(source, "a"), []byte("before"), 0644)
			record, err := h.Begin("task", "attempt")
			if err != nil {
				t.Fatal(err)
			}
			var other *WorkspaceCheckpoint
			if mode == "overlap" {
				other, err = h.Begin("other", "other-attempt")
				if err != nil {
					t.Fatal(err)
				}
			}
			os.WriteFile(filepath.Join(source, "a"), []byte("after"), 0644)
			proof := effectsFiles
			if mode == "unsafe" {
				proof = effectsUnsafe
			}
			outcome := "failed"
			if mode == "success" {
				outcome = "succeeded"
			}
			if err = h.Finish(record, outcome, proof); err != nil {
				t.Fatal(err)
			}
			if other != nil {
				if err = h.Finish(other, "failed", effectsFiles); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "later" {
				os.WriteFile(filepath.Join(source, "a"), []byte("user edit"), 0644)
			}
			expected := record.After
			if mode == "expected" {
				expected = strings.Repeat("a", 40)
			}
			if err = h.Rewind(record.ID, expected); err == nil {
				t.Fatal("unsafe rewind accepted")
			}
			raw, _ := os.ReadFile(filepath.Join(source, "a"))
			if mode == "later" && string(raw) != "user edit" {
				t.Fatal("user edit overwritten")
			}
		})
	}
}
func TestWorkspaceHistoryRejectsTraversalAndBusyFiles(t *testing.T) {
	h, source := fixtureHistory(t)
	os.WriteFile(filepath.Join(source, "a"), []byte("before"), 0644)
	record, err := h.Begin("task", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "a"), []byte("after"), 0644)
	if err = h.Finish(record, "failed", effectsFiles); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../escape", "--all", strings.Repeat("x", 32)} {
		if _, err = h.Diff(id); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	lock, err := lockWorkspaceFile(filepath.Join(source, "a.reticle-lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Rewind(record.ID, record.After); err == nil {
		t.Fatal("busy file overwritten")
	}
	lock.Close()
}
func TestWorkerAutomaticallyRecordsCheckpoints(t *testing.T) {
	t.Setenv("RETICLE_WORKSPACE_CHECKPOINTS", "true")
	h, _ := fixtureHistory(t)
	workspace := h.workspace
	bus := events.NewBus("history")
	defer bus.Close()
	exe, _ := os.Executable()
	worker := NewWorker("fixture", exe, []string{"-test.run=^TestWorkerChild$"}, []string{"RETICLE_TEST_CHILD=wrong"}, &logger.Logger{}, bus)
	_, failure := worker.Execute(context.Background(), Task{ID: "task", AttemptID: "attempt", Memory: map[string]any{"workspace_dir": workspace}})
	if failure == nil {
		t.Fatal("fixture unexpectedly succeeded")
	}
	records, err := h.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Outcome != "failed" || records[0].Before == "" || records[0].After == "" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestInterruptedWorkspaceRewindUsesSDKRecovery(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python required for cross-language journal recovery")
	}
	h, source := fixtureHistory(t)
	for _, name := range []string{"a", "b"} {
		os.WriteFile(filepath.Join(source, name), []byte("before"), 0644)
	}
	record, err := h.Begin("task", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		os.WriteFile(filepath.Join(source, name), []byte("after"), 0644)
	}
	if err = h.Finish(record, "failed", effectsFiles); err != nil {
		t.Fatal(err)
	}
	count := 0
	h.replaceFile = func(path string, data []byte, mode uint32, expected *string) error {
		count++
		if count == 2 {
			return fmt.Errorf("injected restore failure")
		}
		return writeRewindTarget(path, data, mode, expected)
	}
	if err = h.Rewind(record.ID, record.After); err == nil {
		t.Fatal("injected failure ignored")
	}
	raw, _ := os.ReadFile(filepath.Join(source, "a"))
	if string(raw) != "before" {
		t.Fatal("first restoration did not run")
	}
	library, err := filepath.Abs("../../cmd/forge/compiler/lib")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-B", "-c", "import sys;sys.path.insert(0,sys.argv[1]);import patch_tools;patch_tools.recover(sys.argv[2])", library, h.workspace)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("SDK recovery failed: %v %s", err, output)
	}
	for _, name := range []string{"a", "b"} {
		raw, _ := os.ReadFile(filepath.Join(source, name))
		if string(raw) != "after" {
			t.Fatalf("incomplete rewind was not rolled back: %s=%q", name, raw)
		}
	}
}
