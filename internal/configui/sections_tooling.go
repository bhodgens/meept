// internal/configui/sections_tooling.go
package configui

func buildToolingFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Tooling
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewSelectField("mode", "mode", s.Mode, []string{"service", "agent"}),
		NewTextField("agent_id", "agent id", s.AgentID),
		NewTextField("model", "model", s.Model),
		NewToggleField("cache_enabled", "cache enabled", s.CacheEnabled),
		NewNumberField("cache_max_size", "cache max size", s.CacheMaxSize),
		NewNumberField("cache_ttl_minutes", "cache ttl minutes", s.CacheTTLMinutes),
		NewToggleField("include_schema", "include schema", s.IncludeSchema),
		NewToggleField("validate_on_serialize", "validate on serialize", s.ValidateOnSerialize),
		NewToggleField("log_unknown_tools", "log unknown tools", s.LogUnknownTools),
	}
}
