package config

import (
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
)

// envVarPattern matches ${VAR_NAME} or $VAR_NAME patterns.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Load loads configuration from the specified TOML file.
// Environment variables in the form ${VAR_NAME} or $VAR_NAME are expanded.
// Tilde (~) paths are expanded to the user's home directory.
func Load(path string) (*Config, error) {
	path = expandPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults if config doesn't exist
			cfg := DefaultConfig()
			expandConfigPaths(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Expand environment variables in the raw TOML content
	content := expandEnvVars(string(data))

	cfg := DefaultConfig()
	if err := toml.Unmarshal([]byte(content), cfg); err != nil {
		return nil, wrapTOMLUnmarshalError(err, path)
	}

	// Expand tilde paths in the loaded config
	expandConfigPaths(cfg)

	return cfg, nil
}

// wrapTOMLUnmarshalError provides detailed, user-friendly error messages for TOML unmarshaling failures.
func wrapTOMLUnmarshalError(err error, configPath string) error {
	errMsg := err.Error()

	// Extract line information if available
	var lineInfo string
	if strings.Contains(errMsg, "line:") {
		if idx := strings.Index(errMsg, "line:"); idx != -1 {
			remainder := errMsg[idx:]
			parts := strings.Fields(remainder)
			if len(parts) >= 2 {
				lineInfo = parts[1]
			}
		}
	}

	// Build context-aware error messages
	var detailMsg string
	var hintMsg string

	switch {
	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "into"):
		detailMsg = extractTypeMismatch(errMsg)
		hintMsg = "Hint: Check that the value type matches what the field expects."

	case strings.Contains(errMsg, "unexpected"):
		detailMsg = "unexpected token or syntax issue"
		hintMsg = "Hint: Check for missing quotes, unclosed strings, or invalid TOML syntax."

	case strings.Contains(errMsg, "duplicate"):
		detailMsg = "duplicate field or key"
		hintMsg = "Hint: Each configuration key should appear only once."

	default:
		detailMsg = errMsg
		hintMsg = "Hint: Review the TOML syntax at the reported location."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("failed to parse TOML config %s:\n", configPath))
	if lineInfo != "" {
		sb.WriteString(fmt.Sprintf("  Line: %s\n", lineInfo))
	}
	sb.WriteString(fmt.Sprintf("  Detail: %s\n", detailMsg))
	sb.WriteString(fmt.Sprintf("  %s", hintMsg))

	return fmt.Errorf("%s", sb.String())
}

// LoadDefault loads configuration from the default location.
// Prefers JSON5, falls back to TOML for backward compatibility.
func LoadDefault() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return DefaultConfig(), err
	}

	// Try JSON5 first
	json5Path := filepath.Join(homeDir, ".meept", "meept.json5")
	if _, err := os.Stat(json5Path); err == nil {
		return LoadJSON5Config(json5Path)
	}

	// Fall back to TOML
	tomlPath := filepath.Join(homeDir, ".meept", "meept.toml")
	return Load(tomlPath)
}

// LoadJSON5Config loads configuration from a JSON5 file.
func LoadJSON5Config(path string) (*Config, error) {
	path = expandPath(path)

	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		if os.IsNotExist(err) {
			expandConfigPaths(cfg)
			return cfg, nil
		}
		// Error already includes path from LoadJSON5
		return nil, err
	}

	expandConfigPaths(cfg)
	return cfg, nil
}

// ExpandEnvVars expands environment variables in a string.
// Uses a regex rather than os.ExpandEnv because configs use both $VAR and
// ${VAR} syntax (os.ExpandEnv only supports the former).
// Implements recursion depth limiting to detect cyclic env var references.
func ExpandEnvVars(s string) string {
	const maxPasses = 5
	result := s
	for range maxPasses {
		prev := result
		result = envVarPattern.ReplaceAllStringFunc(result, func(match string) string {
			var varName string
			if strings.HasPrefix(match, "${") {
				varName = match[2 : len(match)-1]
			} else {
				varName = match[1:]
			}

			if val, ok := os.LookupEnv(varName); ok {
				return val
			}
			// Return empty string for undefined variables
			return ""
		})
		// If no more env vars to expand, we're done
		if result == prev {
			break
		}
		// Post-replacement check: if result still contains ${...} patterns
		// after multiple passes, we may have a cycle
		if !envVarPattern.MatchString(result) {
			break
		}
	}
	// Warn if we hit the cap (cycle detected) - caller should log
	if envVarPattern.MatchString(result) {
		slog.Warn("env var expansion hit maxPasses — possible cycle", "input", s)
	}
	return result
}

// expandEnvVars expands environment variables in a string.
// Supports both ${VAR_NAME} and $VAR_NAME syntax.
func expandEnvVars(s string) string {
	return ExpandEnvVars(s)
}

// expandPath expands ~ to the home directory.
func expandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to user.Current
		if u, err := user.Current(); err == nil {
			homeDir = u.HomeDir
		} else {
			return path
		}
	}

	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// expandConfigPaths expands all path fields in the config.
func expandConfigPaths(cfg *Config) {
	cfg.Daemon.SocketPath = expandPath(cfg.Daemon.SocketPath)
	cfg.Daemon.PIDFile = expandPath(cfg.Daemon.PIDFile)
	cfg.Daemon.DataDir = expandPath(cfg.Daemon.DataDir)
	cfg.Memory.DataDir = expandPath(cfg.Memory.DataDir)
	cfg.Queue.DBPath = expandPath(cfg.Queue.DBPath)
	cfg.Isolation.BaseDir = expandPath(cfg.Isolation.BaseDir)
	cfg.MCP.ConfigFile = expandPath(cfg.MCP.ConfigFile)
	cfg.Plugins.Directory = expandPath(cfg.Plugins.Directory)
	cfg.Workspace.BaseDir = expandPath(cfg.Workspace.BaseDir)
	cfg.SelfImprove.DataDir = expandPath(cfg.SelfImprove.DataDir)
	cfg.SelfImprove.Sandbox.WorktreeDir = expandPath(cfg.SelfImprove.Sandbox.WorktreeDir)
	cfg.SelfImprove.Detection.LogFile = expandPath(cfg.SelfImprove.Detection.LogFile)

	// Expand shadow paths
	cfg.Shadow.DataDir = expandPath(cfg.Shadow.DataDir)
	cfg.Shadow.Export.OutputDir = expandPath(cfg.Shadow.Export.OutputDir)
	cfg.Shadow.Adapters.AdapterDir = expandPath(cfg.Shadow.Adapters.AdapterDir)

	// Expand TLS cert/key paths
	cfg.Transport.HTTP.TLSCertFile = expandPath(cfg.Transport.HTTP.TLSCertFile)
	cfg.Transport.HTTP.TLSKeyFile = expandPath(cfg.Transport.HTTP.TLSKeyFile)

	// Expand allowed/blocked paths
	for i, p := range cfg.Security.AllowedPaths {
		cfg.Security.AllowedPaths[i] = expandPath(p)
	}
	for i, p := range cfg.Security.BlockedPaths {
		cfg.Security.BlockedPaths[i] = expandPath(p)
	}
	for i, p := range cfg.SelfImprove.Safety.BlockedPaths {
		cfg.SelfImprove.Safety.BlockedPaths[i] = expandPath(p)
	}

	// Expand additional path fields
	cfg.Projects.BaseDir = expandPath(cfg.Projects.BaseDir)
	cfg.OAuth.TokenDir = expandPath(cfg.OAuth.TokenDir)
	cfg.Bots.DataDir = expandPath(cfg.Bots.DataDir)
}

// ModelsConfig represents the models.json5 configuration structure.
type ModelsConfig struct {
	Model             string              `json:"model"`
	SmallModel        string              `json:"small_model"`
	ClassifierModel   string              `json:"classifier_model"` // Model for intent classification (empty = use model)
	SummarizerModel   string              `json:"summarizer_model"` // Model for session summarization (empty = use model)
	DisabledProviders []string            `json:"disabled_providers"`
	DefaultTimeout    int                 `json:"default_timeout"` // Default timeout in seconds
	Providers         map[string]Provider `json:"providers"`
}

// Provider represents a provider configuration in models.json5.
type Provider struct {
	API     string           `json:"api"`
	Options ProviderOptions  `json:"options"`
	Models  map[string]Model `json:"models"`
}

// ProviderOptions holds provider-specific options.
type ProviderOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"` //nolint:gosec // field name, not a secret
	Timeout int    `json:"timeout"`
	NoAuth  bool   `json:"noAuth"` // FIX #0004 - true for providers that don't require API key (e.g., local LLMs)
}

// Model represents a model configuration.
type Model struct {
	Name             string   `json:"name"`
	Capabilities     []string `json:"capabilities"`
	InputCost        float64  `json:"input_cost"`
	OutputCost       float64  `json:"output_cost"`
	ContextLimit     int      `json:"context_limit"`
	MaxOutput        int      `json:"max_output"`
	Temperature      float64  `json:"temperature"`
	TopP             float64  `json:"top_p,omitempty"`
	FrequencyPenalty float64  `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64  `json:"presence_penalty,omitempty"`
}

// LoadModelsConfig loads models configuration from a JSON5 file.
func LoadModelsConfig(path string) (*ModelsConfig, error) {
	var cfg ModelsConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load models config %s: %w", path, err)
	}

	// Apply default timeout to providers that don't specify one
	if cfg.DefaultTimeout > 0 {
		for providerID, provider := range cfg.Providers {
			if provider.Options.Timeout == 0 {
				provider.Options.Timeout = cfg.DefaultTimeout
				cfg.Providers[providerID] = provider
			}
		}
	}

	return &cfg, nil
}

// LoadModelsConfigDefault loads models config from the default location.
// Priority: user config (~/.meept/models.json5) > project config (config/models.json5)
func LoadModelsConfigDefault() (*ModelsConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Try user config first (FIX #0001 - user config takes precedence)
	userPath := filepath.Join(homeDir, ".meept", "models.json5")
	if _, err := os.Stat(userPath); err == nil {
		return LoadModelsConfig(userPath)
	}

	// Fall back to project-local config
	if _, err := os.Stat("config/models.json5"); err == nil {
		return LoadModelsConfig("config/models.json5")
	}

	return nil, fmt.Errorf("models.json5 not found in ~/.meept/ or config/")
}

// StripJSON5Comments converts JSON5 to strict JSON, handling comments,
// trailing commas, and unquoted keys. It delegates to hujson.Standardize
// for full JSON5 spec compliance.
func StripJSON5Comments(s string) string {
	stdJSON, err := hujson.Standardize([]byte(s))
	if err != nil {
		// Fallback: return input unchanged on parse error
		return s
	}
	return string(stdJSON)
}

// stripJSON5Comments is an alias for StripJSON5Comments.
func stripJSON5Comments(s string) string {
	return StripJSON5Comments(s)
}

// EnsureDataDir creates the data directory if it doesn't exist.
func EnsureDataDir(cfg *Config) error {
	if err := os.MkdirAll(cfg.Daemon.DataDir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	return nil
}
