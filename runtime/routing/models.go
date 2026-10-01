package routing

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/logger"
)

// Model Definition
type Model struct {
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	ObservedAt     time.Time `json:"observed_at,omitempty"`
	ID             string    `json:"id"`
	RequestModel   string    `json:"request_model,omitempty"`
	Provider       string    `json:"provider"`
	APIBase        string    `json:"api_base,omitempty"`
	EndpointEnv    string    `json:"endpoint_env,omitempty"`
	APIKeyEnv      string    `json:"api_key_env,omitempty"`
	MetadataSource string    `json:"metadata_source"`
	ToolSupport    string    `json:"tool_support"` // advertised, inferred, or unknown
	Modality       string    `json:"modality,omitempty"`
	ContextLimit   int       `json:"context_limit,omitempty"`
	Cost           float64   `json:"cost"` // deprecated mean USD per 1M tokens; -1 means unknown
	InputCostPerM  float64   `json:"input_cost_per_m"`
	OutputCostPerM float64   `json:"output_cost_per_m"`
	Capability     float64   `json:"capability"` // estimated score, not a benchmark
	Enabled        bool      `json:"enabled"`
}

// KeyHealth describes why a configured credential is or is not currently
// usable. It deliberately separates discovery connectivity from credential
// and quota failures so callers never present a local network error as a
// rate-limit event.
type KeyHealth struct {
	Key        string    `json:"key"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	RetryAt    time.Time `json:"retryAt,omitempty"`
}

const (
	KeyHealthy              = "healthy"
	KeyDiscoveryUnreachable = "discovery_unreachable"
	KeyAuthenticationFailed = "authentication_failed"
	KeyQuotaExhausted       = "quota_exhausted"
	KeyRateLimited          = "rate_limited"
	KeyProviderError        = "provider_error"
)

func (m Model) Key() string {
	if m.APIKeyEnv != "" {
		return m.ID + "|" + m.APIKeyEnv
	}
	return m.ID
}

var (
	AvailableModels []Model
	// LockedKeys is retained for older Studio clients. It now contains only
	// credentials known to be unusable, never transient discovery failures.
	LockedKeys      []string
	KeyHealthStates []KeyHealth
	ModelsMutex     sync.RWMutex
)

func keyHealthForDiscovery(key string, status int, err error, observedAt time.Time) KeyHealth {
	health := KeyHealth{Key: key, Status: KeyProviderError, ObservedAt: observedAt}
	switch {
	case err != nil || status == 0:
		health.Status = KeyDiscoveryUnreachable
		if err != nil {
			// Do not expose request URLs here: Gemini credentials are query
			// parameters and health data is broadcast to Studio.
			health.Reason = "provider discovery request could not connect"
		} else {
			health.Reason = "provider discovery did not return an HTTP response"
		}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		health.Status = KeyAuthenticationFailed
		health.Reason = http.StatusText(status)
	case status == http.StatusTooManyRequests:
		health.Status = KeyRateLimited
		health.Reason = http.StatusText(status)
		health.RetryAt = observedAt.Add(60 * time.Second)
	case status >= 500:
		health.Status = KeyProviderError
		health.Reason = http.StatusText(status)
	default:
		health.Status = KeyProviderError
		health.Reason = "model discovery returned HTTP " + strconv.Itoa(status)
	}
	return health
}

func healthLocksKey(status string) bool {
	return status == KeyAuthenticationFailed || status == KeyQuotaExhausted
}

func healthCurrentlyLocksKey(health KeyHealth, now time.Time) bool {
	if health.Status == KeyAuthenticationFailed {
		return true
	}
	if health.Status != KeyQuotaExhausted {
		return false
	}
	return health.RetryAt.IsZero() || now.Before(health.RetryAt)
}

func rebuildLockedKeysLocked() {
	LockedKeys = LockedKeys[:0]
	now := time.Now()
	for _, health := range KeyHealthStates {
		if healthCurrentlyLocksKey(health, now) {
			LockedKeys = append(LockedKeys, health.Key)
		}
	}
}

// SetKeyHealth updates live credential health after a provider response.
func SetKeyHealth(key, status, reason string, retryAt time.Time) {
	if key == "" {
		return
	}
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	health := KeyHealth{Key: key, Status: status, Reason: reason, ObservedAt: time.Now().UTC(), RetryAt: retryAt}
	for i := range KeyHealthStates {
		if KeyHealthStates[i].Key == key {
			KeyHealthStates[i] = health
			rebuildLockedKeysLocked()
			return
		}
	}
	KeyHealthStates = append(KeyHealthStates, health)
	rebuildLockedKeysLocked()
}

// KeyHealthSnapshot returns defensive copies for telemetry and Studio.
func KeyHealthSnapshot() ([]KeyHealth, []string) {
	ModelsMutex.Lock()
	defer ModelsMutex.Unlock()
	rebuildLockedKeysLocked()
	health := append([]KeyHealth(nil), KeyHealthStates...)
	locked := append([]string(nil), LockedKeys...)
	return health, locked
}

// IsConfiguredCredential is the worker-process trust boundary. A task may only
// request an environment secret that belongs to a model admitted by the router.
func IsConfiguredCredential(key string) bool {
	if key == "" {
		return false
	}
	ModelsMutex.RLock()
	defer ModelsMutex.RUnlock()
	for _, model := range AvailableModels {
		if model.APIKeyEnv == key {
			return true
		}
	}
	return false
}

func keyUnavailableLocked(key string) bool {
	now := time.Now()
	for _, health := range KeyHealthStates {
		if health.Key == key && healthCurrentlyLocksKey(health, now) {
			return true
		}
	}
	return false
}

func estimateCapability(id string) float64 {
	lower := strings.ToLower(id)

	re := regexp.MustCompile(`([0-9\.]+)([bm])`)
	matches := re.FindStringSubmatch(lower)
	if len(matches) == 3 {
		if cap, err := strconv.ParseFloat(matches[1], 64); err == nil {
			if matches[2] == "m" {
				cap /= 1000
			}
			if cap > 40.0 {
				cap = 40.0 // Cap at 40 so parameter size doesn't artificially outrank state-of-the-art models like Gemini/Claude
			}
			return cap
		}
	}

	if strings.Contains(lower, "claude-3.5-sonnet") || strings.Contains(lower, "claude-3-5-sonnet") {
		return 100.0
	}
	if strings.Contains(lower, "gemini-1.5-pro") {
		return 100.0
	}
	if strings.Contains(lower, "gemini-1.5-flash") || strings.Contains(lower, "gemini-3.5-flash") {
		return 15.0
	}
	if strings.Contains(lower, "mini") || strings.Contains(lower, "lite") {
		return 8.0
	}
	if strings.Contains(lower, "compound") {
		return 20.0
	}

	return 10.0 // Default
}

func detectModality(id string) string {
	lower := strings.ToLower(id)
	if strings.Contains(lower, "dall-e") || strings.Contains(lower, "midjourney") || strings.Contains(lower, "flux") || strings.Contains(lower, "stable-diffusion") || strings.Contains(lower, "sdxl") {
		return "image"
	}
	if strings.Contains(lower, "coder") || strings.Contains(lower, "code") {
		return "coding"
	}
	return "text"
}

func finalizeModel(model Model, observedAt time.Time) Model {
	model.ObservedAt = observedAt
	if model.InputCostPerM == 0 && model.OutputCostPerM == 0 && model.Cost < 0 {
		model.InputCostPerM, model.OutputCostPerM = -1, -1
	}
	if model.ToolSupport == "" {
		model.ToolSupport = "unknown"
	}
	if model.MetadataSource == "" {
		model.MetadataSource = "runtime-probe"
	}
	if slash := strings.IndexByte(model.ID, '/'); slash > 0 {
		model.Provider = model.ID[:slash]
	}
	switch model.Provider {
	case "ollama":
		model.EndpointEnv = "OLLAMA_HOST"
	case "comfyui":
		model.EndpointEnv = "COMFYUI_HOST"
	case "llama":
		model.EndpointEnv = "LLAMA_HOST"
	}
	return model
}

// ProviderSlot is the congestion-control identity for a model. Local
// providers do not have API-key environment variables, so their endpoint
// variables must keep Ollama, ComfyUI, and other local runtimes independent.
func ProviderSlot(model Model) string {
	if model.APIKeyEnv != "" {
		return model.APIKeyEnv
	}
	if model.EndpointEnv != "" {
		return model.EndpointEnv
	}
	if model.Provider != "" {
		return "provider:" + model.Provider
	}
	return "model:" + model.ID
}

func perMillion(value float64) float64 {
	if value < 0 {
		return -1
	}
	return value * 1_000_000
}

func openRouterKeyAccess(isFreeTier bool, limit *float64, usage float64) (freeOnly, unavailable bool) {
	freeOnly = isFreeTier
	unavailable = limit != nil && usage >= *limit
	return freeOnly, unavailable
}

// FetchAvailableModels fetches and parses available models dynamically.
func FetchAvailableModels(log *logger.Logger, loadAll bool, providerRegistries ...*ProviderRegistry) {
	observedAt := time.Now().UTC()
	ModelsMutex.RLock()
	previousEnabled := make(map[string]bool, len(AvailableModels))
	for _, model := range AvailableModels {
		previousEnabled[model.Key()] = model.Enabled
	}
	ModelsMutex.RUnlock()
	newAvailableModels := make([]Model, 0)
	newLockedKeys := make([]string, 0)
	newKeyHealth := make([]KeyHealth, 0)
	recordHealth := func(health KeyHealth) {
		for i := range newKeyHealth {
			if newKeyHealth[i].Key == health.Key {
				newKeyHealth[i] = health
				return
			}
		}
		newKeyHealth = append(newKeyHealth, health)
	}

	premiumKeywords := []string{
		"llama-3.3-70b", "llama-3.1-70b", "llama-3.1-405b", "llama-3-70b",
		"claude-3-5-sonnet", "claude-3-5-haiku", "claude-3.5-sonnet", "claude-3.5-haiku", "claude-3-opus",
		"gpt-4o", "o1-mini", "o3-mini", "o1-preview",
		"deepseek-chat", "deepseek-coder",
		"qwen-plus", "qwen-max", "qwen-2.5-72b",
		"gemini-1.5-pro", "gemini-2.0-flash",
		"glm", "gemma", "qwen3", "gpt-oss", "compound",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. OpenRouter
	type ORModel struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt     any `json:"prompt"`
			Completion any `json:"completion"`
		} `json:"pricing"`
	}
	type ORResp struct {
		Data []ORModel `json:"data"`
	}

	orKeys := []string{"OPENROUTER_API_KEY", "OPENROUTER_API_KEY_2", "OPENROUTER_API_KEY_3"}

	// Fetch all OpenRouter models globally once
	var allORModels []ORModel
	var orCatalogErr error
	orCatalogStatus := 0
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models?supported_parameters=tools", nil)
	if resp, err := client.Do(req); err == nil {
		orCatalogStatus = resp.StatusCode
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var orData ORResp
			if b, readErr := io.ReadAll(resp.Body); readErr == nil {
				if decodeErr := json.Unmarshal(b, &orData); decodeErr == nil {
					allORModels = orData.Data
				} else {
					orCatalogErr = decodeErr
				}
			} else {
				orCatalogErr = readErr
			}
		}()
	} else {
		orCatalogErr = err
	}

	for _, envKey := range orKeys {
		if os.Getenv(envKey) == "" {
			continue
		}

		if len(allORModels) == 0 {
			recordHealth(keyHealthForDiscovery(envKey, orCatalogStatus, orCatalogErr, observedAt))
			log.Error("Failed to fetch OpenRouter model catalog", "key", envKey, "status", orCatalogStatus, "error", orCatalogErr)
			continue
		}

		isFreeKey := false
		keyUnavailable := false
		authenticated := false
		authReq, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/auth/key", nil)
		authReq.Header.Set("Authorization", "Bearer "+os.Getenv(envKey))
		authStatus := 0
		var authErr error
		if authResp, err := client.Do(authReq); err == nil {
			authStatus = authResp.StatusCode
			func() {
				defer authResp.Body.Close()
				if authResp.StatusCode != http.StatusOK {
					return
				}
				type AuthResp struct {
					Data struct {
						IsFreeTier bool     `json:"is_free_tier"`
						Limit      *float64 `json:"limit"`
						Usage      float64  `json:"usage"`
					} `json:"data"`
				}
				var aData AuthResp
				if b, readErr := io.ReadAll(authResp.Body); readErr == nil {
					if decodeErr := json.Unmarshal(b, &aData); decodeErr == nil {
						isFreeKey, keyUnavailable = openRouterKeyAccess(aData.Data.IsFreeTier, aData.Data.Limit, aData.Data.Usage)
						authenticated = true
					} else {
						authErr = decodeErr
					}
				} else {
					authErr = readErr
				}
			}()
		} else {
			authErr = err
		}

		if !authenticated {
			recordHealth(keyHealthForDiscovery(envKey, authStatus, authErr, observedAt))
			log.Error("Failed to inspect OpenRouter key", "key", envKey, "status", authStatus, "error", authErr)
			continue
		}
		if keyUnavailable {
			recordHealth(KeyHealth{Key: envKey, Status: KeyQuotaExhausted, Reason: "reported usage has reached the key limit", ObservedAt: observedAt, RetryAt: observedAt.Add(15 * time.Minute)})
		} else {
			recordHealth(KeyHealth{Key: envKey, Status: KeyHealthy, ObservedAt: observedAt})
		}

		added := 0
		for _, m := range allORModels {
			cost := -1.0

			parseCost := func(v any) float64 {
				switch val := v.(type) {
				case string:
					f, _ := strconv.ParseFloat(val, 64)
					return f
				case float64:
					return val
				default:
					return -1.0
				}
			}

			input := parseCost(m.Pricing.Prompt)
			output := parseCost(m.Pricing.Completion)

			if input >= 0 && output >= 0 {
				cost = (input + output) * 500000
			}
			if isFreeKey && cost > 0.0 && !strings.HasSuffix(m.ID, ":free") {
				continue
			}

			if !loadAll {
				isPremium := false
				idLower := strings.ToLower(m.ID)
				isBad := strings.Contains(idLower, "canopy") || strings.Contains(idLower, "liquid") || strings.Contains(idLower, "guard")
				if !isBad {
					for _, kw := range premiumKeywords {
						if strings.Contains(idLower, kw) {
							isPremium = true
							break
						}
					}
				}
				if !isPremium {
					// If using a free key, we must allow free models through
					if !isFreeKey || (cost > 0.0 && !strings.HasSuffix(m.ID, ":free")) {
						continue
					}
				}
			}

			newAvailableModels = append(newAvailableModels, Model{
				ID:             "openrouter/" + m.ID,
				Cost:           cost,
				Capability:     estimateCapability(m.ID),
				APIKeyEnv:      envKey,
				Enabled:        true,
				Modality:       detectModality(m.ID),
				InputCostPerM:  perMillion(input),
				OutputCostPerM: perMillion(output),
				MetadataSource: "openrouter-catalog",
				ToolSupport:    "advertised",
			})
			added++
		}
		log.Info("Dynamically loaded OpenRouter models", "key", envKey, "count", added, "free_only", isFreeKey)
	}

	// 2. Groq
	type GroqModel struct {
		ID string `json:"id"`
	}
	type GroqResp struct {
		Data []GroqModel `json:"data"`
	}

	groqKeys := []string{"GROQ_API_KEY", "GROQ_API_KEY_2", "GROQ_API_KEY_3"}
	for _, envKey := range groqKeys {
		keyVal := os.Getenv(envKey)
		if keyVal == "" {
			continue
		}
		req, _ := http.NewRequest("GET", "https://api.groq.com/openai/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+keyVal)
		if resp, err := client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
			func() {
				defer resp.Body.Close()
				var groqData GroqResp
				if b, readErr := io.ReadAll(resp.Body); readErr == nil && json.Unmarshal(b, &groqData) == nil {
					for _, m := range groqData.Data {
						if strings.Contains(strings.ToLower(m.ID), "guard") || strings.Contains(strings.ToLower(m.ID), "compound") || strings.Contains(strings.ToLower(m.ID), "whisper") || strings.Contains(strings.ToLower(m.ID), "orpheus") {
							continue
						}
						if !loadAll {
							isPremium := false
							idLower := strings.ToLower(m.ID)
							isBad := strings.Contains(idLower, "canopy") || strings.Contains(idLower, "liquid") || strings.Contains(idLower, "guard")
							if !isBad {
								for _, kw := range premiumKeywords {
									if strings.Contains(idLower, kw) {
										isPremium = true
										break
									}
								}
							}
							if !isPremium {
								continue
							}
						}
						newAvailableModels = append(newAvailableModels, Model{
							ID:         "groq/" + m.ID,
							Cost:       -1.0, // Price is not supplied by this catalog endpoint
							Capability: estimateCapability(m.ID),
							APIKeyEnv:  envKey,
							Enabled:    true,
							Modality:   detectModality(m.ID),
						})
					}
					log.Info("Dynamically loaded Groq models", "key", envKey, "count", len(groqData.Data))
					recordHealth(KeyHealth{Key: envKey, Status: KeyHealthy, ObservedAt: observedAt})
				} else {
					recordHealth(KeyHealth{Key: envKey, Status: KeyProviderError, Reason: "could not decode provider model catalog", ObservedAt: observedAt})
				}
			}()
		} else {
			status := 0
			if resp != nil {
				status = resp.StatusCode
				resp.Body.Close()
			}
			recordHealth(keyHealthForDiscovery(envKey, status, err, observedAt))
			log.Error("Failed to fetch Groq models", "key", envKey, "status", status, "error", err)
		}
	}

	// 3. Gemini
	type GemModel struct {
		Name string `json:"name"`
	}
	type GemResp struct {
		Models []GemModel `json:"models"`
	}

	geminiKeys := []string{"GEMINI_API_KEY"}
	for _, envKey := range geminiKeys {
		keyVal := os.Getenv(envKey)
		if keyVal == "" {
			continue
		}
		req, _ := http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/models?key="+keyVal, nil)
		if resp, err := client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
			func() {
				defer resp.Body.Close()
				var gemData GemResp
				if b, readErr := io.ReadAll(resp.Body); readErr == nil && json.Unmarshal(b, &gemData) == nil {
					for _, m := range gemData.Models {
						// m.Name is "models/gemini-1.5-flash"
						id := strings.TrimPrefix(m.Name, "models/")
						newAvailableModels = append(newAvailableModels, Model{
							ID:         "gemini/" + id,
							Cost:       -1.0, // Price is not supplied by this catalog endpoint
							Capability: estimateCapability(id),
							APIKeyEnv:  envKey,
							Enabled:    true,
							Modality:   detectModality(id),
						})
					}
					log.Info("Dynamically loaded Gemini models", "key", envKey, "count", len(gemData.Models))
					recordHealth(KeyHealth{Key: envKey, Status: KeyHealthy, ObservedAt: observedAt})
				} else {
					recordHealth(KeyHealth{Key: envKey, Status: KeyProviderError, Reason: "could not decode provider model catalog", ObservedAt: observedAt})
				}
			}()
		} else {
			status := 0
			if resp != nil {
				status = resp.StatusCode
				resp.Body.Close()
			}
			recordHealth(keyHealthForDiscovery(envKey, status, err, observedAt))
			log.Error("Failed to fetch Gemini models", "key", envKey, "status", status, "error", err)
		}
	}

	// 4. Ollama
	type OllamaModel struct {
		Name string `json:"name"`
	}
	type OllamaResp struct {
		Models []OllamaModel `json:"models"`
	}

	ollamaHost := os.Getenv("OLLAMA_HOST")
	if ollamaHost == "" {
		ollamaHost = "http://localhost:11434"
	}

	reqOllama, _ := http.NewRequest("GET", ollamaHost+"/api/tags", nil)
	if resp, err := client.Do(reqOllama); err == nil && resp.StatusCode == 200 {
		var ollamaData OllamaResp
		if b, err := io.ReadAll(resp.Body); err == nil {
			json.Unmarshal(b, &ollamaData)
			for _, m := range ollamaData.Models {
				newAvailableModels = append(newAvailableModels, Model{
					ID:          "ollama/" + m.Name,
					Cost:        0.0, // Local is free
					Capability:  estimateCapability(m.Name),
					EndpointEnv: "OLLAMA_HOST",
					Enabled:     true,
					Modality:    detectModality(m.Name),
				})
			}
			log.Info("Dynamically loaded Ollama models", "host", ollamaHost, "count", len(ollamaData.Models))
		}
		resp.Body.Close()
	} else {
		if resp != nil {
			resp.Body.Close()
		}
		log.Info("Ollama not detected or unreachable, skipping local models", "host", ollamaHost)
	}

	// 5. ComfyUI (Virtual Local Image Generator)
	comfyHost := os.Getenv("COMFYUI_HOST")
	if comfyHost == "" {
		comfyHost = "http://127.0.0.1:8188"
	}
	comfyHost = strings.TrimRight(comfyHost, "/")
	reqComfy, _ := http.NewRequest("GET", comfyHost+"/object_info/CheckpointLoaderSimple", nil)
	if resp, err := client.Do(reqComfy); err == nil && resp.StatusCode == 200 {
		var comfyData map[string]any
		if b, err := io.ReadAll(resp.Body); err == nil {
			json.Unmarshal(b, &comfyData)
			if loader, ok := comfyData["CheckpointLoaderSimple"].(map[string]any); ok {
				if inputs, ok := loader["input"].(map[string]any); ok {
					if required, ok := inputs["required"].(map[string]any); ok {
						if ckptName, ok := required["ckpt_name"].([]any); ok && len(ckptName) > 0 {
							if ckptList, ok := ckptName[0].([]any); ok {
								for _, ckptRaw := range ckptList {
									if ckptStr, ok := ckptRaw.(string); ok {
										newAvailableModels = append(newAvailableModels, Model{
											ID:          "comfyui/" + ckptStr,
											Cost:        0.0,
											Capability:  10.0,
											EndpointEnv: "COMFYUI_HOST",
											Enabled:     true,
											Modality:    "image",
										})
									}
								}
								log.Info("Dynamically loaded ComfyUI models", "host", comfyHost, "count", len(ckptList))
							}
						}
					}
				}
			}
		}
		resp.Body.Close()
	} else {
		if resp != nil {
			resp.Body.Close()
		}
		log.Info("ComfyUI not detected or unreachable; no image model advertised", "host", comfyHost)
	}

	// 6. Generic OpenAI-compatible local server (llama.cpp/llama-server)
	type LlamaModel struct {
		ID string `json:"id"`
	}
	type LlamaResp struct {
		Data []LlamaModel `json:"data"`
	}

	llamaHost := os.Getenv("LLAMA_HOST")
	if llamaHost == "" {
		llamaHost = "http://localhost:8080"
	}
	llamaHost = strings.TrimRight(llamaHost, "/")

	reqLlama, _ := http.NewRequest("GET", llamaHost+"/v1/models", nil)
	llamaKey := os.Getenv("LLAMA_API_KEY")
	if llamaKey != "" {
		reqLlama.Header.Set("Authorization", "Bearer "+llamaKey)
	}
	if resp, err := client.Do(reqLlama); err == nil && resp.StatusCode == 200 {
		var llamaData LlamaResp
		if b, err := io.ReadAll(resp.Body); err == nil {
			json.Unmarshal(b, &llamaData)
			for _, m := range llamaData.Data {
				newAvailableModels = append(newAvailableModels, Model{
					ID:         "llama/" + m.ID,
					Cost:       0.0, // Local is free
					Capability: estimateCapability(m.ID),
					APIKeyEnv:  "LLAMA_API_KEY",
					Enabled:    true,
					Modality:   detectModality(m.ID),
				})
			}
			log.Info("Dynamically loaded Llama models", "host", llamaHost, "count", len(llamaData.Data))
		}
		resp.Body.Close()
	} else {
		if resp != nil {
			resp.Body.Close()
		}
		log.Info("Llama server not detected, unreachable, or auth failed", "host", llamaHost)
	}

	// 7. User-configured OpenAI-compatible providers. Configuration contains
	// only environment-variable names; resolved secrets never enter the registry.
	for _, registry := range providerRegistries {
		if registry == nil {
			continue
		}
		for _, config := range registry.Configs() {
			models, health, status := discoverConfiguredProvider(client, config, observedAt)
			newAvailableModels = append(newAvailableModels, models...)
			if health != nil {
				recordHealth(*health)
			}
			registry.setStatus(status)
			if status.State == "ready" || status.State == "degraded" {
				log.Info("Loaded configured provider models", "provider", config.ID, "count", status.ModelCount, "state", status.State)
			} else if status.State != "disabled" {
				log.Error("Configured provider unavailable", "provider", config.ID, "state", status.State, "error", status.LastError)
			}
		}
	}

	if len(newAvailableModels) == 0 {
		log.Error("No models discovered; check provider connectivity and credentials")
	}

	for i := range newAvailableModels {
		newAvailableModels[i] = finalizeModel(newAvailableModels[i], observedAt)
		if enabled, exists := previousEnabled[newAvailableModels[i].Key()]; exists {
			newAvailableModels[i].Enabled = enabled
		}
	}
	for _, health := range newKeyHealth {
		if healthCurrentlyLocksKey(health, observedAt) {
			newLockedKeys = append(newLockedKeys, health.Key)
		}
	}
	ModelsMutex.Lock()
	AvailableModels = newAvailableModels
	LockedKeys = newLockedKeys
	KeyHealthStates = newKeyHealth
	ModelsMutex.Unlock()
}

func discoverConfiguredProvider(client *http.Client, config ProviderConfig, observedAt time.Time) ([]Model, *KeyHealth, ProviderStatus) {
	status := ProviderStatus{Config: config, State: "disabled"}
	if !config.Enabled {
		return nil, nil, status
	}
	if config.APIKeyEnv != "" && os.Getenv(config.APIKeyEnv) == "" {
		status.State = "missing_credential"
		status.MissingVariables = []string{config.APIKeyEnv}
		status.LastError = "configured credential variable is not set"
		return nil, nil, status
	}

	models := make([]Model, 0, len(config.Models))
	seen := make(map[string]struct{}, len(config.Models))
	appendModel := func(upstreamID, modality string, capability float64, contextLimit int) {
		if _, exists := seen[upstreamID]; exists {
			return
		}
		seen[upstreamID] = struct{}{}
		if modality == "" {
			modality = detectModality(upstreamID)
		}
		if capability == 0 {
			capability = estimateCapability(upstreamID)
		}
		models = append(models, Model{
			ID:             config.ID + "/" + upstreamID,
			RequestModel:   upstreamID,
			APIBase:        config.BaseURL,
			APIKeyEnv:      config.APIKeyEnv,
			Cost:           -1,
			Capability:     capability,
			Enabled:        true,
			Modality:       modality,
			ContextLimit:   contextLimit,
			MetadataSource: "configured-provider",
			ToolSupport:    config.ToolSupport,
		})
	}
	for _, configured := range config.Models {
		appendModel(configured.ID, configured.Modality, configured.Capability, configured.ContextLimit)
	}
	if config.DiscoveryMode == "static-only" {
		status.State = "ready"
		status.ModelCount = len(models)
		if config.APIKeyEnv == "" {
			return models, nil, status
		}
		health := KeyHealth{Key: config.APIKeyEnv, Status: KeyHealthy, ObservedAt: observedAt}
		return models, &health, status
	}

	req, err := http.NewRequest(http.MethodGet, config.BaseURL+config.ModelsPath, nil)
	if err != nil {
		status.State = "invalid"
		status.LastError = "could not construct model discovery request"
		return models, nil, status
	}
	if config.APIKeyEnv != "" {
		req.Header.Set("Authorization", "Bearer "+os.Getenv(config.APIKeyEnv))
	}
	// Provider discovery carries the configured bearer credential. Do not
	// follow redirects: a profile must name the actual API origin and a remote
	// redirect must never receive the secret implicitly.
	providerClient := *client
	providerClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, requestErr := providerClient.Do(req)
	if requestErr != nil {
		status.State = "discovery_unreachable"
		status.LastError = "model discovery request could not connect"
		status.ModelCount = len(models)
		if len(models) > 0 {
			status.State = "degraded"
		}
		if config.APIKeyEnv == "" {
			return models, nil, status
		}
		health := keyHealthForDiscovery(config.APIKeyEnv, 0, requestErr, observedAt)
		return models, &health, status
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		status.State = "provider_error"
		status.LastError = "model discovery returned HTTP " + strconv.Itoa(resp.StatusCode)
		status.ModelCount = len(models)
		if len(models) > 0 {
			status.State = "degraded"
		}
		if config.APIKeyEnv == "" {
			return models, nil, status
		}
		health := keyHealthForDiscovery(config.APIKeyEnv, resp.StatusCode, nil, observedAt)
		return models, &health, status
	}
	type catalogModel struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`
		Endpoints        []string `json:"endpoints"`
		DefaultEndpoints []string `json:"default_endpoints"`
	}
	var catalog struct {
		Data   []catalogModel `json:"data"`
		Models []catalogModel `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&catalog); err != nil {
		status.State = "degraded"
		status.LastError = "provider returned an invalid model catalog"
		status.ModelCount = len(models)
		return models, nil, status
	}
	const maximumDiscoveredModels = 4096
	catalogItems := append(catalog.Data, catalog.Models...)
	for _, item := range catalogItems {
		if len(seen) >= maximumDiscoveredModels {
			status.State = "degraded"
			status.LastError = "provider model catalog exceeded the 4096-model limit"
			break
		}
		upstreamID := strings.TrimSpace(item.ID)
		if upstreamID == "" {
			upstreamID = strings.TrimSpace(item.Name)
		}
		advertisedEndpoints := append(append([]string(nil), item.Endpoints...), item.DefaultEndpoints...)
		if len(advertisedEndpoints) > 0 {
			chatCompatible := false
			for _, endpoint := range advertisedEndpoints {
				normalized := strings.ToLower(strings.Trim(strings.TrimSpace(endpoint), "/"))
				if normalized == "chat" || normalized == "chat/completions" || normalized == "chat-completions" || normalized == "chat_completions" {
					chatCompatible = true
					break
				}
			}
			if !chatCompatible {
				status.ExcludedModelCount++
				continue
			}
		}
		if upstreamID != "" && len(upstreamID) <= 256 && !strings.ContainsAny(upstreamID, "\x00\r\n") {
			appendModel(upstreamID, "", 0, 0)
		}
	}
	if status.State != "degraded" {
		if len(models) == 0 {
			status.State = "degraded"
			if status.ExcludedModelCount > 0 {
				status.LastError = "model catalog advertised no chat-compatible models"
			} else {
				status.LastError = "model catalog returned no usable models"
			}
		} else {
			status.State = "ready"
		}
	}
	status.ModelCount = len(models)
	if config.APIKeyEnv == "" {
		return models, nil, status
	}
	health := KeyHealth{Key: config.APIKeyEnv, Status: KeyHealthy, ObservedAt: observedAt}
	return models, &health, status
}
