// internal/configui/sections_distributed_memory.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildDistributedMemoryFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.DistributedMemory
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewSelectField("mode", "mode", s.Mode, []string{"local", "distributed"}),
	}
}
