// internal/configui/sections_llm.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildLLMFields() []Field {
	cfg, _ := config.LoadDefault()
	llm := &cfg.LLM
	b := &llm.Budget
	br := &llm.Broker
	at := &llm.AdaptiveTimeout
	cf := &llm.ContextFirewall
	m := &llm.Metrics
	c := &llm.Cache
	return []Field{
		NewNumberField("llm.budget.hourly_token_limit", "hourly token limit", b.HourlyTokenLimit),
		NewNumberField("llm.budget.daily_token_limit", "daily token limit", b.DailyTokenLimit),
		NewNumberField("llm.budget.rate_limit_rpm", "rate limit rpm", b.RateLimitRPM),
		NewNumberField("llm.budget.per_task_token_limit", "per task token limit", b.PerTaskTokenLimit),
		NewNumberField("llm.budget.per_session_token_limit", "per session token limit", b.PerSessionTokenLimit),
		NewNumberField("llm.budget.aggressiveness_int", "budget aggressiveness (x1000)", int(b.Aggressiveness*1000)),
		NewNumberField("llm.broker.max_error_rate", "broker max error rate (x1000)", int(br.MaxErrorRate*1000)),
		NewNumberField("llm.broker.max_p95_latency_ms", "broker max p95 latency ms", int(br.MaxP95LatencyMS)),
		NewToggleField("llm.broker.fallback_enabled", "broker fallback", br.FallbackEnabled),
		NewToggleField("llm.adaptive_timeout.enabled", "adaptive timeout", at.Enabled),
		NewNumberField("llm.adaptive_timeout.min_timeout_seconds", "adaptive min timeout", at.MinTimeoutSeconds),
		NewNumberField("llm.adaptive_timeout.max_timeout_seconds", "adaptive max timeout", at.MaxTimeoutSeconds),
		NewNumberField("llm.adaptive_timeout.warmup_requests", "warmup requests", at.WarmupRequests),
		NewNumberField("llm.adaptive_timeout.window_hours", "window hours", at.WindowHours),
		NewToggleField("llm.context_firewall.enabled", "context firewall", cf.Enabled),
		NewToggleField("llm.context_firewall.summarize_history", "summarize history", cf.SummarizeHistory),
		NewToggleField("llm.context_firewall.chunk_large_inputs", "chunk large inputs", cf.ChunkLargeInputs),
		NewToggleField("llm.context_firewall.drop_context_on_hard_limit", "drop on hard limit", cf.DropContextOnHardLimit),
		NewToggleField("llm.context_firewall.proactive_compression", "proactive compression", cf.ProactiveCompression),
		NewToggleField("llm.metrics.enabled", "metrics", m.Enabled),
		NewTextField("llm.metrics.db_path", "metrics db path", m.DBPath),
		NewNumberField("llm.metrics.retention_days", "retention days", m.RetentionDays),
		NewToggleField("llm.cache.enabled", "cache", c.Enabled),
		NewToggleField("llm.cache.l2_enabled", "cache l2", c.L2Enabled),
	}
}
