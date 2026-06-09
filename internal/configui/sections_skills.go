// internal/configui/sections_skills.go
package configui

func buildSkillsFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Skills
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewToggleField("auto_reload", "auto reload", s.AutoReload),
		NewNumberField("max_cached_skills", "max cached skills", s.CacheSize),
	}
}
