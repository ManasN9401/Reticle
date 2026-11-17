package telemetry

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reticle/runtime/agent"
)

func TestCheckpointControlsValidateAndRewindFailedAttempt(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, ".reticle", "sessions", "run")
	source := filepath.Join(workspace, "src")
	os.MkdirAll(source, 0755)
	h, err := agent.OpenWorkspaceHistory(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.Close()
		filepath.WalkDir(filepath.Join(workspace, "history"), func(path string, entry os.DirEntry, err error) error {
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
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("before"), 0644)
	record, err := h.Begin("task", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("after"), 0644)
	if err = h.Finish(record, "failed", "[RETICLE_EFFECTS: FILES]"); err != nil {
		t.Fatal(err)
	}
	s := &Server{rootDir: root}
	list := httptest.NewRecorder()
	s.handleCheckpoints(list, httptest.NewRequest("GET", "/api/checkpoints?execution=run", nil))
	if list.Code != 200 || !strings.Contains(list.Body.String(), record.ID) {
		t.Fatalf("list %d %s", list.Code, list.Body)
	}
	diff := httptest.NewRecorder()
	s.handleCheckpointDiff(diff, httptest.NewRequest("GET", "/api/checkpoints/diff?execution=run&id="+record.ID, nil))
	if diff.Code != 200 || !strings.Contains(diff.Body.String(), "before") {
		t.Fatalf("diff %d %s", diff.Code, diff.Body)
	}
	for _, body := range []string{`{"execution":"../escape","id":"` + record.ID + `","expected_after":"` + record.After + `"}`, `{"execution":"run","id":"` + record.ID + `","expected_after":"` + record.After + `","extra":true}`} {
		result := httptest.NewRecorder()
		s.handleCheckpointRewind(result, httptest.NewRequest("POST", "/api/checkpoints/rewind", strings.NewReader(body)))
		if result.Code != 400 {
			t.Fatalf("invalid request accepted: %d", result.Code)
		}
	}
	body, _ := json.Marshal(map[string]string{"execution": "run", "id": record.ID, "expected_after": record.After})
	result := httptest.NewRecorder()
	s.handleCheckpointRewind(result, httptest.NewRequest("POST", "/api/checkpoints/rewind", strings.NewReader(string(body))))
	if result.Code != 200 {
		t.Fatalf("rewind %d %s", result.Code, result.Body)
	}
	raw, _ := os.ReadFile(filepath.Join(source, "a.txt"))
	if string(raw) != "before" {
		t.Fatal("rewind did not restore source")
	}
	blocked := httptest.NewRecorder()
	s.handleCheckpoints(blocked, httptest.NewRequest("POST", "/api/checkpoints?execution=run", nil))
	if blocked.Code != 405 {
		t.Fatal("method accepted")
	}
}
