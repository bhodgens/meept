// internal/configui/sections_q_agent.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildQAgentFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.QAgent
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewNumberField("session_idle_trigger_hours", "session idle trigger hours", s.SessionIdleTriggerHours),
		NewNumberField("analysis_timeout_minutes", "analysis timeout minutes", s.AnalysisTimeoutMinutes),
		NewNumberField("min_sessions_for_pattern", "min sessions for pattern", s.MinSessionsForPattern),
		NewFloatField("min_confidence_score", "min confidence score", s.MinConfidenceScore),
		NewFloatField("high_error_rate_threshold", "high error rate threshold", s.HighErrorRateThreshold),
		NewFloatField("high_rejection_rate_threshold", "high rejection rate threshold", s.HighRejectionRateThreshold),
		NewFloatField("duration_variance_threshold", "duration variance threshold", s.DurationVarianceThreshold),
		NewToggleField("notify_chat", "notify chat", s.NotifyChat),
		NewToggleField("notify_cli", "notify cli", s.NotifyCLI),
		NewToggleField("notify_menu_bar", "notify menu bar", s.NotifyMenuBar),
		NewTextField("analysis_dir", "analysis dir", s.AnalysisDir),
		NewTextField("outcomes_log", "outcomes log", s.OutcomesLog),
	}
}
