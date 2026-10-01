package routing

import (
	"testing"
	"time"

	"github.com/reticle/runtime/logger"
)

func TestTaskExclusionSurvivesConfidenceAndModalityFallback(t *testing.T) {
	ModelsMutex.Lock()
	old, health := AvailableModels, KeyHealthStates
	AvailableModels = []Model{
		{ID: "coder/a", Provider: "coder", Enabled: true, Modality: "coding", Capability: 10},
		{ID: "chat/a", Provider: "chat", Enabled: true, Modality: "text", Capability: 40},
	}
	KeyHealthStates = nil
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels, KeyHealthStates = old, health; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{"agent": {"coder/a": .1, "chat/a": .1}}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}, UseBayesianRouting: true}
	policy := RoutePolicy{Excluded: map[string]bool{"coder/a": true}}
	got := r.SelectModelWithPolicy("task", "agent", 2, .9, "coding", policy)
	if got == nil || got.ID != "chat/a" {
		t.Fatalf("fallback lost exclusions: %#v", got)
	}
	r.Release("task")
	policy.Excluded["chat/a"] = true
	if got := r.SelectModelWithPolicy("task", "agent", 2, .9, "coding", policy); got != nil {
		t.Fatalf("rejected model returned: %#v", got)
	}
	if r.RouteAvailability("coding", policy).Potential != 0 {
		t.Fatal("permanent exhaustion incorrectly requires waiting")
	}
	if got := r.SelectModel("other-task", "agent", 2, .9, "coding"); got == nil {
		t.Fatal("exclusions escaped their task")
	}
	r.Release("other-task")
}

func TestTemporaryProviderFailureDoesNotInvalidateKey(t *testing.T) {
	ModelsMutex.Lock()
	old, health := AvailableModels, KeyHealthStates
	AvailableModels = []Model{
		{ID: "a/one", Provider: "a", APIKeyEnv: "A", Enabled: true, Modality: "text"},
		{ID: "a/two", Provider: "a", APIKeyEnv: "A", Enabled: true, Modality: "text"},
		{ID: "b/one", Provider: "b", APIKeyEnv: "B", Enabled: true, Modality: "text"},
	}
	KeyHealthStates = []KeyHealth{{Key: "A", Status: KeyHealthy}}
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels, KeyHealthStates = old, health; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{"A": 5}, ProviderInFlight: map[string]int{}}
	r.CoolProviderSlot("A", time.Minute)
	if r.ProviderCapacity["A"] != 5 || KeyHealthStates[0].Status != KeyHealthy {
		t.Fatal("transport error changed credential health/capacity")
	}
	policy := RoutePolicy{PreferredKey: "a/one|A"}
	if got := r.SelectModelWithPolicy("task", "agent", 0, .9, "text", policy); got == nil || got.Provider != "b" {
		t.Fatalf("preference bypassed cooldown: %#v", got)
	}
	r.Release("task")
	s := r.RouteAvailability("text", policy)
	if s.Potential != 3 || s.Eligible != 1 {
		t.Fatalf("temporary and permanent exclusion conflated: %#v", s)
	}
	r.CoolProviderSlot("B", time.Minute)
	if got := r.SelectModel("task", "agent", 0, .9, "text"); got != nil {
		t.Fatal("cooldown bypassed by fallback")
	}
	ModelsMutex.Lock()
	for i := range AvailableModels {
		AvailableModels[i].CooldownUntil = time.Now().Add(-time.Second)
	}
	ModelsMutex.Unlock()
	if got := r.SelectModel("probe", "agent", 0, .9, "text"); got == nil {
		t.Fatal("recovery probe could not route")
	}
	r.Release("probe")
}

func TestSuccessfulStrongRouteNeedsNoProviderRotation(t *testing.T) {
	t.Setenv("RETICLE_ROOT", t.TempDir())
	ModelsMutex.Lock()
	old, health := AvailableModels, KeyHealthStates
	AvailableModels = []Model{
		{ID: "strong/model", Provider: "strong", Enabled: true, Modality: "text", Capability: 100},
		{ID: "other/model", Provider: "other", Enabled: true, Modality: "text", Capability: 10},
	}
	KeyHealthStates = nil
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels, KeyHealthStates = old, health; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}, UseBayesianRouting: true}
	for _, task := range []string{"first", "next"} {
		got := r.SelectModel(task, "agent", 5, .9, "text")
		if got == nil || got.Provider != "strong" {
			t.Fatalf("successful suitable route displaced: %#v", got)
		}
		r.UpdateProbability("agent", task, true)
	}
}
