// internal/configui/sections_calendar.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildCalendarFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Calendar
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("calendar_id", "calendar id", s.CalendarID),
		NewToggleField("reminder_enabled", "reminder enabled", s.ReminderEnabled),
	}
}
