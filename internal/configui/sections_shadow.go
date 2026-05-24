// internal/configui/sections_shadow.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildShadowFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Shadow
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
	}
}
