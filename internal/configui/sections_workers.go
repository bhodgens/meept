// internal/configui/sections_workers.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildWorkersFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Workers
	return []Field{
		NewNumberField("pool_size", "pool size", s.PoolSize),
		NewNumberField("idle_timeout_seconds", "idle timeout seconds", s.IdleTimeoutSeconds),
		NewDrilldownField("default_caps", "default capabilities", buildStringSliceItems("capability", s.DefaultCaps)),
	}
}
