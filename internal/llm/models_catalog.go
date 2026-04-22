package llm

// ModelCatalogEntry defines a model in the catalog.
type ModelCatalogEntry struct {
	ModelID       string   // Model identifier (e.g., "claude-sonnet-4-6")
	Name          string   // Display name (e.g., "Claude Sonnet 4.6")
	ProviderID    string   // Provider this model belongs to
	ContextWindow int      // Context window size in tokens
	MaxOutput     int      // Max output tokens
	InputCost     float64  // Cost per million input tokens (USD)
	OutputCost    float64  // Cost per million output tokens (USD)
	Capabilities  []string // Model capabilities
}

// ProviderModels maps provider IDs to their model catalogs.
var ProviderModels = map[string][]ModelCatalogEntry{
	"anthropic": {
		{
			ModelID:       "claude-opus-4-7",
			Name:          "Claude Opus 4.7",
			ProviderID:    "anthropic",
			ContextWindow: 200000,
			MaxOutput:     8192,
			InputCost:     15.0,
			OutputCost:    75.0,
			Capabilities:  []string{"completion", "code", "reasoning", "tool_use", "thinking"},
		},
		{
			ModelID:       "claude-sonnet-4-6",
			Name:          "Claude Sonnet 4.6",
			ProviderID:    "anthropic",
			ContextWindow: 200000,
			MaxOutput:     8192,
			InputCost:     3.0,
			OutputCost:    15.0,
			Capabilities:  []string{"completion", "code", "reasoning", "tool_use"},
		},
		{
			ModelID:       "claude-haiku-4-5-20251001",
			Name:          "Claude Haiku 4.5",
			ProviderID:    "anthropic",
			ContextWindow: 200000,
			MaxOutput:     4096,
			InputCost:     1.0,
			OutputCost:    5.0,
			Capabilities:  []string{"completion", "code", "reasoning"},
		},
	},
	"openai": {
		{
			ModelID:       "gpt-5.4",
			Name:          "GPT-5.4",
			ProviderID:    "openai",
			ContextWindow: 128000,
			MaxOutput:     16384,
			InputCost:     2.5,
			OutputCost:    10.0,
			Capabilities:  []string{"completion", "code", "reasoning", "tool_use", "images"},
		},
		{
			ModelID:       "gpt-4.1-mini",
			Name:          "GPT-4.1 Mini",
			ProviderID:    "openai",
			ContextWindow: 128000,
			MaxOutput:     8192,
			InputCost:     0.5,
			OutputCost:    2.0,
			Capabilities:  []string{"completion", "code", "reasoning"},
		},
	},
	"openrouter": {
		{
			ModelID:       "anthropic/claude-sonnet-4.6",
			Name:          "Claude Sonnet 4.6 (via OpenRouter)",
			ProviderID:    "openrouter",
			ContextWindow: 200000,
			MaxOutput:     8192,
			InputCost:     3.0,
			OutputCost:    15.0,
			Capabilities:  []string{"completion", "code", "reasoning", "tool_use"},
		},
	},
	"ollama": {
		{
			ModelID:       "llama3.2",
			Name:          "Llama 3.2",
			ProviderID:    "ollama",
			ContextWindow: 128000,
			MaxOutput:     4096,
			InputCost:     0.0,
			OutputCost:    0.0,
			Capabilities:  []string{"code", "tool_use", "reasoning"},
		},
		{
			ModelID:       "qwen2.5-coder",
			Name:          "Qwen 2.5 Coder",
			ProviderID:    "ollama",
			ContextWindow: 32768,
			MaxOutput:     8192,
			InputCost:     0.0,
			OutputCost:    0.0,
			Capabilities:  []string{"code", "tool_use"},
		},
	},
	"zai": {
		{
			ModelID:       "glm-4.7",
			Name:          "GLM-4.7",
			ProviderID:    "zai",
			ContextWindow: 128000,
			MaxOutput:     8192,
			InputCost:     0.0,
			OutputCost:    0.0,
			Capabilities:  []string{"completion", "code", "reasoning", "tool_use"},
		},
		{
			ModelID:       "glm-4.5-air",
			Name:          "GLM-4.5 Air",
			ProviderID:    "zai",
			ContextWindow: 32000,
			MaxOutput:     4096,
			InputCost:     0.0,
			OutputCost:    0.0,
			Capabilities:  []string{"completion", "code", "reasoning"},
		},
	},
	"google": {
		{
			ModelID:       "gemini-2.5-pro",
			Name:          "Gemini 2.5 Pro",
			ProviderID:    "google",
			ContextWindow: 2000000,
			MaxOutput:     65536,
			InputCost:     1.25,
			OutputCost:    10.0,
			Capabilities:  []string{"completion", "code", "reasoning", "tool_use", "images"},
		},
		{
			ModelID:       "gemini-2.5-flash",
			Name:          "Gemini 2.5 Flash",
			ProviderID:    "google",
			ContextWindow: 1000000,
			MaxOutput:     65536,
			InputCost:     0.075,
			OutputCost:    1.0,
			Capabilities:  []string{"completion", "code", "reasoning"},
		},
	},
	"deepseek": {
		{
			ModelID:       "deepseek-chat",
			Name:          "DeepSeek Chat",
			ProviderID:    "deepseek",
			ContextWindow: 64000,
			MaxOutput:     8192,
			InputCost:     0.27,
			OutputCost:    1.1,
			Capabilities:  []string{"completion", "code", "reasoning"},
		},
		{
			ModelID:       "deepseek-coder",
			Name:          "DeepSeek Coder",
			ProviderID:    "deepseek",
			ContextWindow: 128000,
			MaxOutput:     8192,
			InputCost:     0.27,
			OutputCost:    1.1,
			Capabilities:  []string{"code", "tool_use"},
		},
	},
	"xai": {
		{
			ModelID:       "grok-3",
			Name:          "Grok 3",
			ProviderID:    "xai",
			ContextWindow: 128000,
			MaxOutput:     8192,
			InputCost:     5.0,
			OutputCost:    15.0,
			Capabilities:  []string{"completion", "reasoning"},
		},
	},
	"groq": {
		{
			ModelID:       "llama-3.3-70b",
			Name:          "Llama 3.3 70B",
			ProviderID:    "groq",
			ContextWindow: 128000,
			MaxOutput:     32768,
			InputCost:     0.59,
			OutputCost:    0.79,
			Capabilities:  []string{"completion", "code", "reasoning", "fast"},
		},
	},
	"together": {
		{
			ModelID:       "llama-3.3-70b-instruct",
			Name:          "Llama 3.3 70B Instruct",
			ProviderID:    "together",
			ContextWindow: 128000,
			MaxOutput:     4096,
			InputCost:     0.88,
			OutputCost:    0.88,
			Capabilities:  []string{"completion", "code", "reasoning"},
		},
	},
}

// GetModelsForProvider returns all models for a provider.
func GetModelsForProvider(providerID string) ([]ModelCatalogEntry, bool) {
	models, ok := ProviderModels[providerID]
	return models, ok
}

// GetModel returns a specific model by provider and model ID.
func GetModel(providerID, modelID string) (*ModelCatalogEntry, bool) {
	models, ok := ProviderModels[providerID]
	if !ok {
		return nil, false
	}
	for _, m := range models {
		if m.ModelID == modelID {
			return &m, true
		}
	}
	return nil, false
}

// GetAllCatalogModels returns all models across all providers.
func GetAllCatalogModels() []ModelCatalogEntry {
	var all []ModelCatalogEntry
	for _, models := range ProviderModels {
		all = append(all, models...)
	}
	return all
}
