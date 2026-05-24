// internal/configui/sections_tooling.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildToolingFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Tooling
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewSelectField("mode", "mode", s.Mode, []string{"service", "agent"}),
		NewToggleField("cache_enabled", "cache enabled", s.CacheEnabled),
	}
}
