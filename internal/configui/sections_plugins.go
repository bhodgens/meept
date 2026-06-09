// internal/configui/sections_plugins.go
package configui


func buildPluginsFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Plugins
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("directory", "directory", s.Directory),
	}
}
