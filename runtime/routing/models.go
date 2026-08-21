package routing

// Model Definition
type Model struct {
	ID        string  `json:"id"`
	Cost      float64 `json:"cost"` // Abstract cost metric (lower is better/cheaper)
	APIKeyEnv string  `json:"api_key_env,omitempty"`
}

func (m Model) Key() string {
	if m.APIKeyEnv != "" {
		return m.ID + "|" + m.APIKeyEnv
	}
	return m.ID
}

// AvailableModels is a hardcoded registry for now, simulating available models
// In a real implementation this might be loaded from a config file.
var AvailableModels = []Model{
	{ID: "openrouter/liquid/lfm-2.5-2.6b:free", Cost: 0.0, APIKeyEnv: "OPENROUTER_API_KEY"},
	{ID: "openrouter/liquid/lfm-2.5-2.6b:free", Cost: 0.0, APIKeyEnv: "OPENROUTER_API_KEY_2"},
	{ID: "gemini/gemini-3.5-flash-lite", Cost: 0.0, APIKeyEnv: "GEMINI_API_KEY"},
	{ID: "groq/qwen/qwen3.6-27b", Cost: 0.0, APIKeyEnv: "GROQ_API_KEY"},
	{ID: "groq/groq/compound-mini", Cost: 0.0, APIKeyEnv: "GROQ_API_KEY_2"},
}
