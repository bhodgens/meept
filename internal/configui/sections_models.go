// internal/configui/sections_models.go
package configui

import (
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
		items = append(items, DrilldownItem{Name: name, Fields: fields})
	}
	return items
}
