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

func FetchAvailableModels(log *logger.Logger) {
	AvailableModels = []Model{} // Reset

	// The default fallback models we know exist
	defaultModels := []Model{
		{ID: "openrouter/liquid/lfm-2.5-2.6b:free", Cost: 0.0, Capability: 2.6, APIKeyEnv: "OPENROUTER_API_KEY"},
		{ID: "openrouter/liquid/lfm-2.5-2.6b:free", Cost: 0.0, Capability: 2.6, APIKeyEnv: "OPENROUTER_API_KEY_2"},
		{ID: "gemini/gemini-3.5-flash-lite", Cost: 0.0, Capability: 8.0, APIKeyEnv: "GEMINI_API_KEY"},
		{ID: "groq/qwen/qwen3.6-27b", Cost: 0.0, Capability: 27.0, APIKeyEnv: "GROQ_API_KEY"},
		{ID: "groq/groq/compound-mini", Cost: 0.0, Capability: 8.0, APIKeyEnv: "GROQ_API_KEY_2"},
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

	orKeys := []string{"OPENROUTER_API_KEY", "OPENROUTER_API_KEY_2"}
	orFetched := false
	for _, envKey := range orKeys {
		if os.Getenv(envKey) == "" {
			continue
		}
		if !orFetched { // Only fetch once for OpenRouter since all models are the same
			req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
			if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
				var orData ORResp
				if b, _ := io.ReadAll(resp.Body); err == nil {
					json.Unmarshal(b, &orData)
					for _, m := range orData.Data {
						cost := 0.0
						if p, err := strconv.ParseFloat(m.Pricing.Prompt, 64); err == nil {
							cost += p
						}
						// Only load free models if you want to restrict, or load everything:
						// We'll load everything and let Bayesian route by cost
						AvailableModels = append(AvailableModels, Model{
							ID:        "openrouter/" + m.ID,
							Cost:      cost,
							Capability: estimateCapability(m.ID),
							APIKeyEnv: envKey,
						})
					}
					orFetched = true
					log.Info("Dynamically loaded OpenRouter models", "key", envKey, "count", len(orData.Data))
				}
				resp.Body.Close()
			}
		} else {
			// If we already fetched the list, just copy the entries for the second key
			var additional []Model
			for _, m := range AvailableModels {
				if strings.HasPrefix(m.ID, "openrouter/") && m.APIKeyEnv == "OPENROUTER_API_KEY" {
					additional = append(additional, Model{
						ID:        m.ID,
						Cost:      m.Cost,
						Capability: m.Capability,
						APIKeyEnv: envKey,
					})
				}
			}
			AvailableModels = append(AvailableModels, additional...)
			log.Info("Mirrored OpenRouter models for secondary key", "key", envKey)
		}
	}

	// 2. Groq
	type GroqModel struct {
		ID string `json:"id"`
	}
	type GroqResp struct {
		Data []GroqModel `json:"data"`
	}

	groqKeys := []string{"GROQ_API_KEY", "GROQ_API_KEY_2"}
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
					AvailableModels = append(AvailableModels, Model{
						ID:        "groq/" + m.ID,
						Cost:      1.0, // Fixed low cost for Groq
						Capability: estimateCapability(m.ID),
						APIKeyEnv: envKey,
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
						ID:        "gemini/" + id,
						Cost:      2.0, // Fixed low cost
						Capability: estimateCapability(id),
						APIKeyEnv: envKey,
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
