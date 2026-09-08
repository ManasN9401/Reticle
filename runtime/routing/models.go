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
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	ID            string    `json:"id"`
	Cost          float64   `json:"cost"`       // USD per 1M tokens, mean of input/output prices; -1 means unknown
	Capability    float64   `json:"capability"` // Estimated performance capability
	APIKeyEnv     string    `json:"api_key_env,omitempty"`
	Modality      string    `json:"modality,omitempty"`
	Enabled       bool      `json:"enabled"`
}

func (m Model) Key() string {
	if m.APIKeyEnv != "" {
		return m.ID + "|" + m.APIKeyEnv
	}
	return m.ID
}

var (
	AvailableModels []Model
	LockedKeys      []string // Tracks API keys that are rate limited or free-tier only
	ModelsMutex     sync.RWMutex
)

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

// FetchAvailableModels fetches and parses available models dynamically.
func FetchAvailableModels(log *logger.Logger, loadAll bool) {
	newAvailableModels := make([]Model, 0)
	newLockedKeys := make([]string, 0)

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
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	}
	type ORResp struct {
		Data []ORModel `json:"data"`
	}

	orKeys := []string{"OPENROUTER_API_KEY", "OPENROUTER_API_KEY_2", "OPENROUTER_API_KEY_3"}

	// Fetch all OpenRouter models globally once
	var allORModels []ORModel
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models?supported_parameters=tools", nil)
	if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
		var orData ORResp
		if b, err := io.ReadAll(resp.Body); err == nil {
			json.Unmarshal(b, &orData)
			allORModels = orData.Data
		}
		resp.Body.Close()
	}

	for _, envKey := range orKeys {
		if os.Getenv(envKey) == "" {
			continue
		}

		isFreeKey := false
		authReq, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/auth/key", nil)
		authReq.Header.Set("Authorization", "Bearer "+os.Getenv(envKey))
		if authResp, err := client.Do(authReq); err == nil && authResp.StatusCode == 200 {
			type AuthResp struct {
				Data struct {
					IsFreeTier bool     `json:"is_free_tier"`
					Limit      *float64 `json:"limit"`
					Usage      float64  `json:"usage"`
				} `json:"data"`
			}
			var aData AuthResp
			if b, err := io.ReadAll(authResp.Body); err == nil {
				json.Unmarshal(b, &aData)
				if aData.Data.IsFreeTier {
					isFreeKey = true
				}
				if aData.Data.Limit != nil && aData.Data.Usage >= *aData.Data.Limit {
					isFreeKey = true
				}
			}
			authResp.Body.Close()
		}

		if isFreeKey {
			newLockedKeys = append(newLockedKeys, envKey)
		}

		added := 0
		for _, m := range allORModels {
			cost := -1.0
			input, inputErr := strconv.ParseFloat(m.Pricing.Prompt, 64)
			output, outputErr := strconv.ParseFloat(m.Pricing.Completion, 64)
			if inputErr == nil && outputErr == nil && input >= 0 && output >= 0 {
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
					continue
				}
			}

			newAvailableModels = append(newAvailableModels, Model{
				ID:         "openrouter/" + m.ID,
				Cost:       cost,
				Capability: estimateCapability(m.ID),
				APIKeyEnv:  envKey,
				Enabled:    true,
				Modality:   detectModality(m.ID),
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
		if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
			var groqData GroqResp
			if b, err := io.ReadAll(resp.Body); err == nil {
				json.Unmarshal(b, &groqData)
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
			}
			resp.Body.Close()
		} else {
			newLockedKeys = append(newLockedKeys, envKey)
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
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
		if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
			var gemData GemResp
			if b, err := io.ReadAll(resp.Body); err == nil {
				json.Unmarshal(b, &gemData)
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
			}
			resp.Body.Close()
		} else {
			newLockedKeys = append(newLockedKeys, envKey)
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
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
					ID:         "ollama/" + m.Name,
					Cost:       0.0, // Local is free
					Capability: estimateCapability(m.Name),
					APIKeyEnv:  "OLLAMA_HOST",
					Enabled:    true,
					Modality:   detectModality(m.Name),
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

	if len(newAvailableModels) == 0 {
		log.Error("No models discovered; check provider connectivity and credentials")
	}

	ModelsMutex.Lock()
	AvailableModels = newAvailableModels
	LockedKeys = newLockedKeys
	ModelsMutex.Unlock()
}
