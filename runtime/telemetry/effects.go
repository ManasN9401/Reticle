package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/reticle/runtime/agent"
)

// SetEffectManager is configured before the authenticated control server starts.
func (s *Server) SetEffectManager(manager *agent.EffectManager) { s.effects = manager }

func (s *Server) handleEffects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.effects == nil {
		http.Error(w, "effect ledger unavailable", http.StatusServiceUnavailable)
		return
	}
	records := s.effects.Snapshot()
	if state := r.URL.Query().Get("state"); state != "" {
		switch agent.EffectState(state) {
		case agent.EffectPrepared, agent.EffectRunning, agent.EffectInterrupted, agent.EffectSucceeded, agent.EffectFailed, agent.EffectCancelled:
		default:
			http.Error(w, "invalid effect state", http.StatusBadRequest)
			return
		}
		filtered := make([]agent.EffectRecord, 0)
		for _, record := range records {
			if string(record.State) == state {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	truncated := len(records) > 1000
	if truncated {
		records = records[:1000]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"effects": records, "truncated": truncated})
}

func (s *Server) handleEffectReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.effects == nil {
		http.Error(w, "effect ledger unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var request struct {
		ID          string            `json:"id"`
		State       agent.EffectState `json:"state"`
		ExternalID  string            `json:"external_id"`
		Observation string            `json:"observation"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || request.ID == "" ||
		strings.TrimSpace(request.Observation) == "" || len(request.Observation) > 4096 || len(request.ExternalID) > 1024 {
		http.Error(w, "effect id and bounded external-state observation are required", http.StatusBadRequest)
		return
	}
	// This is a trusted operator control, not an agent tool or inference about
	// transport success. It records an observation and never re-executes a call.
	if err := s.effects.Reconcile(request.ID, request.State, request.ExternalID, request.Observation); err != nil {
		http.Error(w, "effect is not interrupted or observed state is invalid", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
