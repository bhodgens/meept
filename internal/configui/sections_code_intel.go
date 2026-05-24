// internal/configui/sections_code_intel.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildCodeIntelFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.CodeIntel
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
	}
}
