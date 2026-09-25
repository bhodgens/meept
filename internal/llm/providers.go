package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
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
	// ToolChoice is the provider-level default tool_choice policy sent on
	// requests that carry tools AND whose caller marked the turn as an
	// action turn (llm.WithToolChoice). Empty (default) = no tool_choice
	// field is ever sent, so nothing regresses. "required" opts the model
	// into forced tool calls. Per-model tool_choice overrides this value.
	// Unknown values are ignored at resolve time (warn + fall through).
	ToolChoice string `json:"tool_choice,omitempty"`
	// ExtraHeaders are additional HTTP headers sent with every request to
	// this provider (e.g. x-opencode-session for session affinity on the
	// OpenCode Zen/Go gateway). Per-model extra_headers merge over these
	// per key. The sentinel value "${session_id}" is substituted with the
	// current turn's session ID at request time; a header whose value is
	// empty after substitution is omitted.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
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
	TopP           float64  `json:"top_p"`
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
	// model ("full"|"indexed", loop-economics leaf 02). Empty inherits
	// the provider setting. Unknown values are ignored at resolve time.
	SchemaMode string `json:"schema_mode,omitempty"`
	// ToolChoice overrides the provider-level tool_choice policy for this
	// model. Empty inherits the provider setting. See
	// ProviderOptionsConfig.ToolChoice.
	ToolChoice string `json:"tool_choice,omitempty"`
	// ExtraHeaders overrides/extends the provider-level extra HTTP headers
	// for this model (merged per key over the provider map). See
	// ProviderOptionsConfig.ExtraHeaders for the "${session_id}" sentinel.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
}

// ProvidersConfig represents the full models.json5 configuration.
type ProvidersConfig struct {
	Model           string `json:"model"`
	SmallModel      string `json:"small_model"`
	ClassifierModel string `json:"classifier_model"`
	SummarizerModel string `json:"summarizer_model"`
	VisionModel     string `json:"vision_model"`
	ImageModel      string `json:"image_model"`
	VideoModel      string `json:"video_model"`
	// ExtractModel is the models.json5 slot for the json_extract tool's
	// dedicated extraction model (typically a local small LLM). Empty =
	// json_extract reports not-configured.
	ExtractModel string `json:"extract_model"`
	// PlannerModel is the dedicated model for the strategic planner; empty
	// = planner uses its agent default (local 8B).
	PlannerModel string `json:"planner_model"`
	// RefusalModel is the global default refusal fallback target
	// (provider/model ref or alias name; refusal-fallback tree 02).
	// Empty = no global default; a per-agent spec refusal_model overrides
	// this; both empty = refusal fallback disabled for that agent.
	RefusalModel      string                     `json:"refusal_model"`
	DisabledProviders []string                   `json:"disabled_providers"`
	ModelAliases      map[string]ModelAliasEntry `json:"model_aliases"`
	Providers         map[string]ProviderConfig  `json:"providers"`
}

// ModelAliasEntry represents a model alias configuration.
type ModelAliasEntry struct {
	Models                 []string `json:"models"`    // List of "provider/model-id" in priority order
	Timeout                int      `json:"timeout"`   // Cooldown timeout in seconds after failure
	MaxFails               int      `json:"max_fails"` // Max consecutive failures before rotation
	DefaultModel           string   `json:"default_model,omitempty"`
	BalancedStickyRequests bool     `json:"balanced_sticky_requests,omitempty"`
}

// ToolChoiceRequired is the only tool_choice value the request builder acts
// on today: force the model to emit a tool call instead of prose. It is the
// measured lever that fixed the llama.cpp/LFM2.5 narration failure
// (tool_choice auto 15/20, required 20/20 on the failing prompt). Other
// values accepted in config are parsed for forward compatibility but are
// not sent — see resolveToolChoice in client.go.
const ToolChoiceRequired = "required"

// ToolChoiceValid reports whether v is a recognized tool_choice value.
// The empty string is valid and means "no tool_choice field" (the default).
func ToolChoiceValid(v string) bool {
	switch v {
	case "", "auto", "none", ToolChoiceRequired:
		return true
	default:
		return false
	}
}

// envVarPattern matches ${VAR_NAME}, ${VAR_NAME:-default}, or $VAR_NAME
// patterns.
var envVarPattern = regexp.MustCompile(
	`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}` + // ${VAR} or ${VAR:-default}
		`|\$\{([^}]+)\}` + // legacy ${ANYTHING} (MODEL_PATH, session_id)
		`|\$([A-Za-z_][A-Za-z0-9_]*)`) // plain $VAR

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
// Bundled config/models.json5 is the base. The user's models.json5 ($MEEPT_HOME
// when set, else ~/.meept) overlays it (user slots, aliases, and models win).
// Missing image/video entries in the user file still come from the bundled
// catalog.
func LoadProvidersConfigDefault() (*ProvidersConfig, error) {
	bundled := loadFirstProvidersConfig(bundledModelsPaths())
	var user *ProvidersConfig
	if userPath, ok := userModelsConfigPath(); ok {
		var err error
		user, err = LoadProvidersConfig(userPath)
		if err != nil {
			return nil, err
		}
	}
	if bundled == nil && user == nil {
		return nil, fmt.Errorf("models.json5 not found in the meept home or config/")
	}
	if bundled == nil {
		return user, nil
	}
	if user == nil {
		return bundled, nil
	}
	return MergeProvidersConfig(bundled, user), nil
}

// userModelsConfigPath resolves the user's models.json5, honoring MEEPT_HOME.
// It mirrors config.MeeptHome's resolution locally because internal/llm cannot
// import internal/config (cycle via tools/mcp).
//
// os.UserHomeDir().meept alone is wrong: a daemon started with
// MEEPT_HOME=<dir> must read <dir>/models.json5. Without this, an isolated rig
// still loaded the operator's providers and SPAWNED their runtimes - observed
// 2026-09-12, a MEEPT_HOME=/tmp rig brought up the operator's prompt-router
// sidecar and :8081 llama-server. That also mis-registers runtime endpoints,
// so the rig's spawn guard and orphan sweep see the wrong endpoint set.
//
// Returns ("", false) when no user config exists (bundled config still loads).
func userModelsConfigPath() (string, bool) {
	home := strings.TrimSpace(os.Getenv("MEEPT_HOME"))
	if strings.HasPrefix(home, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, home[2:])
		}
	}
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		home = filepath.Join(h, ".meept")
	}
	path := filepath.Join(home, "models.json5")
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
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
	if overlay.ExtractModel != "" {
		out.ExtractModel = overlay.ExtractModel
	}
	if overlay.PlannerModel != "" {
		out.PlannerModel = overlay.PlannerModel
	}
	if overlay.RefusalModel != "" {
		out.RefusalModel = overlay.RefusalModel
	}
	if overlay.DisabledProviders != nil {
		out.DisabledProviders = append([]string(nil), overlay.DisabledProviders...)
	}
	if len(overlay.ModelAliases) > 0 {
		aliases := make(map[string]ModelAliasEntry, len(base.ModelAliases)+len(overlay.ModelAliases))
		maps.Copy(aliases, base.ModelAliases)
		maps.Copy(aliases, overlay.ModelAliases)
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
	// Endpoint ownership: when the overlay (the user's models.json5) declares a
	// provider on the same base URL as a bundled one, the overlay's provider
	// takes that endpoint and the bundled provider is dropped. The bundled
	// entries are templates for a default port (127.0.0.1:8080 for the general
	// local driver), and two providers on one endpoint make the runtime manager
	// pick a winner by registration order - it logged "Conflicting
	// spawn_command for shared endpoint; keeping the first" and silently ran
	// the bundled command from a stale binary. Same-ID merges above are
	// unaffected; only a cross-ID endpoint clash drops the bundled entry.
	dropBundledEndpointDuplicates(base.Providers, overlay.Providers, providers)
	out.Providers = providers
	return &out
}

// dropBundledEndpointDuplicates removes from merged every BASE provider whose
// base URL is also declared by an OVERLAY provider under a different ID, so a
// user-defined provider owns its endpoint. Providers are compared on the
// normalized host:port of Options.BaseURL; entries with an empty BaseURL, with
// the same ID, or with a distinct endpoint are left untouched.
func dropBundledEndpointDuplicates(base, overlay, merged map[string]ProviderConfig) {
	if len(overlay) == 0 {
		return
	}
	claimed := make(map[string]string, len(overlay))
	for id, p := range overlay {
		if key := endpointHostPort(p.Options.BaseURL); key != "" {
			claimed[key] = id
		}
	}
	if len(claimed) == 0 {
		return
	}
	for id, p := range base {
		if _, isOverlay := overlay[id]; isOverlay {
			continue
		}
		key := endpointHostPort(p.Options.BaseURL)
		if key == "" {
			continue
		}
		if _, ok := claimed[key]; ok {
			delete(merged, id)
		}
	}
}

// endpointHostPort normalizes a provider base URL to its host:port, dropping
// the scheme, path and trailing slash. Returns "" for an unparseable or empty
// URL, which callers treat as "not an endpoint owner".
func endpointHostPort(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	trimmed := strings.TrimSpace(baseURL)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	if i := strings.IndexAny(trimmed, "/?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

func cloneProviderConfig(p ProviderConfig) ProviderConfig {
	out := p
	// Deep-copy header maps: out.Options is a value copy but the map
	// headers inside it still alias the source's. A later in-place write
	// through a clone would otherwise mutate the original config.
	if p.Options.ExtraHeaders != nil {
		out.Options.ExtraHeaders = maps.Clone(p.Options.ExtraHeaders)
	}
	if p.Models != nil {
		out.Models = make(map[string]ModelDef, len(p.Models))
		for k, v := range p.Models {
			if v.ExtraHeaders != nil {
				v.ExtraHeaders = maps.Clone(v.ExtraHeaders)
			}
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
	// ExtraHeaders merge per key: overlay entries win, base entries with
	// no overlay counterpart survive (mirrors modelConfigFrom's merge).
	if len(overlay.Options.ExtraHeaders) > 0 {
		merged := make(map[string]string, len(out.Options.ExtraHeaders)+len(overlay.Options.ExtraHeaders))
		maps.Copy(merged, out.Options.ExtraHeaders)
		maps.Copy(merged, overlay.Options.ExtraHeaders)
		out.Options.ExtraHeaders = merged
	}
	if overlay.Lifecycle != nil {
		out.Lifecycle = overlay.Lifecycle
	}
	if out.Models == nil {
		out.Models = map[string]ModelDef{}
	}
	// Copy overlay models with the same deep-copy contract as
	// cloneProviderConfig: a maps.Copy of ModelDef values would leave each
	// overlay model's ExtraHeaders aliasing the overlay source map, and a
	// later in-place write through the merged config would mutate the
	// original provider config.
	for k, v := range overlay.Models {
		if v.ExtraHeaders != nil {
			v.ExtraHeaders = maps.Clone(v.ExtraHeaders)
		}
		out.Models[k] = v
	}
	return out
}

// expandEnvVars expands environment variables in a string.
// Uses a regex rather than os.ExpandEnv because configs use both $VAR and
// ${VAR} syntax (os.ExpandEnv only supports the former) and because it skips
// known placeholder variables that are expanded later (e.g. MODEL_PATH).
//
// Each reference resolves through the shared env-script resolver (see
// envscript.go): process environment first, then the executable `env` script
// in the meept home, then empty (diagnostics name the variable, never the
// value). expandEnvScriptResolver is nil until SetEnvScriptResolver is called
// — tests and callers that have not opted in keep the pure-os.LookupEnv
// behavior exactly.
func expandEnvVars(s string) string {
	// Placeholder variables that should NOT be expanded here
	// They are expanded later by ValidateAndNormalize in runtime_config.go.
	// session_id is a request-time sentinel (see ModelConfig.ExtraHeaders):
	// it must survive load-time expansion so the LLM client can substitute
	// the current turn's session ID per request.
	placeholderVars := map[string]bool{
		"MODEL_PATH": true,
		"session_id": true,
	}

	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		m := envVarPattern.FindStringSubmatch(match)
		var varName, defVal string
		hasDefault := false
		switch {
		case m[1] != "":
			varName = m[1]
			hasDefault = m[2] != ""
			defVal = m[3]
		case m[4] != "":
			varName = m[4]
		case m[5] != "":
			varName = m[5]
		}

		// Skip placeholder variables - they will be expanded later
		if placeholderVars[varName] {
			return match
		}

		var val string
		if expandEnvScriptResolver != nil {
			val, _ = expandEnvScriptResolver.Resolve(varName)
		} else {
			val, _ = os.LookupEnv(varName)
		}
		if val != "" {
			return val
		}
		// Unset or empty: fall back to ${VAR:-default} when the config
		// declares one (e.g. MEEPT_MODELS_DIR defaults to the meept-scoped
		// model store). Plain ${VAR} and $VAR keep the historical behavior:
		// expand to empty.
		if hasDefault {
			return defVal
		}
		return ""
	})
}

// expandEnvScriptResolver is the process-wide env-script resolver used by
// config expansion. Set it once at daemon boot via SetEnvScriptResolver;
// when nil, expansion consults only the process environment (the pre-existing
// behavior, and what unit tests exercise by default).
var expandEnvScriptResolver *envScriptResolver

// SetEnvScriptResolver installs the daemon-wide env-script resolver for
// config expansion. The daemon calls this once at startup; a nil resolver
// (the default) keeps expansion env-only.
func SetEnvScriptResolver(r *envScriptResolver) {
	expandEnvScriptResolver = r
}

// NewEnvScriptResolver builds the env-script resolver for the meept home.
// Exposed for daemon boot wiring and tests.
func NewEnvScriptResolver() *envScriptResolver {
	return newEnvScriptResolver()
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
	// Resolve tool_choice the same way: per-model override > provider
	// default. Unknown values warn and clear so the request builder falls
	// back to sending no tool_choice (the safe default).
	toolChoice := modelDef.ToolChoice
	if toolChoice == "" {
		toolChoice = provider.Options.ToolChoice
	}
	if !ToolChoiceValid(toolChoice) {
		slog.Warn("providers: ignoring unknown tool_choice",
			"provider", providerID, "model", mapKey, "value", toolChoice)
		toolChoice = ""
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
	// Merge extra HTTP headers: per-model entries win per key over the
	// provider-level map (same override direction as tool_constraint).
	// Deep-copy the maps: the provider config is shared and a clone's
	// headers must never alias the source's.
	extraHeaders := make(map[string]string, len(opts.ExtraHeaders)+len(modelDef.ExtraHeaders))
	maps.Copy(extraHeaders, opts.ExtraHeaders)
	maps.Copy(extraHeaders, modelDef.ExtraHeaders)
	return &ModelConfig{
		BaseURL:              opts.BaseURL,
		ModelID:              name,
		APIKey:               opts.APIKey,
		CostPerMillionInput:  modelDef.InputCost,
		CostPerMillionOutput: modelDef.OutputCost,
		MaxTokens:            modelDef.MaxOutput,
		Temperature:          modelDef.Temperature,
		TopP:                 modelDef.TopP,
		ContextLimit:         modelDef.ContextLimit,
		Capabilities:         caps,
		ProviderID:           providerID,
		CatalogRef:           providerID + "/" + mapKey,
		ToolConstraint:       mode,
		SchemaMode:           schemaMode,
		ToolChoice:           toolChoice,
		ExtraHeaders:         extraHeaders,
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
