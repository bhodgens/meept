// internal/configui/sections_plugins.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildPluginsFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Plugins
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("directory", "directory", s.Directory),
	}
}
