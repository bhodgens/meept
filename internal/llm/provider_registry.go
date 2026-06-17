package llm

// ProviderTransport defines the API wire protocol type.
type ProviderTransport string

const (
	TransportOpenAIChat        ProviderTransport = "openai_chat"
	TransportAnthropicMessages ProviderTransport = "anthropic_messages"
	TransportCodexResponses    ProviderTransport = "codex_responses"
	TransportBedrockConverse   ProviderTransport = "bedrock_converse"
)

// Provider ID constants used across catalog, registry, and broker.
const (
	ProviderIDAnthropic = "anthropic"
	ProviderIDOpenAI    = "openai"
	ProviderIDOllama    = "ollama"
	ProviderIDZAI       = "zai"
	ProviderIDGoogle    = "google"
	ProviderIDDeepSeek  = "deepseek"
	ProviderIDBedrock   = "bedrock"
)

// Content type constants for API response parsing.
const (
	ContentTypeText     = "text"
	ContentTypeToolUse  = "tool_use"
	ContentTypeThinking = "thinking"
	ContentTypeFunction = "function"

	// Capability strings used in model definitions.
	CapCode       = "code"
	CapReasoning  = "reasoning"
	CapToolUse    = "tool_use"
	CapThinking   = "thinking"
	CapImages     = "images"
	CapStreaming  = "streaming"
	CapCompletion = "completion"
	CapTools      = "tools"

	// Metrics dimension keys.
	KeyLevel = "level"
)

// AuthType defines how authentication is performed.
type AuthType string

const (
	AuthAPIKey        AuthType = "api_key"
	AuthOAuthDevice   AuthType = "oauth_device_code"
	AuthOAuthExternal AuthType = "oauth_external"
	AuthEnvVar        AuthType = "env"
)

// ProviderDef defines a provider with all its metadata.
type ProviderDef struct {
	ID              string            // Canonical provider ID (e.g., "anthropic", "openrouter")
	Name            string            // Human-readable name (e.g., "Anthropic", "OpenRouter")
	Transport       ProviderTransport // API wire protocol
	AuthType        AuthType          // How to authenticate
	APIKeyEnvVar    string            // Environment variable for API key (e.g., "ANTHROPIC_API_KEY")
	BaseURL         string            // Default base URL (empty = use provider default)
	BaseURLOverride string            // Override base URL if needed
	DocURL          string            // Documentation URL
	Supports        []string          // Capabilities: streaming, tools, images, etc.
}

// CanonicalProviders is the master list of supported providers.
var CanonicalProviders = []ProviderDef{
	//nolint:gosec // field name, not a secret
	{
		ID:           ProviderIDAnthropic,
		Name:         "Anthropic",
		Transport:    TransportAnthropicMessages,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "ANTHROPIC_API_KEY",
		BaseURL:      "https://api.anthropic.com",
		DocURL:       "https://docs.anthropic.com",
		Supports:     []string{CapStreaming, CapTools, CapImages, CapThinking},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           "openrouter",
		Name:         "OpenRouter",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "OPENROUTER_API_KEY",
		BaseURL:      "https://openrouter.ai/api/v1",
		DocURL:       "https://openrouter.ai/docs",
		Supports:     []string{CapStreaming, CapTools, CapImages},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           ProviderIDOpenAI,
		Name:         "OpenAI",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "OPENAI_API_KEY",
		BaseURL:      "https://api.openai.com/v1",
		DocURL:       "https://platform.openai.com/docs",
		Supports:     []string{CapStreaming, CapTools, CapImages, "responses"},
	},
	{
		ID:        ProviderIDOllama,
		Name:      "Ollama",
		Transport: TransportOpenAIChat,
		AuthType:  AuthEnvVar,
		BaseURL:   "http://localhost:11434/v1",
		DocURL:    "https://ollama.ai/docs",
		Supports:  []string{CapStreaming, "local"},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           ProviderIDZAI,
		Name:         "Z.ai",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "ZAI_API_KEY",
		BaseURL:      "https://api.z.ai/api/coding/paas/v4",
		DocURL:       "https://docs.z.ai",
		Supports:     []string{CapStreaming, CapTools},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           ProviderIDGoogle,
		Name:         "Google AI",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "GOOGLE_API_KEY",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
		DocURL:       "https://ai.google.dev",
		Supports:     []string{CapStreaming, CapTools, CapImages},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           ProviderIDDeepSeek,
		Name:         "DeepSeek",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "DEEPSEEK_API_KEY",
		BaseURL:      "https://api.deepseek.com/v1",
		DocURL:       "https://platform.deepseek.com/docs",
		Supports:     []string{CapStreaming, CapCode, CapReasoning},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           "xai",
		Name:         "xAI",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "XAI_API_KEY",
		BaseURL:      "https://api.x.ai/v1",
		DocURL:       "https://docs.x.ai",
		Supports:     []string{CapStreaming, CapReasoning},
	},
	//nolint:gosec // field name, not a secret
	{
		ID:           "groq",
		Name:         "Groq",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "GROQ_API_KEY",
		BaseURL:      "https://api.groq.com/openai/v1",
		DocURL:       "https://console.groq.com/docs",
		Supports:     []string{CapStreaming, "fast"},
	},
	{
		ID:           "together",
		Name:         "Together AI",
		Transport:    TransportOpenAIChat,
		AuthType:     AuthAPIKey,
		APIKeyEnvVar: "TOGETHER_API_KEY",
		BaseURL:      "https://api.together.xyz/v1",
		DocURL:       "https://docs.together.ai",
		Supports:     []string{CapStreaming, "models"},
	},
	{
		ID:        ProviderIDBedrock,
		Name:      "AWS Bedrock",
		Transport: TransportBedrockConverse,
		AuthType:  AuthEnvVar,
		BaseURL:   "",
		DocURL:    "https://docs.aws.amazon.com/bedrock",
		Supports:  []string{CapStreaming, CapTools, "aws"},
	},
	{
		ID:        "github-models",
		Name:      "GitHub Models",
		Transport: TransportOpenAIChat,
		AuthType:  AuthOAuthDevice,
		BaseURL:   "https://models.github.ai/inference",
		DocURL:    "https://docs.github.com/en/github-models",
		Supports:  []string{CapStreaming, CapTools, CapCode},
	},
	{
		ID:        "google-oauth",
		Name:      "Google AI (OAuth)",
		Transport: TransportOpenAIChat,
		AuthType:  AuthOAuthDevice,
		BaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai",
		DocURL:    "https://ai.google.dev",
		Supports:  []string{CapStreaming, CapTools, CapImages},
	},
}

// GetProviderByID looks up a provider by its canonical ID.
func GetProviderByID(id string) (*ProviderDef, bool) {
	for _, p := range CanonicalProviders {
		if p.ID == id {
			return &p, true
		}
	}
	return nil, false
}

// GetProviderByEnvVar looks up a provider by its API key environment variable.
func GetProviderByEnvVar(envVar string) (*ProviderDef, bool) {
	for _, p := range CanonicalProviders {
		if p.APIKeyEnvVar == envVar {
			return &p, true
		}
	}
	return nil, false
}

// ListProviders returns all providers, optionally filtered by transport type.
func ListProviders(transport ProviderTransport) []ProviderDef {
	if transport == "" {
		result := make([]ProviderDef, len(CanonicalProviders))
		copy(result, CanonicalProviders)
		return result
	}
	var result []ProviderDef
	for _, p := range CanonicalProviders {
		if p.Transport == transport {
			result = append(result, p)
		}
	}
	return result
}

// ProvidersFromConfig builds ProviderDef entries from a ProvidersConfig (models.json5).
// Each key in cfg.Providers that doesn't match a canonical provider ID is treated
// as a user-defined OpenAI-compatible provider. Canonical providers get their
// BaseURL/APIKey overridden from config if present.
func ProvidersFromConfig(cfg *ProvidersConfig) []ProviderDef {
	if cfg == nil || len(cfg.Providers) == 0 {
		return nil
	}

	// Build lookup of canonical providers for override detection
	canonical := make(map[string]ProviderDef, len(CanonicalProviders))
	for _, p := range CanonicalProviders {
		canonical[p.ID] = p
	}

	// Disabled providers set
	disabled := make(map[string]bool, len(cfg.DisabledProviders))
	for _, d := range cfg.DisabledProviders {
		disabled[d] = true
	}

	var result []ProviderDef
	for id, pc := range cfg.Providers {
		if disabled[id] {
			continue
		}

		if c, ok := canonical[id]; ok {
			// Override base URL / API key from config
			if pc.Options.BaseURL != "" {
				c.BaseURL = pc.Options.BaseURL
			}
			if pc.Options.APIKey != "" {
				c.APIKeyEnvVar = "" // clear env var, use direct key
			}
			result = append(result, c)
			continue
		}

		// User-defined provider — synthesize a ProviderDef
		def := ProviderDef{
			ID:        id,
			Name:      id, // Use the config key as display name
			Transport: TransportOpenAIChat,
			AuthType:  AuthEnvVar,
			BaseURL:   pc.Options.BaseURL,
			Supports:  []string{CapStreaming},
		}
		if pc.Options.APIKey != "" {
			def.AuthType = AuthAPIKey
		}
		// Detect lifecycle (local LLM)
		if pc.Lifecycle != nil {
			def.Supports = append(def.Supports, "local")
		}
		result = append(result, def)
	}

	return result
}
