// internal/configui/sections_compression.go
package configui

func buildCompressionFields() []Field {
	cfg := loadMainConfigOrFallback()
	c := &cfg.Agent.Compression
	return []Field{
		NewToggleField("agent.compression.enabled", "enabled", c.Enabled),
		NewNumberField("agent.compression.min_tokens_to_compress", "min tokens to compress", c.MinTokensToCompress),
		NewSelectField("agent.compression.strategy", "strategy", c.Strategy, []string{"auto", "smart_crusher", "code", "log", "search"}),
		NewDurationField("agent.compression.ttl", "ttl", c.TTL),
		NewToggleField("agent.compression.log_compression", "log compression", c.LogCompression),
		NewToggleField("agent.compression.code_compression", "code compression", c.CodeCompression),
		NewToggleField("agent.compression.search_compression", "search compression", c.SearchCompression),
		NewToggleField("agent.compression.json_compression", "json compression", c.JSONCompression),
		NewToggleField("agent.compression.compress_user_messages", "compress user messages", c.CompressUserMessages),
		NewFloatField("agent.compression.target_ratio", "target ratio", c.TargetRatio),
	}
}
