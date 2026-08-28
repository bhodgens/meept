package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/pathutil"
	"github.com/tailscale/hujson"
)

// ProviderConfig represents a provider configuration from models.json5.
type ProviderConfig struct {
	API     string                `json:"api"`
	Options ProviderOptionsConfig `json:"options"`
	Models  map[string]ModelDef   `json:"models"`
	// Lifecycle holds local LLM runtime lifecycle configuration (llama.cpp, MLX).
	// Nil means no local runtime lifecycle management for this provider.
	Lifecycle *RuntimeLifecycleConfig `json:"lifecycle,omitempty"`
}

// ProviderOptionsConfig holds provider-specific options.
type ProviderOptionsConfig struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"` //nolint:gosec // field name, not a secret
	Timeout int    `json:"timeout"`
	// ToolConstraint declares the grammar-constraint wire mode every model
	// on this provider supports for tool calls: "llamacpp", "vllm", or
	// "json_schema". Empty (default) = no constraint support. Per-model
	// tool_constraint overrides this value.
	ToolConstraint string `json:"tool_constraint,omitempty"`
	// SchemaMode is the provider-level default tool-schema mode
	// ("full"|"indexed", loop-economics leaf 02). Empty inherits the global
	// [agent.tools].schema_mode. Per-model schema_mode overrides this value.
	// Unknown values are ignored at resolve time (warn + fall through).
	SchemaMode string `json:"schema_mode,omitempty"`
}

// ModelDef represents a model definition in the config.
type ModelDef struct {
	Name           string   `json:"name"`
	Capabilities   []string `json:"capabilities"`
	InputCost      float64  `json:"input_cost"`
	OutputCost     float64  `json:"output_cost"`
	ContextLimit   int      `json:"context_limit"`
	MaxOutput      int      `json:"max_output"`
	Temperature    float64  `json:"temperature"`
	MaxConcurrency int      `json:"max_concurrency"` // Max concurrent requests (0 = unlimited)
	// API overrides the provider transport for this model. Use for image/video
	// models on a chat provider, or for comfyui/gemini/infsh/http backends.
	API             string         `json:"api,omitempty"`
	Workflow        string         `json:"workflow,omitempty"`
	GenerationURL   string         `json:"generation_url,omitempty"`
	BodyTemplate    map[string]any `json:"body_template,omitempty"`
	ResponseURLPath string         `json:"response_url_json_path,omitempty"`
	ResponseB64Path string         `json:"response_b64_json_path,omitempty"`
	ImageApp        string         `json:"image_app,omitempty"`
	VideoApp        string         `json:"video_app,omitempty"`
	// OAuthProvider names an auth registry provider whose stored token is
	// used as the Bearer credential (e.g. "xai-oauth").
	OAuthProvider string `json:"oauth_provider,omitempty"`
	// ToolConstraint overrides the provider-level grammar-constraint wire
	// mode for this model ("llamacpp"|"vllm"|"json_schema"). Empty inherits
	// the provider setting.
	ToolConstraint string `json:"tool_constraint,omitempty"`
	// SchemaMode overrides the provider-level tool-schema mode for this
	// model ("full"|"indexed", loop-economics leaf 02). Empty inherits the
	// provider setting. Unknown values are ignored at resolve time.
	SchemaMode string `json:"schema_mode,omitempty"`
}

// ProvidersConfig represents the full models.json5 configuration.
type ProvidersConfig struct {
	Model             string                     `json:"model"`
	SmallModel        string                     `json:"small_model"`
	ClassifierModel   string                     `json:"classifier_model"`
	SummarizerModel   string                     `json:"summarizer_model"`
	VisionModel       string                     `json:"vision_model"`
	ImageModel        string                     `json:"image_model"`
	VideoModel        string                     `json:"video_model"`
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

	// Standardize JSON5 to strict JSON (comments, trailing commas, unquoted keys)
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON5: %w", err)
	}

	var cfg ProvidersConfig
	if err := json.Unmarshal(stdJSON, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse providers config: %w", err)
	}

	return &cfg, nil
}

// LoadProvidersConfigDefault loads providers config from the default locations.
// Bundled config/models.json5 is the base. ~/.meept/models.json5 overlays it
// (user slots, aliases, and models win). Missing image/video entries in the
// user file still come from the bundled catalog.
func LoadProvidersConfigDefault() (*ProvidersConfig, error) {
	bundled := loadFirstProvidersConfig(bundledModelsPaths())
	homeDir, err := os.UserHomeDir()
	var user *ProvidersConfig
	if err == nil {
		userPath := filepath.Join(homeDir, ".meept", "models.json5")
		if _, statErr := os.Stat(userPath); statErr == nil {
			user, err = LoadProvidersConfig(userPath)
			if err != nil {
				return nil, err
			}
		}
	}
	if bundled == nil && user == nil {
		return nil, fmt.Errorf("models.json5 not found in ~/.meept/ or config/")
	}
	if bundled == nil {
		return user, nil
	}
	if user == nil {
		return bundled, nil
	}
	return MergeProvidersConfig(bundled, user), nil
}

func bundledModelsPaths() []string {
	paths := []string{"config/models.json5"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, "config", "models.json5"),
			filepath.Join(dir, "..", "config", "models.json5"),
		)
	}
	return paths
}

func loadFirstProvidersConfig(paths []string) *ProvidersConfig {
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		cfg, err := LoadProvidersConfig(p)
		if err != nil {
			continue
		}
		return cfg
	}
	return nil
}

// MergeProvidersConfig overlays user config on bundled config.
// Non-empty user slots win. User aliases replace by name.
// User providers merge: new providers are added; existing providers keep
// bundled models unless the user defines the same model id.
func MergeProvidersConfig(base, overlay *ProvidersConfig) *ProvidersConfig {
	if overlay == nil {
		return base
	}
	if base == nil {
		return overlay
	}
	out := *base
	if overlay.Model != "" {
		out.Model = overlay.Model
	}
	if overlay.SmallModel != "" {
		out.SmallModel = overlay.SmallModel
	}
	if overlay.ClassifierModel != "" {
		out.ClassifierModel = overlay.ClassifierModel
	}
	if overlay.SummarizerModel != "" {
		out.SummarizerModel = overlay.SummarizerModel
	}
	if overlay.VisionModel != "" {
		out.VisionModel = overlay.VisionModel
	}
	if overlay.ImageModel != "" {
		out.ImageModel = overlay.ImageModel
	}
	if overlay.VideoModel != "" {
		out.VideoModel = overlay.VideoModel
	}
	if overlay.DisabledProviders != nil {
		out.DisabledProviders = append([]string(nil), overlay.DisabledProviders...)
	}
	if len(overlay.ModelAliases) > 0 {
		aliases := make(map[string]ModelAliasEntry, len(base.ModelAliases)+len(overlay.ModelAliases))
		for k, v := range base.ModelAliases {
			aliases[k] = v
		}
		for k, v := range overlay.ModelAliases {
			aliases[k] = v
		}
		out.ModelAliases = aliases
	}
	providers := make(map[string]ProviderConfig, len(base.Providers)+len(overlay.Providers))
	for k, v := range base.Providers {
		providers[k] = cloneProviderConfig(v)
	}
	for k, v := range overlay.Providers {
		existing, ok := providers[k]
		if !ok {
			providers[k] = cloneProviderConfig(v)
			continue
		}
		providers[k] = mergeProviderConfig(existing, v)
	}
	out.Providers = providers
	return &out
}

func cloneProviderConfig(p ProviderConfig) ProviderConfig {
	out := p
	if p.Models != nil {
		out.Models = make(map[string]ModelDef, len(p.Models))
		for k, v := range p.Models {
			out.Models[k] = v
		}
	}
	return out
}

func mergeProviderConfig(base, overlay ProviderConfig) ProviderConfig {
	out := cloneProviderConfig(base)
	if overlay.API != "" {
		out.API = overlay.API
	}
	if overlay.Options.BaseURL != "" {
		out.Options.BaseURL = overlay.Options.BaseURL
	}
	if overlay.Options.APIKey != "" {
		out.Options.APIKey = overlay.Options.APIKey
	}
	if overlay.Options.Timeout != 0 {
		out.Options.Timeout = overlay.Options.Timeout
	}
	if overlay.Lifecycle != nil {
		out.Lifecycle = overlay.Lifecycle
	}
	if out.Models == nil {
		out.Models = map[string]ModelDef{}
	}
	for id, def := range overlay.Models {
		out.Models[id] = def
	}
	return out
}

// expandEnvVars expands environment variables in a string.
// Uses a regex rather than os.ExpandEnv because configs use both $VAR and
// ${VAR} syntax (os.ExpandEnv only supports the former) and because it skips
// known placeholder variables that are expanded later (e.g. MODEL_PATH).
func expandEnvVars(s string) string {
	// Placeholder variables that should NOT be expanded here
	// They are expanded later by ValidateAndNormalize in runtime_config.go
	placeholderVars := map[string]bool{
		"MODEL_PATH": true,
	}

	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		var varName string
		if strings.HasPrefix(match, "${") {
			varName = match[2 : len(match)-1]
		} else {
			varName = match[1:]
		}

		// Skip placeholder variables - they will be expanded later
		if placeholderVars[varName] {
			return match
		}

		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return ""
	})
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

	return modelConfigFrom(providerID, modelID, provider, modelDef)
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

		for key, modelDef := range provider.Models {
			models = append(models, modelConfigFrom(providerID, key, provider, modelDef))
		}
	}

	return models
}

func modelConfigFrom(providerID, mapKey string, provider ProviderConfig, modelDef ModelDef) *ModelConfig {
	caps := make(map[string]bool)
	for _, capName := range modelDef.Capabilities {
		caps[capName] = true
	}
	// Resolve grammar-constraint mode: per-model override > provider default.
	// A recognized mode also sets the tool_constraint capability so the
	// client's attach path can key off either representation.
	mode := modelDef.ToolConstraint
	if mode == "" {
		mode = provider.Options.ToolConstraint
	}
	if ToolConstraintSupported(mode) {
		caps[CapToolConstraint] = true
	} else if mode != "" {
		slog.Warn("providers: ignoring unknown tool_constraint",
			"provider", providerID, "model", mapKey, "mode", mode)
		mode = ""
	}
	// Resolve tool-schema mode the same way: per-model override > provider
	// default. Unknown values are warned about and cleared so resolution
	// falls through to the global [agent.tools] mode (leaf 02).
	schemaMode := modelDef.SchemaMode
	if schemaMode == "" {
		schemaMode = provider.Options.SchemaMode
	}
	if !SchemaModeValid(schemaMode) {
		slog.Warn("providers: ignoring unknown schema_mode",
			"provider", providerID, "model", mapKey, "mode", schemaMode)
		schemaMode = ""
	}
	name := modelDef.Name
	if name == "" {
		name = mapKey
	}
	opts := provider.Options
	workflow := modelDef.Workflow
	if workflow != "" {
		workflow = pathutil.ExpandPath(workflow)
	}
	return &ModelConfig{
		BaseURL:              opts.BaseURL,
		ModelID:              name,
		APIKey:               opts.APIKey,
		CostPerMillionInput:  modelDef.InputCost,
		CostPerMillionOutput: modelDef.OutputCost,
		MaxTokens:            modelDef.MaxOutput,
		Temperature:          modelDef.Temperature,
		ContextLimit:         modelDef.ContextLimit,
		Capabilities:         caps,
		ProviderID:           providerID,
		CatalogRef:           providerID + "/" + mapKey,
		ToolConstraint:       mode,
		SchemaMode:           schemaMode,
		Timeout:              time.Duration(opts.Timeout) * time.Second,
		MaxConcurrency:       modelDef.MaxConcurrency,
		ProviderAPI:          provider.API,
		GenerationAPI:        modelDef.API,
		Workflow:             workflow,
		GenerationURL:        modelDef.GenerationURL,
		BodyTemplate:         modelDef.BodyTemplate,
		ResponseURLPath:      modelDef.ResponseURLPath,
		ResponseB64Path:      modelDef.ResponseB64Path,
		ImageApp:             modelDef.ImageApp,
		VideoApp:             modelDef.VideoApp,
		OAuthProvider:        modelDef.OAuthProvider,
	}
}
