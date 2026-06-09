// internal/configui/sections_calendar.go
package configui


func buildCalendarFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Calendar
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("calendar_id", "calendar id", s.CalendarID),
		NewToggleField("reminder_enabled", "reminder enabled", s.ReminderEnabled),
	}
}
