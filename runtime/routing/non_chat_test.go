package routing

import (
	"testing"

	"github.com/reticle/runtime/logger"
)

func TestNonChatModelIDs(t *testing.T) {
	for id, want := range map[string]bool{
		"mistral/mistral-embed":             true,
		"mistral/mistral-embed-2312":        true,
		"openai/text-embedding-3-small":     true,
		"cohere/rerank-v3.5":                true,
		"openai/omni-moderation-latest":     true,
		"mistral/mistral-ocr-latest":        true,
		"groq/whisper-large-v3":             true,
		"openai/gpt-4o-mini-tts":            true,
		"cohere/command-a-plus-05-2026":     false,
		"mistral/magistral-medium-latest":   false,
		"nvidia/nemotron-3-ultra-550b-a55b": false,
		"ollama/embedded-systems-coder:7b":  false,
		"llm7/claude-opus-5-5":              false,
		"openrouter/qwen/qwen3-coder":       false,
		"groq/llama-guard-4-12b":            false,
	} {
		if got := isNonChatModelID(id); got != want {
			t.Errorf("isNonChatModelID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestEmbeddingModelsAreNeverRoutedForChatTasks(t *testing.T) {
	ModelsMutex.Lock()
	old, health := AvailableModels, KeyHealthStates
	AvailableModels = []Model{
		{ID: "mistral/mistral-embed", Provider: "mistral", Enabled: true, Modality: "text", Capability: 10},
		{ID: "mistral/mistral-embed-2312", Provider: "mistral", Enabled: true, Modality: "text", Capability: 10},
		{ID: "cohere/command-a-plus", Provider: "cohere", Enabled: true, Modality: "text", Capability: 10},
	}
	KeyHealthStates = nil
	ModelsMutex.Unlock()
	defer func() { ModelsMutex.Lock(); AvailableModels, KeyHealthStates = old, health; ModelsMutex.Unlock() }()
	r := &ModelRouter{Logger: &logger.Logger{}, Matrix: map[string]map[string]float64{}, ProviderCapacity: map[string]int{}, ProviderInFlight: map[string]int{}}

	// Every effort tier, including the middle one that picked mistral-embed-2312 in practice.
	for tier := 0; tier <= 5; tier++ {
		got := r.SelectModel("task", "agent", tier, .9, "text")
		if got == nil || got.ID != "cohere/command-a-plus" {
			t.Fatalf("tier %d routed %#v", tier, got)
		}
		r.Release("task")
	}
	// The cross-modality fallback must not reintroduce them.
	if got := r.SelectModel("task", "agent", 2, .9, "coding"); got == nil || got.ID != "cohere/command-a-plus" {
		t.Fatalf("coding fallback routed %#v", got)
	}
	r.Release("task")
	if s := r.RouteAvailability("text", RoutePolicy{}); s.Potential != 1 || s.Eligible != 1 {
		t.Fatalf("diagnostics count embedding models as routes: %#v", s)
	}
	ModelsMutex.Lock()
	AvailableModels = AvailableModels[:2]
	ModelsMutex.Unlock()
	if got := r.SelectModel("task", "agent", 2, .9, "text"); got != nil {
		t.Fatalf("an embedding-only catalog still produced a route: %#v", got)
	}
}
