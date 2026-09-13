// internal/configui/sections_models.go
package configui

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

func buildModelsFields() []Field {
	cfg, _ := llm.LoadProvidersConfigDefault()
	if cfg == nil {
		cfg = &llm.ProvidersConfig{}
	}
	return []Field{
		NewTextField("model", "default model", cfg.Model),
		NewTextField("small_model", "small model", cfg.SmallModel),
		NewTextField("classifier_model", "classifier model", cfg.ClassifierModel),
		NewTextField("summarizer_model", "summarizer model", cfg.SummarizerModel),
		NewTextField("image_model", "image model", cfg.ImageModel),
		NewTextField("video_model", "video model", cfg.VideoModel),
		NewTextField("disabled_providers", "disabled providers", strings.Join(cfg.DisabledProviders, ", ")),
		NewDrilldownField("providers", "providers", buildProviderItems(cfg.Providers)),
	}
}

func buildProviderItems(providers map[string]llm.ProviderConfig) []DrilldownItem {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]DrilldownItem, 0, len(names))
	for _, name := range names {
		p := providers[name]
		fields := []Field{
			NewTextField("api", "api type", p.API),
			NewTextField("options.baseURL", "base url", p.Options.BaseURL),
			NewMaskedField("options.apiKey", "api key", p.Options.APIKey),
			NewNumberField("options.timeout", "timeout", p.Options.Timeout),
		}
		// Lifecycle fields are always surfaced. When no lifecycle block exists
		// on the provider, fields render with zero-value defaults; the save
		// path (save.go saveModelsConfig) initializes provider.Lifecycle on
		// first dirty lifecycle.* field, so a brand-new lifecycle block can
		// be added entirely from the TUI.
		lc := llm.RuntimeLifecycleConfig{}
		if p.Lifecycle != nil {
			lc = *p.Lifecycle
		}
		fields = append(fields, lifecycleFields(lc, p.IsAutoStopOnExit())...)
		items = append(items, DrilldownItem{Name: name, Fields: fields})
	}
	return items
}

// lifecycleFields returns the drilldown fields for a RuntimeLifecycleConfig.
// Field keys mirror the JSON tags so save.go's reflection path resolves them.
//
// autoStopOnExit is the provider's EFFECTIVE value (ProviderConfig.
// IsAutoStopOnExit — false when the provider carries no lifecycle block).
// Rendering lc.AutoStopOnExitOrDefault() on a substituted zero-value block
// reported "on" for every non-local provider (openai/anthropic/…), the
// OPPOSITE of the accessor the same commit added, and toggling it
// materialized a brand-new lifecycle block on save (F82, bughunt 2026-09-12
// wave). Absent-means-true still applies once a lifecycle block EXISTS.
func lifecycleFields(lc llm.RuntimeLifecycleConfig, autoStopOnExit bool) []Field {
	// model_paths rendered as JSON (map[string]string).
	modelPathsJSON := "{}"
	if len(lc.ModelPaths) > 0 {
		if b, err := json.Marshal(lc.ModelPaths); err == nil {
			modelPathsJSON = string(b)
		}
	}
	// spawn_command rendered as space-joined text.
	spawnCmd := strings.Join(lc.SpawnCommand, " ")

	return []Field{
		NewSelectField("lifecycle.runtime", "runtime", lc.Runtime, []string{"llama-cpp", "mlx"}),
		NewToggleField("lifecycle.auto_start", "auto start", lc.AutoStart),
		// Display the provider's EFFECTIVE value (IsAutoStopOnExit): an absent
		// auto_stop_on_exit key inside an EXISTING lifecycle block means true,
		// and a provider with NO lifecycle block means false — matching the
		// daemon's own accessor instead of reporting "on" for every remote
		// provider (F82). Saving without touching the toggle leaves the key
		// absent; a toggle writes an explicit true/false (save.go
		// setStructField).
		NewToggleField("lifecycle.auto_stop_on_exit", "auto stop on exit", autoStopOnExit),
		NewTextField("lifecycle.model_path", "model path (legacy)", lc.ModelPath),
		NewTextField("lifecycle.model_paths", "model paths (json)", modelPathsJSON),
		NewTextField("lifecycle.spawn_command", "spawn command", spawnCmd),
		NewTextField("lifecycle.pid_file", "pid file", lc.PIDFile),
		NewNumberField("lifecycle.spawn_timeout_seconds", "spawn timeout seconds", lc.SpawnTimeout),
		NewTextField("lifecycle.health_check.endpoint", "health endpoint", lc.HealthCheck.Endpoint),
		NewNumberField("lifecycle.health_check.interval_seconds", "health interval seconds", lc.HealthCheck.IntervalSeconds),
		NewNumberField("lifecycle.health_check.timeout_seconds", "health timeout seconds", lc.HealthCheck.TimeoutSeconds),
		NewNumberField("lifecycle.health_check.unhealthy_threshold", "unhealthy threshold", lc.HealthCheck.UnhealthyThreshold),
		NewToggleField("lifecycle.restart_policy.enabled", "restart enabled", lc.RestartPolicy.Enabled),
		NewNumberField("lifecycle.restart_policy.max_attempts", "restart max attempts", lc.RestartPolicy.MaxAttempts),
		NewNumberField("lifecycle.restart_policy.cooldown_seconds", "restart cooldown seconds", lc.RestartPolicy.CooldownSeconds),
		NewNumberField("lifecycle.restart_policy.reset_after_seconds", "restart reset after seconds", lc.RestartPolicy.ResetAfterSeconds),
	}
}
