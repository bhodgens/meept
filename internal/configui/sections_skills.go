// internal/configui/sections_skills.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildSkillsFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Skills
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewToggleField("auto_reload", "auto reload", s.AutoReload),
		NewNumberField("max_cached_skills", "max cached skills", s.CacheSize),
	}
}
