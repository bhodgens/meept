// internal/configui/sections_scheduler.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildSchedulerFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Scheduler
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("timezone", "timezone", s.Timezone),
	}
}
