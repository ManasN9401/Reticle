package routing

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestProviderRegistryPersistsArbitraryCredentialReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".reticle", "providers.json")
	registry, err := NewProviderRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	config := ProviderConfig{
		ID: "llm7", Protocol: "openai-compatible", BaseURL: "https://example.invalid/v1",
		APIKeyEnv: "LLM7_API_KEY", ModelsPath: "/models", Enabled: true,
		Models: []ProviderModelConfig{{ID: "example/model", Modality: "text"}},
	}
	if _, err := registry.Put(config); err != nil {
		t.Fatal(err)
	}
	config.Name = "LLM7"
	if _, err := registry.Put(config); err != nil {
		t.Fatalf("provider update was not atomically replaceable: %v", err)
	}
	reloaded, err := NewProviderRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	items := reloaded.Configs()
	if len(items) != 1 || items[0].APIKeyEnv != "LLM7_API_KEY" || items[0].Name != "LLM7" {
		t.Fatalf("unexpected provider snapshot: %#v", items)
	}
}

func TestConfiguredProviderDiscoversOpenAICompatibleModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/code-model"}]}`))
	}))
	defer server.Close()
	t.Setenv("ARBITRARY_LLM_KEY", "fixture-secret")
	config, err := ValidateProviderConfig(ProviderConfig{
		ID: "fixture", Protocol: "openai-compatible", BaseURL: server.URL + "/v1",
		APIKeyEnv: "ARBITRARY_LLM_KEY", ModelsPath: "/models", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	models, health, status := discoverConfiguredProvider(server.Client(), config, time.Now())
	if status.State != "ready" || status.ModelCount != 1 || health == nil || health.Status != KeyHealthy {
		t.Fatalf("unexpected discovery status: %#v %#v", status, health)
	}
	if len(models) != 1 || models[0].ID != "fixture/vendor/code-model" || models[0].RequestModel != "vendor/code-model" || models[0].APIBase != server.URL+"/v1" || models[0].Modality != "coding" {
		t.Fatalf("unexpected discovered model: %#v", models)
	}
}

func TestConfiguredProviderExcludesAdvertisedNonChatModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"command-chat","endpoints":["chat"]},{"id":"embed-v5","endpoints":["embed"]},{"id":"metadata-unknown"}]}`))
	}))
	defer server.Close()
	config, err := ValidateProviderConfig(ProviderConfig{ID: "fixture", BaseURL: server.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	models, _, status := discoverConfiguredProvider(server.Client(), config, time.Now())
	if status.ModelCount != 2 || status.ExcludedModelCount != 1 {
		t.Fatalf("unexpected filtered discovery status: %#v", status)
	}
	if models[0].RequestModel != "command-chat" || models[1].RequestModel != "metadata-unknown" {
		t.Fatalf("unexpected filtered models: %#v", models)
	}
}

func TestConfiguredProviderStaticOnlySkipsCatalog(t *testing.T) {
	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requested = true
		http.Error(w, "catalog must not be called", http.StatusInternalServerError)
	}))
	defer server.Close()
	config, err := ValidateProviderConfig(ProviderConfig{
		ID: "fixture", BaseURL: server.URL, Enabled: true, DiscoveryMode: "static-only",
		Models: []ProviderModelConfig{{ID: "command-chat", Modality: "text"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	models, _, status := discoverConfiguredProvider(server.Client(), config, time.Now())
	if requested || status.State != "ready" || len(models) != 1 || models[0].RequestModel != "command-chat" {
		t.Fatalf("static-only discovery was not isolated: requested=%v status=%#v models=%#v", requested, status, models)
	}
}

func TestStaticOnlyProviderRequiresAModel(t *testing.T) {
	_, err := ValidateProviderConfig(ProviderConfig{ID: "fixture", BaseURL: "https://example.invalid/v1", Enabled: true, DiscoveryMode: "static-only"})
	if err == nil {
		t.Fatal("empty static-only provider was accepted")
	}
}

func TestProviderValidationRejectsRemotePlainHTTP(t *testing.T) {
	_, err := ValidateProviderConfig(ProviderConfig{ID: "unsafe", BaseURL: "http://example.com/v1", APIKeyEnv: "KEY", Enabled: true})
	if err == nil {
		t.Fatal("remote plaintext provider was accepted")
	}
}

func TestConfiguredProviderDoesNotForwardCredentialThroughRedirect(t *testing.T) {
	targetReached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetReached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/models", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	t.Setenv("REDIRECT_TEST_KEY", "must-not-leak")
	config, err := ValidateProviderConfig(ProviderConfig{
		ID: "redirect", BaseURL: origin.URL, APIKeyEnv: "REDIRECT_TEST_KEY", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, health, status := discoverConfiguredProvider(origin.Client(), config, time.Now())
	if targetReached {
		t.Fatal("provider discovery followed a redirect carrying a credential")
	}
	if status.State != "provider_error" || health == nil || health.Status != KeyProviderError {
		t.Fatalf("unexpected redirect status: %#v %#v", status, health)
	}
}
