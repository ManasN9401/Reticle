package routing

// Model Definition
type Model struct {
	ID   string  `json:"id"`
	Cost float64 `json:"cost"` // Abstract cost metric (lower is better/cheaper)
}

// AvailableModels is a hardcoded registry for now, simulating available models
// In a real implementation this might be loaded from a config file.
var AvailableModels = []Model{
	{ID: "groq/llama3-8b-8192", Cost: 1.0},
	{ID: "groq/llama3-70b-8192", Cost: 5.0},
	{ID: "openrouter/openai/gpt-4o", Cost: 50.0},
}
