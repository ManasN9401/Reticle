package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/reticle/runtime/agent"
)

func (s *Server) workspaceHistory(execution string) (*agent.WorkspaceHistory, error) {
	if execution == "" || strings.ContainsAny(execution, "/\\:") || execution == "." || execution == ".." {
		return nil, fmt.Errorf("invalid execution id")
	}
	workspace, err := SafePath(filepath.Join(s.rootDir, ".reticle", "sessions"), execution)
	if err != nil {
		return nil, err
	}
	store, err := SafePath(workspace, "history/repository")
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(store); err != nil {
		return nil, fmt.Errorf("workspace history unavailable")
	}
	return agent.OpenWorkspaceHistory(workspace)
}
func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	h, err := s.workspaceHistory(r.URL.Query().Get("execution"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	records, err := h.Records()
	if err != nil {
		http.Error(w, "checkpoint history unavailable", 409)
		return
	}
	truncated := len(records) > 1000
	if truncated {
		records = records[:1000]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"checkpoints": records, "truncated": truncated})
}
func (s *Server) handleCheckpointDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	h, err := s.workspaceHistory(r.URL.Query().Get("execution"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	diff, err := h.Diff(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "checkpoint diff unavailable", 409)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, diff)
}
func (s *Server) handleCheckpointRewind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var request struct {
		Execution     string `json:"execution"`
		ID            string `json:"id"`
		ExpectedAfter string `json:"expected_after"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(request.ExpectedAfter) != 40 {
		http.Error(w, "execution, checkpoint and expected_after are required", 400)
		return
	}
	h, err := s.workspaceHistory(request.Execution)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err = h.Rewind(request.ID, request.ExpectedAfter); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
