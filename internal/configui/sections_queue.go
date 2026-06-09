// internal/configui/sections_queue.go
package configui


func buildQueueFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Queue
	return []Field{
		NewTextField("db_path", "db path", s.DBPath),
		NewNumberField("max_retries", "max retries", s.MaxRetries),
	}
}
