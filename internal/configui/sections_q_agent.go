// internal/configui/sections_q_agent.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildQAgentFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.QAgent
	confidenceX100 := int(s.MinConfidenceScore * 100)
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewNumberField("session_idle_trigger_hours", "session idle trigger hours", s.SessionIdleTriggerHours),
		NewNumberField("min_sessions_for_pattern", "min sessions for pattern", s.MinSessionsForPattern),
		NewNumberField("min_confidence_score", "min confidence score (x100)", confidenceX100),
	}
}
