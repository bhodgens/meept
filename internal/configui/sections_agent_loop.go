// internal/configui/sections_agent_loop.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildAgentLoopFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Agent
	return []Field{
		NewToggleField("progress_enabled", "progress enabled", s.ProgressEnabled),
		NewNumberField("progress_interval_seconds", "progress interval seconds", s.ProgressIntervalSeconds),
	}
}
