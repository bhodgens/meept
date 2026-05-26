// internal/configui/sections_mcp.go
package configui

import (
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/mcp"
)

func buildMCPServersFields() []Field {
	cfg, _ := config.LoadMCPConfigDefault()
	return []Field{
		NewDrilldownField("servers", "mcp servers", buildMCPServerItems(cfg.Servers)),
	}
}

func buildMCPServerItems(servers []mcp.ServerConfig) []DrilldownItem {
	items := make([]DrilldownItem, 0, len(servers))
	for _, s := range servers {
		serverType := s.Type
		if serverType == "" && len(s.Command) > 0 {
			serverType = "stdio"
		} else if serverType == "" && s.URL != "" {
			serverType = "http"
		}
		fields := []Field{
			NewTextField("name", "name", s.Name),
			NewSelectField("type", "type", serverType, []string{"stdio", "http"}),
			NewTextField("url", "url", s.URL),
			NewTextField("command", "command", joinStrings(s.Command)),
		}
		if len(s.Env) > 0 {
			fields = append(fields, NewMapStringStringDrilldownField("env", "env vars", "env", buildMapStringStringItems(s.Env)))
		}
		if len(s.Headers) > 0 {
			fields = append(fields, NewMapStringStringDrilldownField("headers", "headers", "headers", buildMapStringStringItems(s.Headers)))
		}
		items = append(items, DrilldownItem{Name: s.Name, Fields: fields})
	}
	return items
}
