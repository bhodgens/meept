// internal/configui/sections_workers.go
package configui

func buildWorkersFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Workers
	return []Field{
		NewNumberField("pool_size", "pool size", s.PoolSize),
		NewNumberField("idle_timeout_seconds", "idle timeout seconds", s.IdleTimeoutSeconds),
		NewDrilldownField("default_caps", "default capabilities", buildStringSliceItems("capability", s.DefaultCaps)),
	}
}
