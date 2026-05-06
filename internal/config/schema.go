// Package config provides configuration loading and validation for meept.
package config

import (
	"log/slog"
	"time"
)

// Config is the root configuration structure loaded from meept.toml.
//
//gendoc:section config
//gendoc:desc Root configuration structure containing all subsystem configurations.
//gendoc:example [config] daemon.socket_path = "~/.meept/meept.sock"
type Config struct {
	Daemon            DaemonConfig            `toml:"daemon" json:"daemon"`
	LLM               LLMConfig               `toml:"llm" json:"llm"`
	Memory            MemoryConfig            `toml:"memory" json:"memory"`
	Memvid            MemvidConfig            `toml:"memvid" json:"memvid"`
	MultiAgent        MultiAgentConfig        `toml:"multiagent" json:"multiagent"`
	Agents            AgentsConfig            `toml:"agents" json:"agents"`
	Agent             AgentConfig             `toml:"agent" json:"agent"`
	Security          SecurityConfig          `toml:"security" json:"security"`
	Scheduler         SchedulerConfig         `toml:"scheduler" json:"scheduler"`
	Queue             QueueConfig             `toml:"queue" json:"queue"`
	Workers           WorkersConfig           `toml:"workers" json:"workers"`
	Isolation         IsolationConfig         `toml:"isolation" json:"isolation"`
	Telegram          TelegramConfig          `toml:"telegram" json:"telegram"`
	Web               WebConfig               `toml:"web" json:"web"`
	MCP               MCPConfig               `toml:"mcp" json:"mcp"`
	Plugins           PluginsConfig           `toml:"plugins" json:"plugins"`
	Workspace         WorkspaceConfig         `toml:"workspace" json:"workspace"`
	Skills            SkillsConfig            `toml:"skills" json:"skills"`
	SelfImprove       SelfImproveConfig       `toml:"selfimprove" json:"selfimprove"`
	Orchestrator      OrchestratorConfig      `toml:"orchestrator" json:"orchestrator"`
	Shadow            ShadowConfig            `toml:"shadow" json:"shadow"`
	DistributedMemory DistributedMemoryConfig `toml:"distributed_memory" json:"distributed_memory"`
	QAgent            QAgentConfig            `toml:"q_agent" json:"q_agent"`
	CodeIntel         CodeIntelConfig         `toml:"code_intel" json:"code_intel"`
	Calendar          CalendarConfig          `toml:"calendar" json:"calendar"`
}

// CalendarConfig holds Google Calendar integration settings.
type CalendarConfig struct {
	// Enabled turns on Google Calendar integration
	Enabled bool `toml:"enabled" json:"enabled"`
	// ClientID is the Google OAuth2 client ID (supports ${ENV_VAR} expansion)
	ClientID string `toml:"client_id" json:"client_id"`
	// ClientSecret is the Google OAuth2 client secret (supports ${ENV_VAR} expansion)
	ClientSecret string `toml:"client_secret" json:"client_secret"`
	// CalendarID is the Google Calendar ID (default: "primary")
	CalendarID string `toml:"calendar_id" json:"calendar_id"`
	// RedirectURI is the OAuth2 redirect URI (default: "http://localhost:8888/callback")
	RedirectURI string `toml:"redirect_uri" json:"redirect_uri"`
	// ReminderEnabled turns on the reminder watcher for upcoming events
	ReminderEnabled bool `toml:"reminder_enabled" json:"reminder_enabled"`
	// ReminderCheckInterval is how often to check for upcoming events (default: 5m)
	ReminderCheckInterval string `toml:"reminder_check_interval" json:"reminder_check_interval"`
	// ReminderAdvanceMinutes triggers reminders this many minutes before an event
	ReminderAdvanceMinutes int `toml:"reminder_advance_minutes" json:"reminder_advance_minutes"`
}

// CodeIntelConfig holds code intelligence settings (AST/LSP).
type CodeIntelConfig struct {
	// Enabled turns on code intelligence features
	Enabled bool `toml:"enabled" json:"enabled"`
	// AST holds AST parsing settings
	AST ASTConfig `toml:"ast" json:"ast"`
	// LSP holds LSP client settings
	LSP LSPConfig `toml:"lsp" json:"lsp"`
}

// ASTConfig holds AST parsing settings.
type ASTConfig struct {
	// CacheEnabled enables parse result caching
	CacheEnabled bool `toml:"cache_enabled" json:"cache_enabled"`
	// CacheMaxSize is the maximum number of cached parse results
	CacheMaxSize int `toml:"cache_max_size" json:"cache_max_size"`
	// CacheTTLMinutes is how long cached results remain valid
	CacheTTLMinutes int `toml:"cache_ttl_minutes" json:"cache_ttl_minutes"`
}

// LSPConfig holds LSP client settings.
type LSPConfig struct {
	// Enabled turns on LSP client features
	Enabled bool `toml:"enabled" json:"enabled"`
	// Servers maps language IDs to server configurations
	Servers map[string]LSPServerConfig `toml:"servers" json:"servers"`
	// AutoStartServers starts LSP servers on demand
	AutoStartServers bool `toml:"auto_start_servers" json:"auto_start_servers"`
	// ConnectionTimeoutSeconds is the timeout for connecting to LSP servers
	ConnectionTimeoutSeconds int `toml:"connection_timeout_seconds" json:"connection_timeout_seconds"`
}

// LSPServerConfig configures a single LSP server.
type LSPServerConfig struct {
	// Command is the command to start the server
	Command string `toml:"command" json:"command"`
	// Args are command line arguments
	Args []string `toml:"args" json:"args"`
	// Transport is "stdio" or "tcp"
	Transport string `toml:"transport" json:"transport"`
	// Host is the TCP host (for tcp transport)
	Host string `toml:"host" json:"host"`
	// Port is the TCP port (for tcp transport)
	Port int `toml:"port" json:"port"`
	// Languages are the language IDs this server handles
	Languages []string `toml:"languages" json:"languages"`
}

// DaemonConfig holds daemon-specific settings.
//
//gendoc:section daemon
//gendoc:desc Configuration for the daemon process including socket, logging, and data directory.
//gendoc:example [daemon] socket_path = "~/.meept/meept.sock"
type DaemonConfig struct {
	SocketPath string `toml:"socket_path" json:"socket_path"`
	PIDFile    string `toml:"pid_file" json:"pid_file"`
	LogLevel   string `toml:"log_level" json:"log_level"`
	DataDir    string `toml:"data_dir" json:"data_dir"`
}

// LLMConfig holds LLM configuration including budget, broker, and metrics.
//
//gendoc:section llm
//gendoc:desc LLM subsystem configuration including token budget, model broker, adaptive timeout, context firewall, and metrics.
//gendoc:example [llm] budget.hourly_token_limit = 100000
type LLMConfig struct {
	Budget          BudgetConfig          `toml:"budget" json:"budget"`
	Broker          LLMBrokerConfig       `toml:"broker" json:"broker"`
	AdaptiveTimeout LLMAdaptiveTimeoutConfig `toml:"adaptive_timeout" json:"adaptive_timeout"`
	ContextFirewall LLMContextFirewallConfig `toml:"context_firewall" json:"context_firewall"`
	Metrics         LLMMetricsConfig      `toml:"metrics" json:"metrics"`
	Cache           LLMSimpleFeatureConfig `toml:"cache" json:"cache"`
}

// LLMSimpleFeatureConfig configures a simple feature with enabled flag and optional settings.
type LLMSimpleFeatureConfig struct {
	Enabled         bool   `toml:"enabled" json:"enabled"`
	L1MaxEntries    int    `toml:"l1_max_entries" json:"l1_max_entries"`
	L2Enabled       bool   `toml:"l2_enabled" json:"l2_enabled"`
	L2DBPath        string `toml:"l2_db_path" json:"l2_db_path"`
	DefaultTTLMin   int    `toml:"default_ttl_min" json:"default_ttl_min"`
}

// LLMBrokerConfig configures the model broker.
type LLMBrokerConfig struct {
	MaxErrorRate    float64 `toml:"max_error_rate" json:"max_error_rate"`     // default 0.10
	MaxP95LatencyMS float64 `toml:"max_p95_latency_ms" json:"max_p95_latency_ms"` // default 30000
	FallbackEnabled bool    `toml:"fallback_enabled" json:"fallback_enabled"`   // default true
}

// LLMAdaptiveTimeoutConfig configures adaptive timeout calculation.
type LLMAdaptiveTimeoutConfig struct {
	Enabled                bool    `toml:"enabled" json:"enabled"`                    // default true
	StddevMultiplier       float64 `toml:"stddev_multiplier" json:"stddev_multiplier"`          // default 3.0
	StddevTokenRateTimeout bool    `toml:"stddev_token_rate_timeout" json:"stddev_token_rate_timeout"`  // default true
	MinTimeoutSeconds      int     `toml:"min_timeout_seconds" json:"min_timeout_seconds"`        // default 10
	MaxTimeoutSeconds      int     `toml:"max_timeout_seconds" json:"max_timeout_seconds"`        // default 300
	WarmupRequests         int     `toml:"warmup_requests" json:"warmup_requests"`            // default 20
	WindowHours            int     `toml:"window_hours" json:"window_hours"`               // default 24
}

// LLMContextFirewallConfig configures context budget management.
type LLMContextFirewallConfig struct {
	Enabled                    bool    `toml:"enabled" json:"enabled"`                        // default true
	SummarizeHistory           bool    `toml:"summarize_history" json:"summarize_history"`              // default true
	SmallModelContextThreshold int     `toml:"small_model_context_threshold" json:"small_model_context_threshold"`  // default 32768
	IterationBudgetRatio       float64 `toml:"iteration_budget_ratio" json:"iteration_budget_ratio"`         // default 0.30
	ConversationBudgetRatio    float64 `toml:"conversation_budget_ratio" json:"conversation_budget_ratio"`      // default 0.50
	ChunkLargeInputs           bool    `toml:"chunk_large_inputs" json:"chunk_large_inputs"`             // default true
	ChunkThresholdRatio        float64 `toml:"chunk_threshold_ratio" json:"chunk_threshold_ratio"`          // default 0.25
	// WrapUpThreshold is the "soft" limit (0.0-1.0) where wrap-up suggestions are injected
	WrapUpThreshold float64 `toml:"wrap_up_threshold" json:"wrap_up_threshold"` // default 0.50
	// HardLimit is the "hard" limit (0.0-1.0) where context is dropped and reattempted
	HardLimit float64 `toml:"hard_limit" json:"hard_limit"` // default 0.80
	// DropContextOnHardLimit enables context dropping when hard limit is hit
	DropContextOnHardLimit bool `toml:"drop_context_on_hard_limit" json:"drop_context_on_hard_limit"` // default true
	// ProactiveCompression enables the multi-stage ContextCompressor inside the
	// firewall. When true, the compressor runs before the legacy
	// chunk/summarize/drop pipeline.
	ProactiveCompression bool `toml:"proactive_compression" json:"proactive_compression"` // default false
	// ModelContextLimit overrides the model's ContextLimit for the compressor.
	// When zero, model.ContextLimit is used.
	ModelContextLimit int `toml:"model_context_limit" json:"model_context_limit"`
}

// LLMMetricsConfig configures HTTP-level metrics collection.
type LLMMetricsConfig struct {
	Enabled             bool   `toml:"enabled" json:"enabled"`                // default true
	DBPath              string `toml:"db_path" json:"db_path"`                // default "~/.meept/metrics.db"
	RetentionDays       int    `toml:"retention_days" json:"retention_days"`         // default 7
	StatsRefreshMinutes int    `toml:"stats_refresh_minutes" json:"stats_refresh_minutes"`  // default 5
}

// BudgetConfig holds token budget settings.
type BudgetConfig struct {
	HourlyTokenLimit int     `toml:"hourly_token_limit" json:"hourly_token_limit"`
	DailyTokenLimit  int     `toml:"daily_token_limit" json:"daily_token_limit"`
	RateLimitRPM     int     `toml:"rate_limit_rpm" json:"rate_limit_rpm"`
	Aggressiveness   float64 `toml:"aggressiveness" json:"aggressiveness"`
}

// MemoryBackend defines the storage backend for memory.
type MemoryBackend string

const (
	// MemoryBackendMemvid uses the memvid service as the primary backend.
	MemoryBackendMemvid MemoryBackend = "memvid"
	// MemoryBackendSQLite uses local SQLite as the backend.
	MemoryBackendSQLite MemoryBackend = "sqlite"
)

// MemorySecurityConfig holds memory security settings.
type MemorySecurityConfig struct {
	// Enabled turns on security scanning for memory operations
	Enabled bool `toml:"enabled" json:"enabled"`
	// FailClosed blocks memory operations when scanner is unavailable (default: true)
	FailClosed bool `toml:"fail_closed" json:"fail_closed"`
	// LogBlocked logs blocked memory store attempts
	LogBlocked bool `toml:"log_blocked" json:"log_blocked"`
}

// MemoryCachingConfig holds memory prefix caching settings (Hermes pattern).
type MemoryCachingConfig struct {
	// Enabled turns on frozen snapshot prefix caching
	Enabled bool `toml:"enabled" json:"enabled"`
	// RefreshOnSessionEnd refreshes the snapshot at session end
	RefreshOnSessionEnd bool `toml:"refresh_on_session_end" json:"refresh_on_session_end"`
}
// MemoryCategoryLimit holds character limit settings for a memory category.
type MemoryCategoryLimit struct {
	Enabled        bool `toml:"enabled" json:"enabled"`
	CharacterLimit int  `toml:"character_limit" json:"character_limit"`
}

// MemoryLimitsConfig holds character limit settings for different memory categories.
type MemoryLimitsConfig struct {
	Episodic     MemoryCategoryLimit `toml:"episodic" json:"episodic"`
	TaskCode     MemoryCategoryLimit `toml:"task_code" json:"task_code"`
	TaskGeneral  MemoryCategoryLimit `toml:"task_general" json:"task_general"`
	TaskCommands MemoryCategoryLimit `toml:"task_commands" json:"task_commands"`
	Personality  MemoryCategoryLimit `toml:"personality" json:"personality"`
}

// MemoryExpirationConfig holds memory expiration settings.
type MemoryExpirationConfig struct {
	// Enabled turns on access-based expiration
	Enabled bool `toml:"enabled" json:"enabled"`
	// AccessExpirationDays is the number of days without access before expiration (0 = disabled)
	AccessExpirationDays int `toml:"access_expiration_days" json:"access_expiration_days"`
	// SummarizeBeforeDelete creates a summary before deleting expired memories
	SummarizeBeforeDelete bool `toml:"summarize_before_delete" json:"summarize_before_delete"`
	// SummaryCategory is the category for summary memories
	SummaryCategory string `toml:"summary_category" json:"summary_category"`
}

// MemoryVersioningConfig holds versioned memory settings.
type MemoryVersioningConfig struct {
	Enabled     bool `toml:"enabled" json:"enabled"`
	MaxVersions int  `toml:"max_versions" json:"max_versions"`
}


// MemoryConfig holds memory subsystem settings.
//
//gendoc:section memory
//gendoc:desc Memory subsystem configuration including backend selection, consolidation, episodic/task/personality memory types, embeddings, security, caching, limits, expiration, and versioning.
//gendoc:example [memory] backend = "memvid"
type MemoryConfig struct {
	// Backend specifies the storage backend: "memvid" (default) or "sqlite"
	Backend                    MemoryBackend        `toml:"backend" json:"backend"`
	DataDir                    string               `toml:"data_dir" json:"data_dir"`
	ConsolidationIntervalHours int                  `toml:"consolidation_interval_hours" json:"consolidation_interval_hours"`
	Episodic                   EpisodicConfig       `toml:"episodic" json:"episodic"`
	Task                       TaskMemoryConfig     `toml:"task" json:"task"`
	Personality                PersonalityConfig    `toml:"personality" json:"personality"`
	Embeddings                 EmbeddingConfig      `toml:"embeddings" json:"embeddings"`
	// Security holds memory security settings
	Security MemorySecurityConfig `toml:"security" json:"security"`
	// Caching holds memory prefix caching settings
	Caching MemoryCachingConfig `toml:"caching" json:"caching"`
	// Limits holds character limit settings for memory categories
	Limits MemoryLimitsConfig `toml:"limits" json:"limits"`
	// Expiration holds memory expiration settings
	Expiration MemoryExpirationConfig `toml:"expiration" json:"expiration"`
	// Versioning holds versioned memory settings
	Versioning MemoryVersioningConfig `toml:"versioning" json:"versioning"`
	// ProjectOverrides allows per-project character limit overrides
	ProjectOverrides map[string]MemoryLimitsConfig `toml:"project_overrides" json:"project_overrides"`
}

// EpisodicConfig holds episodic memory settings.
type EpisodicConfig struct {
	Enabled         bool `toml:"enabled" json:"enabled"`
	MaxContextItems int  `toml:"max_context_items" json:"max_context_items"`
}

// TaskMemoryConfig holds task memory settings.
type TaskMemoryConfig struct {
	Enabled bool     `toml:"enabled" json:"enabled"`
	Domains []string `toml:"domains" json:"domains"`
}

// PersonalityConfig holds personality memory settings.
type PersonalityConfig struct {
	Enabled                     bool `toml:"enabled" json:"enabled"`
	UpdateIntervalConversations int  `toml:"update_interval_conversations" json:"update_interval_conversations"`
}

// EmbeddingConfig holds vector embedding settings for semantic memory search.
type EmbeddingConfig struct {
	Enabled   bool   `toml:"enabled" json:"enabled"`
	Provider  string `toml:"provider" json:"provider"` // "openai" or "ollama"
	APIKey    string `toml:"api_key" json:"api_key"`
	BaseURL   string `toml:"base_url" json:"base_url"`
	Model     string `toml:"model" json:"model"`
	Dimension int    `toml:"dimension" json:"dimension"`
}

// MemvidConfig holds memvid service settings.
type MemvidConfig struct {
	Enabled  bool   `toml:"enabled" json:"enabled"`
	Endpoint string `toml:"endpoint" json:"endpoint"`
	DataDir  string `toml:"data_dir" json:"data_dir"`
	Timeout  int    `toml:"timeout_seconds" json:"timeout_seconds"`
}

// DistributedMemoryConfig holds settings for 2-tier distributed memory sync.
type DistributedMemoryConfig struct {
	// Enabled turns on distributed memory synchronization
	Enabled bool `toml:"enabled" json:"enabled"`
	// Mode is "local" (default, no sync) or "distributed" (sync with memvid)
	Mode string `toml:"mode" json:"mode"`
	// Sync configures synchronization behavior
	Sync SyncConfig `toml:"sync" json:"sync"`
	// Distillation configures which memories to promote
	Distillation DistillationConfig `toml:"distillation" json:"distillation"`
}

// QAgentConfig holds configuration for the Q Agent (meta-agent for agent creation and optimization).
type QAgentConfig struct {
	// Enabled turns on Q Agent analysis and recommendations
	Enabled bool `toml:"enabled" json:"enabled"`
	// SessionIdleTriggerHours is the idle time before a session is considered complete for analysis
	SessionIdleTriggerHours int `toml:"session_idle_trigger_hours" json:"session_idle_trigger_hours"`
	// AnalysisTimeoutMinutes is the maximum duration for analysis runs
	AnalysisTimeoutMinutes int `toml:"analysis_timeout_minutes" json:"analysis_timeout_minutes"`
	// MinSessionsForPattern is the minimum sessions required to detect a pattern
	MinSessionsForPattern int `toml:"min_sessions_for_pattern" json:"min_sessions_for_pattern"`
	// MinConfidenceScore is the minimum confidence score for recommendations
	MinConfidenceScore float64 `toml:"min_confidence_score" json:"min_confidence_score"`
	// HighErrorRateThreshold is the error rate threshold for flagging issues
	HighErrorRateThreshold float64 `toml:"high_error_rate_threshold" json:"high_error_rate_threshold"`
	// HighRejectionRateThreshold is the rejection rate threshold for flagging issues
	HighRejectionRateThreshold float64 `toml:"high_rejection_rate_threshold" json:"high_rejection_rate_threshold"`
	// DurationVarianceThreshold is the duration variance threshold for detecting misconfigurations
	DurationVarianceThreshold float64 `toml:"duration_variance_threshold" json:"duration_variance_threshold"`
	// NotifyChat enables notifications via chat
	NotifyChat bool `toml:"notify_chat" json:"notify_chat"`
	// NotifyCLI enables notifications via CLI
	NotifyCLI bool `toml:"notify_cli" json:"notify_cli"`
	// NotifyMenuBar enables notifications via menu bar
	NotifyMenuBar bool `toml:"notify_menu_bar" json:"notify_menu_bar"`
	// AnalysisDir is the directory for cached analysis results
	AnalysisDir string `toml:"analysis_dir" json:"analysis_dir"`
	// OutcomesLog is the path for the outcomes log file
	OutcomesLog string `toml:"outcomes_log" json:"outcomes_log"`
}

// SyncConfig holds sync timing and behavior settings.
type SyncConfig struct {
	// HydrateOnClaim fetches relevant memories when a job is claimed
	HydrateOnClaim bool `toml:"hydrate_on_claim" json:"hydrate_on_claim"`
	// HydrationLimit is max memories to fetch during hydration
	HydrationLimit int `toml:"hydration_limit" json:"hydration_limit"`
	// DistillOnComplete promotes memories when a job completes
	DistillOnComplete bool `toml:"distill_on_complete" json:"distill_on_complete"`
	// PeriodicDistillIntervalMinutes runs distillation on a timer (0 = disabled)
	PeriodicDistillIntervalMinutes int `toml:"periodic_distill_interval_minutes" json:"periodic_distill_interval_minutes"`
	// RetryOnFailure queues failed sync operations for retry
	RetryOnFailure bool `toml:"retry_on_failure" json:"retry_on_failure"`
	// MaxRetries is the max retry attempts for failed operations
	MaxRetries int `toml:"max_retries" json:"max_retries"`
}

// DistillationConfig controls which memories get promoted to shared storage.
type DistillationConfig struct {
	// PageRankThreshold promotes memories with PageRank above this value
	PageRankThreshold float64 `toml:"pagerank_threshold" json:"pagerank_threshold"`
	// HubConnectivityThreshold promotes memories with degree >= this
	HubConnectivityThreshold int `toml:"hub_connectivity_threshold" json:"hub_connectivity_threshold"`
	// PromoteTaskCompletions always promotes task completion summaries
	PromoteTaskCompletions bool `toml:"promote_task_completions" json:"promote_task_completions"`
	// CrossAgentReferencesMin promotes memories referenced by >= N other agents
	CrossAgentReferencesMin int `toml:"cross_agent_references_min" json:"cross_agent_references_min"`
	// MinMemoryAgeMinutes requires memories to be at least this old
	MinMemoryAgeMinutes int `toml:"min_memory_age_minutes" json:"min_memory_age_minutes"`
}

// CacheConfig holds settings for tool result caching.
type CacheConfig struct {
	// Enabled turns on tool result caching
	Enabled bool `toml:"enabled" json:"enabled"`
	// MaxEntries is the maximum number of cached results
	MaxEntries int `toml:"max_entries" json:"max_entries"`
	// DefaultTTLSeconds is the default time-to-live for cache entries
	DefaultTTLSeconds int `toml:"default_ttl_seconds" json:"default_ttl_seconds"`
	// CleanupFreqSeconds is how often to cleanup expired entries
	CleanupFreqSeconds int `toml:"cleanup_freq_seconds" json:"cleanup_freq_seconds"`
	// EnabledTools is a list of tool names to cache (empty = all tools)
	EnabledTools []string `toml:"enabled_tools" json:"enabled_tools"`
}

// AgentConfig holds agent loop settings.
//
//gendoc:section agent
//gendoc:desc Agent configuration including progress streaming, caching, error handling, review workflow, validation, and watchdog monitoring.
//gendoc:example [agent] progress_enabled = true
type AgentConfig struct {
	// ProgressEnabled turns on streaming progress updates
	ProgressEnabled bool `toml:"progress_enabled" json:"progress_enabled"`
	// ProgressIntervalSeconds is the minimum interval between progress events
	ProgressIntervalSeconds int `toml:"progress_interval_seconds" json:"progress_interval_seconds"`
	// Cache holds tool result caching settings
	Cache CacheConfig `toml:"cache" json:"cache"`
	// Errors holds error handling settings
	Errors ErrorsConfig `toml:"errors" json:"errors"`
	// Review holds code review settings
	Review ReviewConfig `toml:"review" json:"review"`
	// Validation holds task completion validation settings
	Validation ValidationConfig `toml:"validation" json:"validation"`
	// Watchdog holds agent monitoring settings
	Watchdog WatchdogConfig `toml:"watchdog" json:"watchdog"`
}

// WatchdogConfig holds agent monitoring and timeout settings.
type WatchdogConfig struct {
	// Enabled turns on watchdog monitoring
	Enabled bool `toml:"enabled" json:"enabled"`
	// TimeoutMinutes is the timeout in minutes before aborting (default: 10)
	TimeoutMinutes int `toml:"timeout_minutes" json:"timeout_minutes"`
	// HeartbeatIntervalSec is the heartbeat interval in seconds (default: 30)
	HeartbeatIntervalSec int `toml:"heartbeat_interval_sec" json:"heartbeat_interval_sec"`
	// MaxIterations is the maximum iterations before aborting (default: 50)
	MaxIterations int `toml:"max_iterations" json:"max_iterations"`
	// StuckIterationCount is iterations without progress before flagged as stuck (default: 5)
	StuckIterationCount int `toml:"stuck_iteration_count" json:"stuck_iteration_count"`
}

// ErrorsConfig holds error handling settings.
type ErrorsConfig struct {
	// DetailedErrors enables detailed error messages
	DetailedErrors bool `toml:"detailed_errors" json:"detailed_errors"`
	// IncludeExamples adds usage examples to error messages
	IncludeExamples bool `toml:"include_examples" json:"include_examples"`
	// MaxSuggestionLength limits the length of error suggestions
	MaxSuggestionLength int `toml:"max_suggestion_length" json:"max_suggestion_length"`
}

// ReviewConfig holds code review settings for the multi-agent system.
type ReviewConfig struct {
	// Enabled turns on automatic code review
	Enabled bool `toml:"enabled" json:"enabled"`
	// RequireReview lists intent types that require review
	RequireReview []string `toml:"require_review" json:"require_review"`
	// SkipReview lists intent types that skip review
	SkipReview []string `toml:"skip_review" json:"skip_review"`
	// ReviewerMapping maps agent IDs to reviewer agent IDs
	ReviewerMapping map[string]string `toml:"reviewer_mapping" json:"reviewer_mapping"`
	// MaxRevisionCycles is the maximum revision cycles before auto-approval
	MaxRevisionCycles int `toml:"max_revision_cycles" json:"max_revision_cycles"`
	// AutoApprovePatterns lists glob patterns that are auto-approved
	AutoApprovePatterns []string `toml:"auto_approve_patterns" json:"auto_approve_patterns"`
}

// ValidationConfig holds task completion validation settings.
type ValidationConfig struct {
	// Enabled turns on task completion validation
	Enabled bool `toml:"enabled" json:"enabled"`
	// RequireValidation lists tool hints that require validation
	RequireValidation []string `toml:"require_validation" json:"require_validation"`
	// SkipValidation lists tool hints that skip validation
	SkipValidation []string `toml:"skip_validation" json:"skip_validation"`
	// SkipValidationAgents lists agents that skip validation
	SkipValidationAgents []string `toml:"skip_validation_agents" json:"skip_validation_agents"`
	// MaxValidationLoops is the maximum validation loops before escalation
	MaxValidationLoops int `toml:"max_validation_loops" json:"max_validation_loops"`
}

// MultiAgentConfig holds multi-agent orchestration settings.
type MultiAgentConfig struct {
	Enabled            bool   `toml:"enabled" json:"enabled"`
	DispatcherModel    string `toml:"dispatcher_model" json:"dispatcher_model"`
	DefaultModel       string `toml:"default_model" json:"default_model"`
	ClassifierModel    string `toml:"classifier_model" json:"classifier_model"` // Model for intent classification (defaults to small_model)
	MaxMemoryRefs      int    `toml:"max_memory_refs" json:"max_memory_refs"`
	ContextSearchLimit int    `toml:"context_search_limit" json:"context_search_limit"`
}

// AgentsConfig holds agent configuration settings.
type AgentsConfig struct {
	// Enabled enables the multi-agent system with TOML-based agent definitions.
	Enabled bool `toml:"enabled" json:"enabled"`

	// ConfigDirs are directories to search for agent definition TOML files.
	// Searched in order; later directories override earlier ones.
	ConfigDirs []string `toml:"config_dirs" json:"config_dirs"`

	// PromptsDir is the base directory for prompt components.
	PromptsDir string `toml:"prompts_dir" json:"prompts_dir"`

	// DefaultModel is the fallback model for agents that don't specify one.
	DefaultModel string `toml:"default_model" json:"default_model"`

	// DispatcherID is the agent ID that handles intake/routing.
	DispatcherID string `toml:"dispatcher_id" json:"dispatcher_id"`
}

// SecurityConfig holds security settings.
//
//gendoc:section security
//gendoc:desc Security configuration including input sanitization, path restrictions, output monitoring, shell command scanning, and audit logging.
//gendoc:example [security] sanitize_inputs = true
type SecurityConfig struct {
	SanitizeInputs              bool     `toml:"sanitize_inputs" json:"sanitize_inputs"`
	SanitizeStrictness          string   `toml:"sanitize_strictness" json:"sanitize_strictness"` // "permissive", "standard", "strict"
	LLMFilterExternal           bool     `toml:"llm_filter_external" json:"llm_filter_external"`
	RequireConfirmationHigh     bool     `toml:"require_confirmation_high" json:"require_confirmation_high"`
	RequireConfirmationCritical bool     `toml:"require_confirmation_critical" json:"require_confirmation_critical"`
	BlockFinancial              bool     `toml:"block_financial" json:"block_financial"`
	AllowedPaths                []string `toml:"allowed_paths" json:"allowed_paths"`
	BlockedPaths                []string `toml:"blocked_paths" json:"blocked_paths"`

	// Output monitoring
	MonitorOutput bool `toml:"monitor_output" json:"monitor_output"` // Enable credential detection in LLM output
	RedactOutput  bool `toml:"redact_output" json:"redact_output"`  // Automatically redact detected credentials

	// Shell command security
	ScanShellCommands bool   `toml:"scan_shell_commands" json:"scan_shell_commands"` // Enable Tirith command scanning
	TirithBinary      string `toml:"tirith_binary" json:"tirith_binary"`       // Path to tirith binary

	// Audit logging
	EnableAuditLog bool   `toml:"enable_audit_log" json:"enable_audit_log"` // Enable security audit logging
	AuditDBPath    string `toml:"audit_db_path" json:"audit_db_path"`    // Path to audit log database

	// Override matching behavior
	// When true, uses strict glob/exact matching for permission overrides.
	// When false (default), uses lenient three-strategy cascade (substring, glob, trimmed substring).
	// Changing this will affect existing overrides - migrate with caution.
	StrictOverrideMatching bool `toml:"strict_override_matching" json:"strict_override_matching"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Enabled  bool   `toml:"enabled" json:"enabled"`
	Timezone string `toml:"timezone" json:"timezone"`
}

// QueueConfig holds job queue settings.
type QueueConfig struct {
	DBPath     string `toml:"db_path" json:"db_path"`
	MaxRetries int    `toml:"max_retries" json:"max_retries"`
}

// WorkersConfig holds worker pool settings.
type WorkersConfig struct {
	PoolSize           int      `toml:"pool_size" json:"pool_size"`
	IdleTimeoutSeconds int      `toml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	DefaultCaps        []string `toml:"default_caps" json:"default_caps"`
}

// IsolationConfig holds task isolation settings.
type IsolationConfig struct {
	BaseDir     string `toml:"base_dir" json:"base_dir"`
	AutoGitInit bool   `toml:"auto_git_init" json:"auto_git_init"`
	AutoTest    bool   `toml:"auto_test" json:"auto_test"`
}

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	Enabled      bool    `toml:"enabled" json:"enabled"`
	Token        string  `toml:"token" json:"token"`
	CreatorID    int64   `toml:"creator_id" json:"creator_id"`
	AllowedUsers []int64 `toml:"allowed_users" json:"allowed_users"` // Telegram user IDs allowed to interact (empty = all)
	AllowedChats []int64 `toml:"allowed_chats" json:"allowed_chats"` // Telegram chat IDs allowed (empty = all)
	PollTimeout  int     `toml:"poll_timeout" json:"poll_timeout"`  // Long polling timeout in seconds
}

// WebConfig holds web API settings.
type WebConfig struct {
	Enabled   bool   `toml:"enabled" json:"enabled"`
	Host      string `toml:"host" json:"host"`
	Port      int    `toml:"port" json:"port"`
	SecretKey string `toml:"secret_key" json:"secret_key"`
}

// MCPConfig holds MCP settings.
type MCPConfig struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	ConfigFile string `toml:"config_file" json:"config_file"`
}

// PluginsConfig holds plugin settings.
type PluginsConfig struct {
	Enabled   bool   `toml:"enabled" json:"enabled"`
	Directory string `toml:"directory" json:"directory"`
}

// WorkspaceConfig holds workspace settings.
type WorkspaceConfig struct {
	Enabled          bool   `toml:"enabled" json:"enabled"`
	BaseDir          string `toml:"base_dir" json:"base_dir"`
	AutoCommit       bool   `toml:"auto_commit" json:"auto_commit"`
	CommitOnPlan     bool   `toml:"commit_on_plan" json:"commit_on_plan"`
	CommitOnStep     bool   `toml:"commit_on_step" json:"commit_on_step"`
	CleanupCompleted bool   `toml:"cleanup_completed" json:"cleanup_completed"`
}

// SkillsConfig holds skills settings.
type SkillsConfig struct {
	Enabled     bool     `toml:"enabled" json:"enabled"`
	SearchPaths []string `toml:"search_paths" json:"search_paths"` // Additional skill directories beyond defaults
	AutoReload  bool     `toml:"auto_reload" json:"auto_reload"`  // Watch for skill file changes
	CacheSize   int      `toml:"cache_size" json:"cache_size"`   // Max skills to cache in lazy loader (default: 50)
}


// SelfImproveConfig holds self-improvement settings.
type SelfImproveConfig struct {
	Enabled               bool            `toml:"enabled" json:"enabled"`
	DataDir               string          `toml:"data_dir" json:"data_dir"`
	MaxIterationsPerCycle int             `toml:"max_iterations_per_cycle" json:"max_iterations_per_cycle"`
	MaxFixesPerCycle      int             `toml:"max_fixes_per_cycle" json:"max_fixes_per_cycle"`
	AutoRunIntervalHours  int             `toml:"auto_run_interval_hours" json:"auto_run_interval_hours"`
	AIInfra               AIInfraConfig   `toml:"ai_infra" json:"ai_infra"`
	Sandbox               SandboxConfig   `toml:"sandbox" json:"sandbox"`
	Safety                SafetyConfig    `toml:"safety" json:"safety"`
	Detection             DetectionConfig `toml:"detection" json:"detection"`
}

// AIInfraConfig holds AI infrastructure settings for self-improvement.
type AIInfraConfig struct {
	Enabled         bool    `toml:"enabled" json:"enabled"`
	BaseURL         string  `toml:"base_url" json:"base_url"`
	APIKeyEnv       string  `toml:"api_key_env" json:"api_key_env"`
	AnalysisModel   string  `toml:"analysis_model" json:"analysis_model"`
	GenerationModel string  `toml:"generation_model" json:"generation_model"`
	ReviewModel     string  `toml:"review_model" json:"review_model"`
	TimeoutSeconds  float64 `toml:"timeout_seconds" json:"timeout_seconds"`
	MaxRetries      int     `toml:"max_retries" json:"max_retries"`
}

// SandboxConfig holds sandbox settings for self-improvement.
type SandboxConfig struct {
	WorktreeDir        string  `toml:"worktree_dir" json:"worktree_dir"`
	CleanupOnSuccess   bool    `toml:"cleanup_on_success" json:"cleanup_on_success"`
	CleanupOnFailure   bool    `toml:"cleanup_on_failure" json:"cleanup_on_failure"`
	MaxWorktrees       int     `toml:"max_worktrees" json:"max_worktrees"`
	TestTimeoutSeconds float64 `toml:"test_timeout_seconds" json:"test_timeout_seconds"`
}

// SafetyConfig holds safety settings for self-improvement.
type SafetyConfig struct {
	RequireHumanApproval   bool     `toml:"require_human_approval" json:"require_human_approval"`
	MaxFilesPerFix         int      `toml:"max_files_per_fix" json:"max_files_per_fix"`
	MaxLinesChangedPerFix  int      `toml:"max_lines_changed_per_fix" json:"max_lines_changed_per_fix"`
	BlockedPaths           []string `toml:"blocked_paths" json:"blocked_paths"`
	AllowedRiskLevels      []string `toml:"allowed_risk_levels" json:"allowed_risk_levels"`
	BlockCriticalRisk      bool     `toml:"block_critical_risk" json:"block_critical_risk"`
	RequireTestsPass       bool     `toml:"require_tests_pass" json:"require_tests_pass"`
	MinConfidenceThreshold float64  `toml:"min_confidence_threshold" json:"min_confidence_threshold"`
}

// DetectionConfig holds detection settings for self-improvement.
type DetectionConfig struct {
	ScanPytest       bool     `toml:"scan_pytest" json:"scan_pytest"`
	ScanRuntimeLogs  bool     `toml:"scan_runtime_logs" json:"scan_runtime_logs"`
	ScanTypeCheck    bool     `toml:"scan_type_check" json:"scan_type_check"`
	ScanLint         bool     `toml:"scan_lint" json:"scan_lint"`
	LogFile          string   `toml:"log_file" json:"log_file"`
	LogLookbackHours int      `toml:"log_lookback_hours" json:"log_lookback_hours"`
	PytestArgs       []string `toml:"pytest_args" json:"pytest_args"`
	MypyArgs         []string `toml:"mypy_args" json:"mypy_args"`
	RuffArgs         []string `toml:"ruff_args" json:"ruff_args"`
}

// OrchestratorConfig holds hierarchical orchestrator settings.
type OrchestratorConfig struct {
	MaxPlanSteps     int `toml:"max_plan_steps" json:"max_plan_steps"`
	MaxResearchSteps int `toml:"max_research_steps" json:"max_research_steps"`
	PlannerTimeout   int `toml:"planner_timeout" json:"planner_timeout"`
	TokenBudgetAlert int `toml:"token_budget_alert" json:"token_budget_alert"`
}

// ShadowConfig holds shadow training settings.
type ShadowConfig struct {
	Enabled   bool                  `toml:"enabled" json:"enabled"`
	DataDir   string                `toml:"data_dir" json:"data_dir"`
	Shadowing ShadowShadowingConfig `toml:"shadowing" json:"shadowing"`
	Teacher   ShadowTeacherConfig   `toml:"teacher" json:"teacher"`
	Quality   ShadowQualityConfig   `toml:"quality" json:"quality"`
	Examples  ShadowExamplesConfig  `toml:"examples" json:"examples"`
	Export    ShadowExportConfig    `toml:"export" json:"export"`
	Adapters  ShadowAdaptersConfig  `toml:"adapters" json:"adapters"`
}

// ShadowShadowingConfig controls when and how responses are shadowed.
type ShadowShadowingConfig struct {
	Mode          string   `toml:"mode" json:"mode"`
	MinComplexity string   `toml:"min_complexity" json:"min_complexity"`
	Domains       []string `toml:"domains" json:"domains"`
	TaskTypes     []string `toml:"task_types" json:"task_types"`
	SampleRate    float64  `toml:"sample_rate" json:"sample_rate"`
	QueueSize     int      `toml:"queue_size" json:"queue_size"`
	WorkerCount   int      `toml:"worker_count" json:"worker_count"`
}

// ShadowTeacherConfig configures the teacher model.
type ShadowTeacherConfig struct {
	Model             string  `toml:"model" json:"model"`
	FallbackModel     string  `toml:"fallback_model" json:"fallback_model"`
	Temperature       float64 `toml:"temperature" json:"temperature"`
	MaxTokens         int     `toml:"max_tokens" json:"max_tokens"`
	TimeoutSeconds    int     `toml:"timeout_seconds" json:"timeout_seconds"`
	MaxDailyQueries   int     `toml:"max_daily_queries" json:"max_daily_queries"`
	MaxDailyCost      float64 `toml:"max_daily_cost" json:"max_daily_cost"`
	RequestsPerMinute int     `toml:"requests_per_minute" json:"requests_per_minute"`
}

// ShadowQualityConfig configures quality scoring.
type ShadowQualityConfig struct {
	Method               string                       `toml:"method" json:"method"`
	HighQualityThreshold float64                      `toml:"high_quality_threshold" json:"high_quality_threshold"`
	TrainableThreshold   float64                      `toml:"trainable_threshold" json:"trainable_threshold"`
	PreferenceMargin     float64                      `toml:"preference_margin" json:"preference_margin"`
	HeuristicWeights     ShadowHeuristicWeightsConfig `toml:"heuristic_weights" json:"heuristic_weights"`
	EvalPromptTemplate   string                       `toml:"eval_prompt_template" json:"eval_prompt_template"`
}

// ShadowHeuristicWeightsConfig defines scoring dimension weights.
type ShadowHeuristicWeightsConfig struct {
	Relevance    float64 `toml:"relevance" json:"relevance"`
	Completeness float64 `toml:"completeness" json:"completeness"`
	Correctness  float64 `toml:"correctness" json:"correctness"`
	Style        float64 `toml:"style" json:"style"`
}

// ShadowExamplesConfig configures few-shot example management.
type ShadowExamplesConfig struct {
	Enabled          bool    `toml:"enabled" json:"enabled"`
	MaxPerCategory   int     `toml:"max_per_category" json:"max_per_category"`
	MinQuality       float64 `toml:"min_quality" json:"min_quality"`
	DefaultCount     int     `toml:"default_count" json:"default_count"`
	MaxCount         int     `toml:"max_count" json:"max_count"`
	SimilarityWeight float64 `toml:"similarity_weight" json:"similarity_weight"`
	RecencyWeight    float64 `toml:"recency_weight" json:"recency_weight"`
	QualityWeight    float64 `toml:"quality_weight" json:"quality_weight"`
	MaxContextTokens int     `toml:"max_context_tokens" json:"max_context_tokens"`
}

// ShadowExportConfig configures training data export.
type ShadowExportConfig struct {
	OutputDir                string   `toml:"output_dir" json:"output_dir"`
	Formats                  []string `toml:"formats" json:"formats"`
	MinRecords               int      `toml:"min_records" json:"min_records"`
	IncludeLowQuality        bool     `toml:"include_low_quality" json:"include_low_quality"`
	Deduplicate              bool     `toml:"deduplicate" json:"deduplicate"`
	DedupSimilarityThreshold float64  `toml:"dedup_similarity_threshold" json:"dedup_similarity_threshold"`
}

// ShadowAdaptersConfig configures adapter management.
type ShadowAdaptersConfig struct {
	Enabled        bool             `toml:"enabled" json:"enabled"`
	OllamaEndpoint string           `toml:"ollama_endpoint" json:"ollama_endpoint"`
	AutoTrain      bool             `toml:"auto_train" json:"auto_train"`
	TrainThreshold int              `toml:"train_threshold" json:"train_threshold"`
	TrainSchedule  string           `toml:"train_schedule" json:"train_schedule"`
	AdapterDir     string           `toml:"adapter_dir" json:"adapter_dir"`
	LoRA           ShadowLoRAConfig `toml:"lora" json:"lora"`
	DPO            ShadowDPOConfig  `toml:"dpo" json:"dpo"`
}

// ShadowLoRAConfig configures LoRA training parameters.
type ShadowLoRAConfig struct {
	Rank                 int      `toml:"rank" json:"rank"`
	Alpha                int      `toml:"alpha" json:"alpha"`
	Dropout              float64  `toml:"dropout" json:"dropout"`
	TargetModules        []string `toml:"target_modules" json:"target_modules"`
	LearningRate         float64  `toml:"learning_rate" json:"learning_rate"`
	Epochs               int      `toml:"epochs" json:"epochs"`
	BatchSize            int      `toml:"batch_size" json:"batch_size"`
	GradientAccumulation int      `toml:"gradient_accumulation" json:"gradient_accumulation"`
	WarmupRatio          float64  `toml:"warmup_ratio" json:"warmup_ratio"`
	MaxGradNorm          float64  `toml:"max_grad_norm" json:"max_grad_norm"`
}

// ShadowDPOConfig configures DPO training parameters.
type ShadowDPOConfig struct {
	Beta     float64 `toml:"beta" json:"beta"`
	LossType string  `toml:"loss_type" json:"loss_type"`
}


// GetLimitsForProject returns the character limits for a specific project path.
// If project-specific overrides exist, they are returned; otherwise defaults are used.
func (c *MemoryConfig) GetLimitsForProject(projectPath string) MemoryLimitsConfig {
	if c.ProjectOverrides != nil {
		if overrides, exists := c.ProjectOverrides[projectPath]; exists {
			return overrides
		}
	}
	return c.Limits
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Daemon: DaemonConfig{
			SocketPath: "~/.meept/meept.sock",
			PIDFile:    "~/.meept/meept.pid",
			LogLevel:   "INFO",
			DataDir:    "~/.meept",
		},
		LLM: LLMConfig{
			Budget: BudgetConfig{
				HourlyTokenLimit: 100000,
				DailyTokenLimit:  1000000,
				RateLimitRPM:     30,
				Aggressiveness:   0.5,
			},
			Broker: LLMBrokerConfig{
				MaxErrorRate:    0.10,
				MaxP95LatencyMS: 30000,
				FallbackEnabled: true,
			},
			AdaptiveTimeout: LLMAdaptiveTimeoutConfig{
				Enabled:                true,
				StddevMultiplier:       3.0,
				StddevTokenRateTimeout: true,
				MinTimeoutSeconds:      10,
				MaxTimeoutSeconds:      300,
				WarmupRequests:         20,
				WindowHours:            24,
			},
			ContextFirewall: LLMContextFirewallConfig{
				Enabled:                    true,
				SummarizeHistory:           true,
				SmallModelContextThreshold: 32768,
				IterationBudgetRatio:       0.30,
				ConversationBudgetRatio:    0.50,
				ChunkLargeInputs:           true,
				ChunkThresholdRatio:        0.25,
				WrapUpThreshold:        0.50,
				HardLimit:              0.80,
				DropContextOnHardLimit: true,
				ProactiveCompression:   false,
			},
			Metrics: LLMMetricsConfig{
				Enabled:             true,
				DBPath:              "~/.meept/metrics.db",
				RetentionDays:       7,
				StatsRefreshMinutes: 5,
			},
		},
		Memory: MemoryConfig{
			Backend:                    MemoryBackendMemvid, // Default to memvid, fallback to sqlite
			DataDir:                    "~/.meept/memory",
			ConsolidationIntervalHours: 6,
			Episodic: EpisodicConfig{
				Enabled:         true,
				MaxContextItems: 20,
			},
			Task: TaskMemoryConfig{
				Enabled: true,
				Domains: []string{"general", "code", "commands"},
			},
			Personality: PersonalityConfig{
				Enabled:                     true,
				UpdateIntervalConversations: 10,
			},
			Security: MemorySecurityConfig{
				Enabled:    true,
				FailClosed: true,
				LogBlocked: true,
			},
			Caching: MemoryCachingConfig{
				Enabled:             true,
				RefreshOnSessionEnd: true,
			},
			Limits: MemoryLimitsConfig{
				Episodic: MemoryCategoryLimit{
					Enabled:        true,
					CharacterLimit: 2200,
				},
				TaskCode: MemoryCategoryLimit{
					Enabled:        true,
					CharacterLimit: 3000,
				},
				TaskGeneral: MemoryCategoryLimit{
					Enabled:        true,
					CharacterLimit: 2200,
				},
				TaskCommands: MemoryCategoryLimit{
					Enabled:        true,
					CharacterLimit: 1500,
				},
				Personality: MemoryCategoryLimit{
					Enabled:        true,
					CharacterLimit: 1375,
				},
			},
			Expiration: MemoryExpirationConfig{
				Enabled:               true,
				AccessExpirationDays:  90,
				SummarizeBeforeDelete: true,
				SummaryCategory:       "archived",
			},
			Versioning: MemoryVersioningConfig{
				Enabled:     true,
				MaxVersions: 10,
			},
			ProjectOverrides: make(map[string]MemoryLimitsConfig),
		},
		Memvid: MemvidConfig{
			Enabled:  false,
			Endpoint: "http://localhost:8765",
			DataDir:  "~/.meept/memvid",
			Timeout:  30,
		},
		MultiAgent: MultiAgentConfig{
			Enabled:            true,
			DispatcherModel:    "", // Use default
			DefaultModel:       "",
			ClassifierModel:    "", // Empty = use small_model
			MaxMemoryRefs:      20,
			ContextSearchLimit: 10,
		},
		Agents: AgentsConfig{
			Enabled:      false,
			ConfigDirs:   []string{"~/.meept/agents", "config/agents"},
			PromptsDir:   "config/prompts",
			DefaultModel: "", // Empty = use llm.default_model
			DispatcherID: "dispatcher",
		},
		Agent: AgentConfig{
			ProgressEnabled:         true, // Enabled by default for TUI progress bars
			ProgressIntervalSeconds: 30,
			Cache: CacheConfig{
				Enabled:            false, // Disabled by default
				MaxEntries:         1000,
				DefaultTTLSeconds:  300, // 5 minutes
				CleanupFreqSeconds: 60,  // 1 minute
				EnabledTools: []string{
					"file_read",
					"list_directory",
					"memory_search",
					"memory_get_context",
					"platform_status",
					"platform_agents",
					"platform_tools",
				},
			},
			Errors: ErrorsConfig{
				DetailedErrors:      true,
				IncludeExamples:     true,
				MaxSuggestionLength: 500,
			},
			Review: ReviewConfig{
				Enabled:       true,
				RequireReview: []string{"code", "refactor", "debug", "git"},
				SkipReview:    []string{"chat", "report", "recall", "search"},
				ReviewerMapping: map[string]string{
					"coder":     "code-reviewer",
					"debugger":  "debug-reviewer",
					"planner":   "planner-reviewer",
					"analyst":   "analyst-reviewer",
					"committer": "code-reviewer",
				},
				MaxRevisionCycles:   3,
				AutoApprovePatterns: []string{"*.md", "LICENSE"},
			},
			Validation: ValidationConfig{
				Enabled:              true,
				RequireValidation:    []string{"code", "refactor", "debug", "git", "fix", "commit"},
				SkipValidation:       []string{"chat", "report", "recall", "search", "analyze", "platform"},
				SkipValidationAgents: []string{"chat", "analyst"},
				MaxValidationLoops:   3,
			},
			Watchdog: WatchdogConfig{
				Enabled:              true,
				TimeoutMinutes:       10,
				HeartbeatIntervalSec: 30,
				MaxIterations:        50,
				StuckIterationCount:  5,
			},
		},
		Security: SecurityConfig{
			SanitizeInputs:              true,
			SanitizeStrictness:          "standard",
			LLMFilterExternal:           false,
			RequireConfirmationHigh:     true,
			RequireConfirmationCritical: true,
			BlockFinancial:              true,
			AllowedPaths:                []string{"~/*"},
			BlockedPaths:                []string{"~/.ssh/*", "~/.gnupg/*", "~/.meept/meept.toml"},
			MonitorOutput:               true,
			RedactOutput:                true,
			ScanShellCommands:           true,
			TirithBinary:                "tirith",
			EnableAuditLog:              false,
			AuditDBPath:                 "~/.meept/audit.db",
		},
		Scheduler: SchedulerConfig{
			Enabled:  true,
			Timezone: "UTC",
		},
		Queue: QueueConfig{
			DBPath:     "~/.meept/queue.db",
			MaxRetries: 3,
		},
		Workers: WorkersConfig{
			PoolSize:           4,
			IdleTimeoutSeconds: 300,
			DefaultCaps:        []string{"code", "reasoning"},
		},
		Isolation: IsolationConfig{
			BaseDir:     "~/.meept/sandboxes",
			AutoGitInit: true,
			AutoTest:    true,
		},
		Telegram: TelegramConfig{
			Enabled:     false,
			Token:       "",
			CreatorID:   0,
			PollTimeout: 30,
		},
		Web: WebConfig{
			Enabled:   false,
			Host:      "127.0.0.1",
			Port:      8420,
			SecretKey: "",
		},
		MCP: MCPConfig{
			Enabled:    false,
			ConfigFile: "~/.meept/mcp_servers.json",
		},
		Plugins: PluginsConfig{
			Enabled:   true,
			Directory: "~/.meept/plugins",
		},
		Workspace: WorkspaceConfig{
			Enabled:          true,
			BaseDir:          "~/.meept/workspaces",
			AutoCommit:       true,
			CommitOnPlan:     true,
			CommitOnStep:     true,
			CleanupCompleted: false,
		},
		Skills: SkillsConfig{
			Enabled:     false,
			SearchPaths: []string{},
			AutoReload:  false,
		},
		SelfImprove: SelfImproveConfig{
			Enabled:               false,
			DataDir:               "~/.meept/selfimprove",
			MaxIterationsPerCycle: 5,
			MaxFixesPerCycle:      10,
			AutoRunIntervalHours:  0,
			AIInfra: AIInfraConfig{
				Enabled:         false,
				BaseURL:         "http://localhost:8100",
				APIKeyEnv:       "MEEPT_AI_INFRA_KEY",
				AnalysisModel:   "claude-opus-4-5-20251101",
				GenerationModel: "claude-sonnet-4-5-20241022",
				ReviewModel:     "claude-opus-4-5-20251101",
				TimeoutSeconds:  120.0,
				MaxRetries:      3,
			},
			Sandbox: SandboxConfig{
				WorktreeDir:        "~/.meept/selfimprove/worktrees",
				CleanupOnSuccess:   true,
				CleanupOnFailure:   false,
				MaxWorktrees:       5,
				TestTimeoutSeconds: 300.0,
			},
			Safety: SafetyConfig{
				RequireHumanApproval:   true,
				MaxFilesPerFix:         10,
				MaxLinesChangedPerFix:  500,
				BlockedPaths:           []string{},
				AllowedRiskLevels:      []string{"low", "medium", "high"},
				BlockCriticalRisk:      true,
				RequireTestsPass:       true,
				MinConfidenceThreshold: 0.7,
			},
			Detection: DetectionConfig{
				ScanPytest:       true,
				ScanRuntimeLogs:  true,
				ScanTypeCheck:    true,
				ScanLint:         true,
				LogFile:          "~/.meept/meept.log",
				LogLookbackHours: 24,
				PytestArgs:       []string{"-v", "--tb=short"},
				MypyArgs:         []string{"--ignore-missing-imports"},
				RuffArgs:         []string{},
			},
		},
		Orchestrator: OrchestratorConfig{
			MaxPlanSteps:     10,
			MaxResearchSteps: 3,
			PlannerTimeout:   120,
			TokenBudgetAlert: 5000,
		},
		Shadow: ShadowConfig{
			Enabled: false,
			DataDir: "~/.meept/shadow",
			Shadowing: ShadowShadowingConfig{
				Mode:          "async",
				MinComplexity: "moderate",
				Domains:       []string{},
				TaskTypes:     []string{},
				SampleRate:    0.5,
				QueueSize:     1000,
				WorkerCount:   2,
			},
			Teacher: ShadowTeacherConfig{
				Model:             "",
				FallbackModel:     "",
				Temperature:       0.0,
				MaxTokens:         4096,
				TimeoutSeconds:    120,
				MaxDailyQueries:   500,
				MaxDailyCost:      10.0,
				RequestsPerMinute: 30,
			},
			Quality: ShadowQualityConfig{
				Method:               "hybrid",
				HighQualityThreshold: 0.85,
				TrainableThreshold:   0.6,
				PreferenceMargin:     0.1,
				HeuristicWeights: ShadowHeuristicWeightsConfig{
					Relevance:    0.30,
					Completeness: 0.25,
					Correctness:  0.35,
					Style:        0.10,
				},
				EvalPromptTemplate: "",
			},
			Examples: ShadowExamplesConfig{
				Enabled:          true,
				MaxPerCategory:   100,
				MinQuality:       0.8,
				DefaultCount:     3,
				MaxCount:         5,
				SimilarityWeight: 0.7,
				RecencyWeight:    0.2,
				QualityWeight:    0.1,
				MaxContextTokens: 2000,
			},
			Export: ShadowExportConfig{
				OutputDir:                "~/.meept/shadow/exports",
				Formats:                  []string{"jsonl", "dpo"},
				MinRecords:               100,
				IncludeLowQuality:        false,
				Deduplicate:              true,
				DedupSimilarityThreshold: 0.95,
			},
			Adapters: ShadowAdaptersConfig{
				Enabled:        false,
				OllamaEndpoint: "http://localhost:11434",
				AutoTrain:      false,
				TrainThreshold: 500,
				TrainSchedule:  "",
				AdapterDir:     "~/.meept/shadow/adapters",
				LoRA: ShadowLoRAConfig{
					Rank:                 16,
					Alpha:                32,
					Dropout:              0.05,
					TargetModules:        []string{"q_proj", "v_proj", "k_proj", "o_proj"},
					LearningRate:         2e-4,
					Epochs:               3,
					BatchSize:            4,
					GradientAccumulation: 4,
					WarmupRatio:          0.03,
					MaxGradNorm:          1.0,
				},
				DPO: ShadowDPOConfig{
					Beta:     0.1,
					LossType: "sigmoid",
				},
			},
		},
		DistributedMemory: DistributedMemoryConfig{
			Enabled: false,
			Mode:    "local",
			Sync: SyncConfig{
				HydrateOnClaim:                 true,
				HydrationLimit:                 20,
				DistillOnComplete:              true,
				PeriodicDistillIntervalMinutes: 30,
				RetryOnFailure:                 true,
				MaxRetries:                     3,
			},
			Distillation: DistillationConfig{
				PageRankThreshold:        0.3,
				HubConnectivityThreshold: 5,
				PromoteTaskCompletions:   true,
				CrossAgentReferencesMin:  2,
				MinMemoryAgeMinutes:      5,
			},
		},
		QAgent: QAgentConfig{
			Enabled:                    true,
			SessionIdleTriggerHours:    12,
			AnalysisTimeoutMinutes:     30,
			MinSessionsForPattern:      5,
			MinConfidenceScore:         0.7,
			HighErrorRateThreshold:     0.2,
			HighRejectionRateThreshold: 0.3,
			DurationVarianceThreshold:  3.0,
			NotifyChat:                 true,
			NotifyCLI:                  true,
			NotifyMenuBar:              false,
			AnalysisDir:                "~/.meept/q_analysis",
			OutcomesLog:                "~/.meept/q_outcomes.jsonl",
		},
		CodeIntel: CodeIntelConfig{
			Enabled: true,
			AST: ASTConfig{
				CacheEnabled:    true,
				CacheMaxSize:    100,
				CacheTTLMinutes: 5,
			},
			LSP: LSPConfig{
				Enabled:                  false,
				Servers:                  make(map[string]LSPServerConfig),
				AutoStartServers:         true,
				ConnectionTimeoutSeconds: 10,
			},
		},
		Calendar: CalendarConfig{
			Enabled:                false,
			CalendarID:            "primary",
			RedirectURI:           "http://localhost:8888/callback",
			ReminderEnabled:       false,
			ReminderCheckInterval: "5m",
			ReminderAdvanceMinutes: 10,
		},
	}
}

// ParseLogLevel converts a string log level to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ShutdownTimeout returns the default shutdown timeout.
func (c *Config) ShutdownTimeout() time.Duration {
	return 10 * time.Second
}
