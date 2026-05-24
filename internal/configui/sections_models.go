// internal/configui/sections_models.go
package configui

import "github.com/caimlas/meept/internal/llm"

func buildModelsFields() []Field {
	cfg, _ := llm.LoadProvidersConfigDefault()
	if cfg == nil {
		cfg = &llm.ProvidersConfig{}
	}
	return []Field{
		NewTextField("model", "default model", cfg.Model),
		NewTextField("small_model", "small model", cfg.SmallModel),
		NewTextField("disabled_providers", "disabled providers", joinStrings(cfg.DisabledProviders)),
		NewDrilldownField("providers", "providers", len(cfg.Providers)),
	}
}

// joinStrings joins a string slice with ", " for display in a text field.
func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
