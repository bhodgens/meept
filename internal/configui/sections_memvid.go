// internal/configui/sections_memvid.go
package configui

func buildMemvidFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Memvid
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("endpoint", "endpoint", s.Endpoint),
		NewTextField("data_dir", "data dir", s.DataDir),
		NewNumberField("timeout_seconds", "timeout seconds", s.Timeout),
	}
}
