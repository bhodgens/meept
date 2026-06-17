// internal/configui/sections_scheduler.go
package configui

func buildSchedulerFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Scheduler
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("timezone", "timezone", s.Timezone),
	}
}
