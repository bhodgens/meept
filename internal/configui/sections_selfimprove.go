// internal/configui/sections_selfimprove.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildSelfImproveFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.SelfImprove
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewNumberField("max_iterations_per_cycle", "max iterations per cycle", s.MaxIterationsPerCycle),
		NewNumberField("max_fixes_per_cycle", "max fixes per cycle", s.MaxFixesPerCycle),
	}
}
