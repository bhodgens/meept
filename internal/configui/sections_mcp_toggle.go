// internal/configui/sections_mcp_toggle.go
package configui

func buildMCPFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.MCP
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("config_file", "config file", s.ConfigFile),
	}
}
