package routing

import (
	"encoding/json"
	"github.com/reticle/runtime/logger"
	"os"
	"path/filepath"
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

func TestTextTasksRequireExplicitCoderFallback(t *testing.T) {
	ModelsMutex.Lock()
	old := AvailableModels
	AvailableModels = []Model{{ID: "ollama/qwen-coder", Enabled: true, Modality: "coding", APIKeyEnv: "OLLAMA_HOST"}}
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels = old; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, inFlight: map[string]string{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}
	if got := r.SelectModel("planning", "planner", 1, 0.9, "text"); got != nil {
		t.Fatalf("text task silently received coder model: %s", got.ID)
	}
	r.SetTextToCodingFallback(true)
	if got := r.SelectModel("planning-enabled", "planner", 1, 0.9, "text"); got == nil || got.Modality != "coding" {
		t.Fatal("explicit text-to-coder fallback was not honored")
	}
}

func TestProviderFamilyCooldownCoversCredentialSlots(t *testing.T) {
	ModelsMutex.Lock()
	old := AvailableModels
	AvailableModels = []Model{
		{ID: "openrouter/a", Provider: "openrouter", APIKeyEnv: "KEY_1", Enabled: true},
		{ID: "openrouter/b", Provider: "openrouter", APIKeyEnv: "KEY_2", Enabled: true},
		{ID: "groq/c", Provider: "groq", APIKeyEnv: "GROQ_KEY", Enabled: true},
	}
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels = old; ModelsMutex.Unlock() }()

	r := &ModelRouter{}
	r.PenalizeProviderFamily("openrouter")
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	if AvailableModels[0].CooldownUntil.IsZero() || AvailableModels[1].CooldownUntil.IsZero() {
		t.Fatal("provider credential slot escaped account-wide cooldown")
	}
	if !AvailableModels[2].CooldownUntil.IsZero() {
		t.Fatal("unrelated provider was cooled down")
	}
}

func TestRoutingMatrixIsAtomicallyPersistedAndReportsFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RETICLE_ROOT", root)
	router := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{"agent": {"model": 0.75}}}
	if err := router.saveMatrix(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".reticle", "routing_matrix.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]map[string]float64
	if err := json.Unmarshal(data, &stored); err != nil || stored["agent"]["model"] != 0.75 {
		t.Fatalf("invalid persisted matrix: %v %#v", err, stored)
	}

	blockedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(blockedRoot, ".reticle"), []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RETICLE_ROOT", blockedRoot)
	if err := router.saveMatrix(); err == nil {
		t.Fatal("routing persistence failure was ignored")
	}
}
