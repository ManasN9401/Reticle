package routing

import (
	"github.com/reticle/runtime/logger"
	"testing"
	"time"
)

func TestCapacityAndCooldownWithoutLearning(t *testing.T) {
	ModelsMutex.Lock()
	old := AvailableModels
	AvailableModels = []Model{{ID: "fixture", Enabled: true, Modality: "text", APIKeyEnv: "FIXTURE_KEY"}}
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels = old; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, inFlight: map[string]string{}, ProviderCapacity: map[string]int{"FIXTURE_KEY": 1}, ProviderInFlight: map[string]int{}}
	if r.SelectModel("one", "agent", 1, 0.9, "text") == nil {
		t.Fatal("available model rejected")
	}
	if r.SelectModel("two", "agent", 1, 0.9, "text") != nil {
		t.Fatal("capacity exceeded when learning disabled")
	}
	r.Release("one")
	if r.ProviderInFlight["FIXTURE_KEY"] != 0 {
		t.Fatal("lease leaked")
	}
	ModelsMutex.Lock()
	AvailableModels[0].CooldownUntil = time.Now().Add(time.Hour)
	ModelsMutex.Unlock()
	if r.SelectModel("three", "agent", 1, 0.9, "text") != nil {
		t.Fatal("cooldown bypassed")
	}
}
