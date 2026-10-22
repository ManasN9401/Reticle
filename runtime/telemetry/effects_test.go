package telemetry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/toolbroker"
)

func TestInterruptedEffectsAreFilteredBeforeHistoryLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.json")
	records := map[string]agent.EffectRecord{}
	for i := 0; i < 1002; i++ {
		id := fmt.Sprintf("a/%04d", i)
		records[id] = agent.EffectRecord{ID: id, AttemptID: "a", Adapter: "fixture", Target: "tool", RequestHash: "digest", State: agent.EffectSucceeded}
	}
	record := records["a/1001"]
	record.State = agent.EffectInterrupted
	records[record.ID] = record
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewPersistentEffectManager(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetEffectManager(manager)
	w := httptest.NewRecorder()
	s.handleEffects(w, httptest.NewRequest("GET", "http://localhost/api/effects?state=interrupted", nil))
	var response struct {
		Effects   []agent.EffectRecord `json:"effects"`
		Truncated bool                 `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(response.Effects) != 1 || response.Truncated || response.Effects[0].ID != record.ID {
		t.Fatalf("unresolved operation hidden behind history limit: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleEffects(w, httptest.NewRequest("GET", "http://localhost/api/effects?state=bogus", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatal("unknown state filter accepted")
	}
}

func TestEffectReconciliationRequiresAuthenticatedObservedState(t *testing.T) {
	m, err := agent.NewPersistentEffectManager(filepath.Join(t.TempDir(), "effects.json"))
	if err != nil {
		t.Fatal(err)
	}
	intent := toolbroker.CallIntent{ID: "a/c", AttemptID: "a", ExecutionID: "run", TaskID: "task", Adapter: "mcp", Tool: "write", RequestHash: "digest"}
	if err := m.BeginCall(intent); err != nil {
		t.Fatal(err)
	}
	if err := m.FinishCall(intent.ID, toolbroker.CallOutcome{OK: true, Effect: toolbroker.Uncertain}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetEffectManager(m)
	token := strings.Repeat("a", 64)
	h := protected(token, http.HandlerFunc(s.handleEffectReconcile))
	for _, fixture := range []struct {
		body, credential string
		want             int
	}{
		{`{"id":"a/c","state":"failed","observation":"remote lookup confirmed absent"}`, "", 401},
		{`{"id":"a/c","state":"failed","observation":""}`, token, 400},
		{`{"id":"a/c","state":"running","observation":"guess"}`, token, 409},
		{`{"id":"a/c","state":"failed","observation":"remote lookup confirmed absent","unknown":1}`, token, 400},
		{`{"id":"a/c","state":"failed","observation":"remote lookup confirmed absent"}`, token, 200},
		{`{"id":"a/c","state":"succeeded","observation":"contradict earlier state"}`, token, 409},
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/effects/reconcile", strings.NewReader(fixture.body))
		r.Header.Set("Authorization", "Bearer "+fixture.credential)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != fixture.want {
			t.Fatalf("got %d want %d: %s", w.Code, fixture.want, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.handleEffects(w, httptest.NewRequest("GET", "http://127.0.0.1/api/effects", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"failed"`) {
		t.Fatalf("missing observed ledger state: %s", w.Body.String())
	}
}
