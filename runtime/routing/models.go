package routing

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hyperparallel/runtime/logger"
)

// Model Definition
type Model struct {
	ID        string  `json:"id"`
	Cost      float64 `json:"cost"`       // Financial cost
	Capability float64 `json:"capability"` // Estimated performance capability
	APIKeyEnv string  `json:"api_key_env,omitempty"`
	Enabled   bool    `json:"enabled"`
}

func (m Model) Key() string {
	if m.APIKeyEnv != "" {
		return m.ID + "|" + m.APIKeyEnv
	}
	return m.ID
}

var AvailableModels = []Model{}

func estimateCapability(id string) float64 {
	lower := strings.ToLower(id)
	
	re := regexp.MustCompile(`([0-9\.]+)[bm]`)
	matches := re.FindStringSubmatch(lower)
	if len(matches) == 2 {
		if cap, err := strconv.ParseFloat(matches[1], 64); err == nil {
			if cap > 40.0 {
				cap = 40.0 // Cap at 40 so parameter size doesn't artificially outrank state-of-the-art models like Gemini/Claude
			}
			return cap
		}
	}
	
	if strings.Contains(lower, "claude-3.5-sonnet") || strings.Contains(lower, "claude-3-5-sonnet") { return 100.0 }
	if strings.Contains(lower, "gemini-1.5-pro") { return 100.0 }
	if strings.Contains(lower, "gemini-1.5-flash") || strings.Contains(lower, "gemini-3.5-flash") { return 15.0 }
	if strings.Contains(lower, "mini") || strings.Contains(lower, "lite") { return 8.0 }
	if strings.Contains(lower, "compound") { return 20.0 }
	
	return 10.0 // Default
}

// FetchAvailableModels fetches and parses available models dynamically.
func FetchAvailableModels(log *logger.Logger, loadAll bool) {
	AvailableModels = make([]Model, 0)
	
	premiumKeywords := []string{
		"llama-3.3-70b", "llama-3.1-70b", "llama-3.1-405b", "llama-3-70b",
		"qwen-2.5-72b", "qwen-2.5-coder-32b", "qwen3.6-27b", "qwen-2.5-32b",
		"gemini-1.5-pro", "gemini-2.0-pro", "gemini-1.5-flash",
		"claude-3-5-sonnet", "claude-3-opus", "claude-3-5-haiku",
		"gpt-4o", "gpt-4-turbo", "o1", "o3",
	}

	// The default fallback models we know exist
	defaultModels := []Model{
		{ID: "openrouter/liquid/lfm-2.5-2.6b:free", Cost: 0.0, Capability: 2.6, APIKeyEnv: "OPENROUTER_API_KEY", Enabled: true},
		{ID: "openrouter/liquid/lfm-2.5-2.6b:free", Cost: 0.0, Capability: 2.6, APIKeyEnv: "OPENROUTER_API_KEY_2", Enabled: true},
		{ID: "gemini/gemini-3.5-flash-lite", Cost: 0.0, Capability: 8.0, APIKeyEnv: "GEMINI_API_KEY", Enabled: true},
		{ID: "groq/qwen/qwen3.6-27b", Cost: 0.0, Capability: 27.0, APIKeyEnv: "GROQ_API_KEY", Enabled: true},
		{ID: "groq/groq/compound-mini", Cost: 0.0, Capability: 8.0, APIKeyEnv: "GROQ_API_KEY_2", Enabled: true},
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
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
		var orData ORResp
		if b, _ := io.ReadAll(resp.Body); err == nil {
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
			if b, _ := io.ReadAll(authResp.Body); err == nil {
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

		added := 0
		for _, m := range allORModels {
			cost := 0.0
			if p, err := strconv.ParseFloat(m.Pricing.Prompt, 64); err == nil {
				cost += p
			}
			if isFreeKey && cost > 0.0 && !strings.HasSuffix(m.ID, ":free") {
				continue
			}
			if strings.Contains(strings.ToLower(m.ID), "guard") {
				continue
			}
			if !loadAll {
				isPremium := false
				idLower := strings.ToLower(m.ID)
				isBad := strings.Contains(idLower, "canopy") || strings.Contains(idLower, "liquid") || strings.Contains(idLower, "guard") || strings.Contains(idLower, "free")
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

			AvailableModels = append(AvailableModels, Model{
				ID:         "openrouter/" + m.ID,
				Cost:       cost,
				Capability: estimateCapability(m.ID),
				APIKeyEnv:  envKey,
				Enabled:    true,
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
			if b, _ := io.ReadAll(resp.Body); err == nil {
				json.Unmarshal(b, &groqData)
				for _, m := range groqData.Data {
					if strings.Contains(strings.ToLower(m.ID), "guard") {
						continue
					}
					if !loadAll {
						isPremium := false
						idLower := strings.ToLower(m.ID)
						isBad := strings.Contains(idLower, "canopy") || strings.Contains(idLower, "liquid") || strings.Contains(idLower, "guard") || strings.Contains(idLower, "free")
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
					AvailableModels = append(AvailableModels, Model{
						ID:         "groq/" + m.ID,
						Cost:       0.0, // Groq is currently free tier dominated
						Capability: estimateCapability(m.ID),
						APIKeyEnv:  envKey,
						Enabled:    true,
					})
				}
				log.Info("Dynamically loaded Groq models", "key", envKey, "count", len(groqData.Data))
			}
			resp.Body.Close()
		} else {
			log.Error("Failed to fetch Groq models", "key", envKey, "status", resp.StatusCode)
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
			if b, _ := io.ReadAll(resp.Body); err == nil {
				json.Unmarshal(b, &gemData)
				for _, m := range gemData.Models {
					// m.Name is "models/gemini-1.5-flash"
					id := strings.TrimPrefix(m.Name, "models/")
					AvailableModels = append(AvailableModels, Model{
						ID:         "gemini/" + id,
						Cost:       2.0, // Fixed low cost
						Capability: estimateCapability(id),
						APIKeyEnv:  envKey,
						Enabled:    true,
					})
				}
				log.Info("Dynamically loaded Gemini models", "key", envKey, "count", len(gemData.Models))
			}
			resp.Body.Close()
		} else {
			log.Error("Failed to fetch Gemini models", "key", envKey)
		}
	}

	if len(AvailableModels) == 0 {
		log.Error("Failed to load any dynamic models, falling back to defaults")
		AvailableModels = defaultModels
	}
}
