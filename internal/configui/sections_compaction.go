// internal/configui/sections_compaction.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildCompactionFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Compaction
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("model", "model", s.Model),
		NewNumberField("reserve_tokens", "reserve tokens", s.ReserveTokens),
		NewNumberField("keep_recent_tokens", "keep recent tokens", s.KeepRecentTokens),
		NewNumberField("max_response_tokens", "max response tokens", s.MaxResponseTokens),
		NewTextField("summary_format", "summary format", s.SummaryFormat),
		NewFloatField("trigger_ratio", "trigger ratio", s.TriggerRatio),
		NewToggleField("iterative_updates", "iterative updates", s.IterativeUpdates),
		NewToggleField("track_file_ops", "track file ops", s.TrackFileOps),
		NewNumberField("timeout_seconds", "timeout seconds", s.TimeoutSeconds),
	}
}
