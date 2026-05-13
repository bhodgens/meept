package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
)

// ProviderConfig represents a provider configuration from models.json5.
type ProviderConfig struct {
	API     string                `json:"api"`
	Options ProviderOptionsConfig `json:"options"`
	Models  map[string]ModelDef   `json:"models"`
}

// ProviderOptionsConfig holds provider-specific options.
type ProviderOptionsConfig struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"` //nolint:gosec // field name, not a secret
	Timeout int    `json:"timeout"`
}

// ModelDef represents a model definition in the config.
type ModelDef struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	InputCost    float64  `json:"input_cost"`
	OutputCost   float64  `json:"output_cost"`
	ContextLimit int      `json:"context_limit"`
	MaxOutput    int      `json:"max_output"`
	Temperature  float64  `json:"temperature"`
}

// ProvidersConfig represents the full models.json5 configuration.
type ProvidersConfig struct {
	Model             string                     `json:"model"`
	SmallModel        string                     `json:"small_model"`
	DisabledProviders []string                   `json:"disabled_providers"`
	ModelAliases      map[string]ModelAliasEntry `json:"model_aliases"`
	Providers         map[string]ProviderConfig  `json:"providers"`
}

// ModelAliasEntry represents a model alias configuration.
type ModelAliasEntry struct {
	Models   []string `json:"models"`    // List of "provider/model-id" in priority order
	Timeout  int      `json:"timeout"`   // Cooldown timeout in seconds after failure
	MaxFails int      `json:"max_fails"` // Max consecutive failures before rotation
}

// envVarPattern matches ${VAR_NAME} or $VAR_NAME patterns.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// LoadProvidersConfig loads providers configuration from a JSON5 file.
func LoadProvidersConfig(path string) (*ProvidersConfig, error) {
	path = pathutil.ExpandPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read providers config: %w", err)
	}

	// Expand environment variables
	content := expandEnvVars(string(data))

	// Strip JSON5 comments
	content = stripJSON5Comments(content)

	var cfg ProvidersConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse providers config: %w", err)
	}

	return &cfg, nil
}

// LoadProvidersConfigDefault loads providers config from the default locations.
func LoadProvidersConfigDefault() (*ProvidersConfig, error) {
	// Try project-local first
	if _, err := os.Stat("config/models.json5"); err == nil {
		return LoadProvidersConfig("config/models.json5")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".meept", "models.json5")
	return LoadProvidersConfig(configPath)
}

// expandEnvVars expands environment variables in a string.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		var varName string
		if strings.HasPrefix(match, "${") {
			varName = match[2 : len(match)-1]
		} else {
			varName = match[1:]
		}

		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return ""
	})
}

// stripJSON5Comments removes // and /* */ comments from JSON5 content.
func stripJSON5Comments(s string) string {
	var result strings.Builder
	inString := false
	inSingleLineComment := false
	inMultiLineComment := false
	i := 0

	for i < len(s) {
		if !inSingleLineComment && !inMultiLineComment {
			if s[i] == '"' && (i == 0 || s[i-1] != '\\') {
				inString = !inString
			}
		}

		if !inString {
			if !inMultiLineComment && i+1 < len(s) && s[i:i+2] == "//" {
				inSingleLineComment = true
				i += 2
				continue
			}

			if inSingleLineComment && s[i] == '\n' {
				inSingleLineComment = false
				result.WriteByte('\n')
				i++
				continue
			}

			if !inSingleLineComment && i+1 < len(s) && s[i:i+2] == "/*" {
				inMultiLineComment = true
				i += 2
				continue
			}

			if inMultiLineComment && i+1 < len(s) && s[i:i+2] == "*/" {
				inMultiLineComment = false
				i += 2
				continue
			}
		}

		if !inSingleLineComment && !inMultiLineComment {
			result.WriteByte(s[i])
		}
		i++
	}

	return result.String()
}

// ResolveModelRef resolves a "provider/model-id" reference to a ModelConfig.
func ResolveModelRef(ref string, cfg *ProvidersConfig) *ModelConfig {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return nil
	}

	providerID := parts[0]
	modelID := parts[1]

	// Check if provider is disabled
	if slices.Contains(cfg.DisabledProviders, providerID) {
		return nil
	}

	provider, ok := cfg.Providers[providerID]
	if !ok {
		return nil
	}

	modelDef, ok := provider.Models[modelID]
	if !ok {
		return nil
	}

	// Build capabilities map
	caps := make(map[string]bool)
	for _, cap := range modelDef.Capabilities {
		caps[cap] = true
	}

	return &ModelConfig{
		BaseURL:              provider.Options.BaseURL,
		ModelID:              modelDef.Name,
		APIKey:               provider.Options.APIKey,
		CostPerMillionInput:  modelDef.InputCost,
		CostPerMillionOutput: modelDef.OutputCost,
		MaxTokens:            modelDef.MaxOutput,
		Temperature:          modelDef.Temperature,
		ContextLimit:         modelDef.ContextLimit,
		Capabilities:         caps,
		ProviderID:           providerID,
	}
}

// GetAllModels returns all available models from the configuration.
func GetAllModels(cfg *ProvidersConfig) []*ModelConfig {
	var models []*ModelConfig

	disabledSet := make(map[string]bool)
	for _, d := range cfg.DisabledProviders {
		disabledSet[d] = true
	}

	for providerID, provider := range cfg.Providers {
		if disabledSet[providerID] {
			continue
		}

		for modelID, modelDef := range provider.Models {
			caps := make(map[string]bool)
			for _, cap := range modelDef.Capabilities {
				caps[cap] = true
			}

			models = append(models, &ModelConfig{
				BaseURL:              provider.Options.BaseURL,
				ModelID:              modelID,
				APIKey:               provider.Options.APIKey,
				CostPerMillionInput:  modelDef.InputCost,
				CostPerMillionOutput: modelDef.OutputCost,
				MaxTokens:            modelDef.MaxOutput,
				Temperature:          modelDef.Temperature,
				ContextLimit:         modelDef.ContextLimit,
				Capabilities:         caps,
				ProviderID:           providerID,
			})
		}
	}

	return models
}
