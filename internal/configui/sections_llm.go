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
		NewFloatField("llm.budget.aggressiveness", "budget aggressiveness", b.Aggressiveness),
		NewFloatField("llm.broker.max_error_rate", "broker max error rate", br.MaxErrorRate),
		NewNumberField("llm.broker.max_p95_latency_ms", "broker max p95 latency ms", int(br.MaxP95LatencyMS)),
		NewToggleField("llm.broker.fallback_enabled", "broker fallback", br.FallbackEnabled),
		NewToggleField("llm.adaptive_timeout.enabled", "adaptive timeout", at.Enabled),
		NewFloatField("llm.adaptive_timeout.stddev_multiplier", "stddev multiplier", at.StddevMultiplier),
		NewToggleField("llm.adaptive_timeout.stddev_token_rate_timeout", "stddev token rate timeout", at.StddevTokenRateTimeout),
		NewNumberField("llm.adaptive_timeout.min_timeout_seconds", "adaptive min timeout", at.MinTimeoutSeconds),
		NewNumberField("llm.adaptive_timeout.max_timeout_seconds", "adaptive max timeout", at.MaxTimeoutSeconds),
		NewNumberField("llm.adaptive_timeout.warmup_requests", "warmup requests", at.WarmupRequests),
		NewNumberField("llm.adaptive_timeout.window_hours", "window hours", at.WindowHours),
		NewDrilldownField("llm.context_firewall", "context firewall", []DrilldownItem{
			{Name: "context firewall", Fields: []Field{
				NewToggleField("llm.context_firewall.enabled", "enabled", cf.Enabled),
				NewToggleField("llm.context_firewall.summarize_history", "summarize history", cf.SummarizeHistory),
				NewNumberField("llm.context_firewall.small_model_context_threshold", "small model context threshold", cf.SmallModelContextThreshold),
				NewFloatField("llm.context_firewall.iteration_budget_ratio", "iteration budget ratio", cf.IterationBudgetRatio),
				NewFloatField("llm.context_firewall.conversation_budget_ratio", "conversation budget ratio", cf.ConversationBudgetRatio),
				NewToggleField("llm.context_firewall.chunk_large_inputs", "chunk large inputs", cf.ChunkLargeInputs),
				NewFloatField("llm.context_firewall.chunk_threshold_ratio", "chunk threshold ratio", cf.ChunkThresholdRatio),
				NewFloatField("llm.context_firewall.wrap_up_threshold", "wrap up threshold", cf.WrapUpThreshold),
				NewFloatField("llm.context_firewall.hard_limit", "hard limit", cf.HardLimit),
				NewToggleField("llm.context_firewall.drop_context_on_hard_limit", "drop on hard limit", cf.DropContextOnHardLimit),
				NewToggleField("llm.context_firewall.proactive_compression", "proactive compression", cf.ProactiveCompression),
				NewNumberField("llm.context_firewall.model_context_limit", "model context limit", cf.ModelContextLimit),
				NewToggleField("llm.context_firewall.hierarchical_summarization", "hierarchical summarization", cf.HierarchicalSummarization),
				NewNumberField("llm.context_firewall.max_summary_level", "max summary level", cf.MaxSummaryLevel),
				NewNumberField("llm.context_firewall.summary_level_threshold", "summary level threshold", cf.SummaryLevelThreshold),
			}},
		}),
		NewToggleField("llm.metrics.enabled", "metrics", m.Enabled),
		NewTextField("llm.metrics.db_path", "metrics db path", m.DBPath),
		NewNumberField("llm.metrics.retention_days", "retention days", m.RetentionDays),
		NewNumberField("llm.metrics.stats_refresh_minutes", "stats refresh minutes", m.StatsRefreshMinutes),
		NewDrilldownField("llm.cache", "cache", []DrilldownItem{
			{Name: "cache", Fields: []Field{
				NewToggleField("llm.cache.enabled", "enabled", c.Enabled),
				NewNumberField("llm.cache.l1_max_entries", "l1 max entries", c.L1MaxEntries),
				NewToggleField("llm.cache.l2_enabled", "l2 enabled", c.L2Enabled),
				NewTextField("llm.cache.l2_db_path", "l2 db path", c.L2DBPath),
				NewNumberField("llm.cache.default_ttl_min", "default ttl min", c.DefaultTTLMin),
			}},
		}),
	}
}
