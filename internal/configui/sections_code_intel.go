// internal/configui/sections_code_intel.go
package configui

import "github.com/caimlas/meept/internal/config"

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
	}
}
