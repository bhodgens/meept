// internal/configui/sections_queue.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildQueueFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Queue
	return []Field{
		NewTextField("db_path", "db path", s.DBPath),
		NewNumberField("max_retries", "max retries", s.MaxRetries),
	}
}
