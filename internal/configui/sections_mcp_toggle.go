// internal/configui/sections_mcp_toggle.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildMCPFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.MCP
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("config_file", "config file", s.ConfigFile),
	}
}
