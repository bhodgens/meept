// internal/configui/sections_code_intel.go
package configui

import (
	"strings"

	"github.com/caimlas/meept/internal/config"
)

func buildCodeIntelFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.CodeIntel
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewDrilldownField("ast", "ast", []DrilldownItem{
			{Name: "ast", Fields: []Field{
				NewToggleField("ast.cache_enabled", "cache enabled", s.AST.CacheEnabled),
				NewNumberField("ast.cache_max_size", "cache max size", s.AST.CacheMaxSize),
				NewNumberField("ast.cache_ttl_minutes", "cache ttl minutes", s.AST.CacheTTLMinutes),
			}},
		}),
		NewDrilldownField("lsp", "lsp", []DrilldownItem{
			{Name: "lsp", Fields: []Field{
				NewToggleField("lsp.enabled", "enabled", s.LSP.Enabled),
				NewToggleField("lsp.auto_start_servers", "auto start servers", s.LSP.AutoStartServers),
				NewNumberField("lsp.connection_timeout_seconds", "connection timeout seconds", s.LSP.ConnectionTimeoutSeconds),
			}},
		}),
		NewDrilldownField("lsp.servers", "lsp servers", buildLSPServerItems(s.LSP.Servers)),
	}
}

// buildLSPServerItems creates drilldown items from the LSP servers map.
func buildLSPServerItems(servers map[string]config.LSPServerConfig) []DrilldownItem {
	items := make([]DrilldownItem, 0, len(servers))
	for lang, srv := range servers {
		items = append(items, DrilldownItem{
			Name: lang,
			Fields: []Field{
				NewTextField("command", "command", srv.Command),
				NewTextField("args", "args", strings.Join(srv.Args, ", ")),
				NewSelectField("transport", "transport", srv.Transport, []string{"stdio", "tcp"}),
				NewTextField("host", "host", srv.Host),
				NewNumberField("port", "port", srv.Port),
			},
		})
	}
	return items
}
