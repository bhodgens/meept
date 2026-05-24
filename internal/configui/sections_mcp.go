// internal/configui/sections_mcp.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildMCPServersFields() []Field {
	cfg, _ := config.LoadMCPConfigDefault()
	return []Field{
		NewDrilldownField("servers", "mcp servers", len(cfg.Servers)),
	}
}
