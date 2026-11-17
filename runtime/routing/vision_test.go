package routing

import (
	"github.com/reticle/runtime/logger"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVisionRequirementSurvivesAllFallbacks(t *testing.T) {
	ModelsMutex.Lock()
	old, health := AvailableModels, KeyHealthStates
	AvailableModels = []Model{
		{ID: "text/only", Modality: "coding", Enabled: true, Capability: 100},
		{ID: "fixture/visual", Modality: "text", Enabled: true, Vision: true, Capability: 10},
	}
	KeyHealthStates = nil
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels, KeyHealthStates = old, health; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
	policy := RoutePolicy{RequireVision: true}
	got := r.SelectModelWithPolicy("visual-task", "agent", 2, .99, "coding", policy)
	if got == nil || !got.Vision {
		t.Fatalf("text-only fallback escaped vision requirement: %#v", got)
	}
	r.Release("visual-task")
	policy.Excluded = map[string]bool{"fixture/visual": true}
	if got := r.SelectModelWithPolicy("no-route", "agent", 2, .99, "coding", policy); got != nil {
		t.Fatalf("fallback relaxed vision: %#v", got)
	}
	if available := r.RouteAvailability("coding", policy); available.Potential != 0 {
		t.Fatalf("availability=%#v", available)
	}
}

func TestVisionMetadataIsExplicitAndStaticOverridesDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"visual","supports_vision":true},{"id":"name-vision-but-unknown"},{"id":"static","supports_vision":true}]}`))
	}))
	defer server.Close()
	config := ProviderConfig{ID: "fixture", BaseURL: server.URL, ModelsPath: "/models", Enabled: true, Models: []ProviderModelConfig{{ID: "static", Vision: false}}}
	models, _, _ := discoverConfiguredProvider(server.Client(), config, time.Now())
	if len(models) != 3 {
		t.Fatalf("models=%#v", models)
	}
	byID := map[string]bool{}
	for _, model := range models {
		byID[model.RequestModel] = model.Vision
	}
	if !byID["visual"] || byID["name-vision-but-unknown"] || byID["static"] {
		t.Fatalf("metadata=%v", byID)
	}
	if !containsImageInput([]string{"text", "image"}) || containsImageInput([]string{"text"}) {
		t.Fatal("bad catalog image detection")
	}
}
