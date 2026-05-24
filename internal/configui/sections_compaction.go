// internal/configui/sections_compaction.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildCompactionFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Compaction
	triggerRatioX100 := int(s.TriggerRatio * 100)
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewNumberField("reserve_tokens", "reserve tokens", s.ReserveTokens),
		NewNumberField("keep_recent_tokens", "keep recent tokens", s.KeepRecentTokens),
		NewNumberField("trigger_ratio", "trigger ratio (x100)", triggerRatioX100),
		NewToggleField("iterative_updates", "iterative updates", s.IterativeUpdates),
		NewToggleField("track_file_ops", "track file ops", s.TrackFileOps),
		NewNumberField("timeout_seconds", "timeout seconds", s.TimeoutSeconds),
	}
}
