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
	Daemon            DaemonConfig            `toml:"daemon"`
	LLM               LLMConfig               `toml:"llm"`
	Memory            MemoryConfig            `toml:"memory"`
	Memvid            MemvidConfig            `toml:"memvid"`
	MultiAgent        MultiAgentConfig        `toml:"multiagent"`
	Agents            AgentsConfig            `toml:"agents"`
	Agent             AgentConfig             `toml:"agent"`
	Security          SecurityConfig          `toml:"security"`
	Scheduler         SchedulerConfig         `toml:"scheduler"`
	Queue             QueueConfig             `toml:"queue"`
	Workers           WorkersConfig           `toml:"workers"`
	Isolation         IsolationConfig         `toml:"isolation"`
	Telegram          TelegramConfig          `toml:"telegram"`
	Web               WebConfig               `toml:"web"`
	MCP               MCPConfig               `toml:"mcp"`
	Plugins           PluginsConfig           `toml:"plugins"`
	Workspace         WorkspaceConfig         `toml:"workspace"`
	Skills            SkillsConfig            `toml:"skills"`
	SelfImprove       SelfImproveConfig       `toml:"selfimprove"`
	Orchestrator      OrchestratorConfig      `toml:"orchestrator"`
	Shadow            ShadowConfig            `toml:"shadow"`
	DistributedMemory DistributedMemoryConfig `toml:"distributed_memory"`
	QAgent            QAgentConfig            `toml:"q_agent"`
	CodeIntel         CodeIntelConfig         `toml:"code_intel"`
	Calendar          CalendarConfig          `toml:"calendar"`
}

// CalendarConfig holds Google Calendar integration settings.
type CalendarConfig struct {
	// Enabled turns on Google Calendar integration
	Enabled bool `toml:"enabled"`
	// ClientID is the Google OAuth2 client ID (supports ${ENV_VAR} expansion)
	ClientID string `toml:"client_id"`
	// ClientSecret is the Google OAuth2 client secret (supports ${ENV_VAR} expansion)
	ClientSecret string `toml:"client_secret"`
	// CalendarID is the Google Calendar ID (default: "primary")
	CalendarID string `toml:"calendar_id"`
	// RedirectURI is the OAuth2 redirect URI (default: "http://localhost:8888/callback")
	RedirectURI string `toml:"redirect_uri"`
	// ReminderEnabled turns on the reminder watcher for upcoming events
	ReminderEnabled bool `toml:"reminder_enabled"`
	// ReminderCheckInterval is how often to check for upcoming events (default: 5m)
	ReminderCheckInterval string `toml:"reminder_check_interval"`
	// ReminderAdvanceMinutes triggers reminders this many minutes before an event
	ReminderAdvanceMinutes int `toml:"reminder_advance_minutes"`
}

// CodeIntelConfig holds code intelligence settings (AST/LSP).
type CodeIntelConfig struct {
	// Enabled turns on code intelligence features
	Enabled bool `toml:"enabled"`
	// AST holds AST parsing settings
	AST ASTConfig `toml:"ast"`
	// LSP holds LSP client settings
	LSP LSPConfig `toml:"lsp"`
}

// ASTConfig holds AST parsing settings.
type ASTConfig struct {
	// CacheEnabled enables parse result caching
	CacheEnabled bool `toml:"cache_enabled"`
	// CacheMaxSize is the maximum number of cached parse results
	CacheMaxSize int `toml:"cache_max_size"`
	// CacheTTLMinutes is how long cached results remain valid
	CacheTTLMinutes int `toml:"cache_ttl_minutes"`
}

// LSPConfig holds LSP client settings.
type LSPConfig struct {
	// Enabled turns on LSP client features
	Enabled bool `toml:"enabled"`
	// Servers maps language IDs to server configurations
	Servers map[string]LSPServerConfig `toml:"servers"`
	// AutoStartServers starts LSP servers on demand
	AutoStartServers bool `toml:"auto_start_servers"`
	// ConnectionTimeoutSeconds is the timeout for connecting to LSP servers
	ConnectionTimeoutSeconds int `toml:"connection_timeout_seconds"`
}

// LSPServerConfig configures a single LSP server.
type LSPServerConfig struct {
	// Command is the command to start the server
	Command string `toml:"command"`
	// Args are command line arguments
	Args []string `toml:"args"`
	// Transport is "stdio" or "tcp"
	Transport string `toml:"transport"`
	// Host is the TCP host (for tcp transport)
	Host string `toml:"host"`
	// Port is the TCP port (for tcp transport)
	Port int `toml:"port"`
	// Languages are the language IDs this server handles
	Languages []string `toml:"languages"`
}

// DaemonConfig holds daemon-specific settings.
//
//gendoc:section daemon
//gendoc:desc Configuration for the daemon process including socket, logging, and data directory.
//gendoc:example [daemon] socket_path = "~/.meept/meept.sock"
type DaemonConfig struct {
	SocketPath string `toml:"socket_path"`
	PIDFile    string `toml:"pid_file"`
	LogLevel   string `toml:"log_level"`
	DataDir    string `toml:"data_dir"`
}

// LLMConfig holds LLM configuration including budget, broker, and metrics.
//
//gendoc:section llm
//gendoc:desc LLM subsystem configuration including token budget, model broker, adaptive timeout, context firewall, and metrics.
//gendoc:example [llm] budget.hourly_token_limit = 100000
type LLMConfig struct {
	Budget          BudgetConfig          `toml:"budget"`
	Broker          LLMBrokerConfig       `toml:"broker"`
	AdaptiveTimeout LLMAdaptiveTimeoutConfig `toml:"adaptive_timeout"`
	ContextFirewall LLMContextFirewallConfig `toml:"context_firewall"`
	Metrics         LLMMetricsConfig      `toml:"metrics"`
}

// LLMBrokerConfig configures the model broker.
type LLMBrokerConfig struct {
	MaxErrorRate    float64 `toml:"max_error_rate"`     // default 0.10
	MaxP95LatencyMS float64 `toml:"max_p95_latency_ms"` // default 30000
	FallbackEnabled bool    `toml:"fallback_enabled"`   // default true
}

// LLMAdaptiveTimeoutConfig configures adaptive timeout calculation.
type LLMAdaptiveTimeoutConfig struct {
	Enabled                bool    `toml:"enabled"`                    // default true
	StddevMultiplier       float64 `toml:"stddev_multiplier"`          // default 3.0
	StddevTokenRateTimeout bool    `toml:"stddev_token_rate_timeout"`  // default true
	MinTimeoutSeconds      int     `toml:"min_timeout_seconds"`        // default 10
	MaxTimeoutSeconds      int     `toml:"max_timeout_seconds"`        // default 300
	WarmupRequests         int     `toml:"warmup_requests"`            // default 20
	WindowHours            int     `toml:"window_hours"`               // default 24
}

// LLMContextFirewallConfig configures context budget management.
type LLMContextFirewallConfig struct {
	Enabled                    bool    `toml:"enabled"`                        // default true
	SummarizeHistory           bool    `toml:"summarize_history"`              // default true
	SmallModelContextThreshold int     `toml:"small_model_context_threshold"`  // default 32768
	IterationBudgetRatio       float64 `toml:"iteration_budget_ratio"`         // default 0.30
	ConversationBudgetRatio    float64 `toml:"conversation_budget_ratio"`      // default 0.50
	ChunkLargeInputs           bool    `toml:"chunk_large_inputs"`             // default true
	ChunkThresholdRatio        float64 `toml:"chunk_threshold_ratio"`          // default 0.25
	// WrapUpThreshold is the "soft" limit (0.0-1.0) where wrap-up suggestions are injected
	WrapUpThreshold float64 `toml:"wrap_up_threshold"` // default 0.50
	// HardLimit is the "hard" limit (0.0-1.0) where context is dropped and reattempted
	HardLimit float64 `toml:"hard_limit"` // default 0.80
	// DropContextOnHardLimit enables context dropping when hard limit is hit
	DropContextOnHardLimit bool `toml:"drop_context_on_hard_limit"` // default true
}

// LLMMetricsConfig configures HTTP-level metrics collection.
type LLMMetricsConfig struct {
	Enabled             bool   `toml:"enabled"`                // default true
	DBPath              string `toml:"db_path"`                // default "~/.meept/metrics.db"
	RetentionDays       int    `toml:"retention_days"`         // default 7
	StatsRefreshMinutes int    `toml:"stats_refresh_minutes"`  // default 5
}

// BudgetConfig holds token budget settings.
type BudgetConfig struct {
	HourlyTokenLimit int     `toml:"hourly_token_limit"`
	DailyTokenLimit  int     `toml:"daily_token_limit"`
	RateLimitRPM     int     `toml:"rate_limit_rpm"`
	Aggressiveness   float64 `toml:"aggressiveness"`
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
	Enabled bool `toml:"enabled"`
	// FailClosed blocks memory operations when scanner is unavailable (default: true)
	FailClosed bool `toml:"fail_closed"`
	// LogBlocked logs blocked memory store attempts
	LogBlocked bool `toml:"log_blocked"`
}

// MemoryCachingConfig holds memory prefix caching settings (Hermes pattern).
type MemoryCachingConfig struct {
	// Enabled turns on frozen snapshot prefix caching
	Enabled bool `toml:"enabled"`
	// RefreshOnSessionEnd refreshes the snapshot at session end
	RefreshOnSessionEnd bool `toml:"refresh_on_session_end"`
}
// MemoryCategoryLimit holds character limit settings for a memory category.
type MemoryCategoryLimit struct {
	Enabled        bool `toml:"enabled"`
	CharacterLimit int  `toml:"character_limit"`
}

// MemoryLimitsConfig holds character limit settings for different memory categories.
type MemoryLimitsConfig struct {
	Episodic     MemoryCategoryLimit `toml:"episodic"`
	TaskCode     MemoryCategoryLimit `toml:"task_code"`
	TaskGeneral  MemoryCategoryLimit `toml:"task_general"`
	TaskCommands MemoryCategoryLimit `toml:"task_commands"`
	Personality  MemoryCategoryLimit `toml:"personality"`
}

// MemoryExpirationConfig holds memory expiration settings.
type MemoryExpirationConfig struct {
	// Enabled turns on access-based expiration
	Enabled bool `toml:"enabled"`
	// AccessExpirationDays is the number of days without access before expiration (0 = disabled)
	AccessExpirationDays int `toml:"access_expiration_days"`
	// SummarizeBeforeDelete creates a summary before deleting expired memories
	SummarizeBeforeDelete bool `toml:"summarize_before_delete"`
	// SummaryCategory is the category for summary memories
	SummaryCategory string `toml:"summary_category"`
}

// MemoryVersioningConfig holds versioned memory settings.
type MemoryVersioningConfig struct {
	Enabled     bool `toml:"enabled"`
	MaxVersions int  `toml:"max_versions"`
}


// MemoryConfig holds memory subsystem settings.
//
//gendoc:section memory
//gendoc:desc Memory subsystem configuration including backend selection, consolidation, episodic/task/personality memory types, embeddings, security, caching, limits, expiration, and versioning.
//gendoc:example [memory] backend = "memvid"
type MemoryConfig struct {
	// Backend specifies the storage backend: "memvid" (default) or "sqlite"
	Backend                    MemoryBackend        `toml:"backend"`
	DataDir                    string               `toml:"data_dir"`
	ConsolidationIntervalHours int                  `toml:"consolidation_interval_hours"`
	Episodic                   EpisodicConfig       `toml:"episodic"`
	Task                       TaskMemoryConfig     `toml:"task"`
	Personality                PersonalityConfig    `toml:"personality"`
	Embeddings                 EmbeddingConfig      `toml:"embeddings"`
	// Security holds memory security settings
	Security MemorySecurityConfig `toml:"security"`
	// Caching holds memory prefix caching settings
	Caching MemoryCachingConfig `toml:"caching"`
	// Limits holds character limit settings for memory categories
	Limits MemoryLimitsConfig `toml:"limits"`
	// Expiration holds memory expiration settings
	Expiration MemoryExpirationConfig `toml:"expiration"`
	// Versioning holds versioned memory settings
	Versioning MemoryVersioningConfig `toml:"versioning"`
	// ProjectOverrides allows per-project character limit overrides
	ProjectOverrides map[string]MemoryLimitsConfig `toml:"project_overrides"`
}

// EpisodicConfig holds episodic memory settings.
type EpisodicConfig struct {
	Enabled         bool `toml:"enabled"`
	MaxContextItems int  `toml:"max_context_items"`
}

// TaskMemoryConfig holds task memory settings.
type TaskMemoryConfig struct {
	Enabled bool     `toml:"enabled"`
	Domains []string `toml:"domains"`
}

// PersonalityConfig holds personality memory settings.
type PersonalityConfig struct {
	Enabled                     bool `toml:"enabled"`
	UpdateIntervalConversations int  `toml:"update_interval_conversations"`
}

// EmbeddingConfig holds vector embedding settings for semantic memory search.
type EmbeddingConfig struct {
	Enabled   bool   `toml:"enabled"`
	Provider  string `toml:"provider"` // "openai" or "ollama"
	APIKey    string `toml:"api_key"`
	BaseURL   string `toml:"base_url"`
	Model     string `toml:"model"`
	Dimension int    `toml:"dimension"`
}

// MemvidConfig holds memvid service settings.
type MemvidConfig struct {
	Enabled  bool   `toml:"enabled"`
	Endpoint string `toml:"endpoint"`
	DataDir  string `toml:"data_dir"`
	Timeout  int    `toml:"timeout_seconds"`
}

// DistributedMemoryConfig holds settings for 2-tier distributed memory sync.
type DistributedMemoryConfig struct {
	// Enabled turns on distributed memory synchronization
	Enabled bool `toml:"enabled"`
	// Mode is "local" (default, no sync) or "distributed" (sync with memvid)
	Mode string `toml:"mode"`
	// Sync configures synchronization behavior
	Sync SyncConfig `toml:"sync"`
	// Distillation configures which memories to promote
	Distillation DistillationConfig `toml:"distillation"`
}

// QAgentConfig holds configuration for the Q Agent (meta-agent for agent creation and optimization).
type QAgentConfig struct {
	// Enabled turns on Q Agent analysis and recommendations
	Enabled bool `toml:"enabled"`
	// SessionIdleTriggerHours is the idle time before a session is considered complete for analysis
	SessionIdleTriggerHours int `toml:"session_idle_trigger_hours"`
	// AnalysisTimeoutMinutes is the maximum duration for analysis runs
	AnalysisTimeoutMinutes int `toml:"analysis_timeout_minutes"`
	// MinSessionsForPattern is the minimum sessions required to detect a pattern
	MinSessionsForPattern int `toml:"min_sessions_for_pattern"`
	// MinConfidenceScore is the minimum confidence score for recommendations
	MinConfidenceScore float64 `toml:"min_confidence_score"`
	// HighErrorRateThreshold is the error rate threshold for flagging issues
	HighErrorRateThreshold float64 `toml:"high_error_rate_threshold"`
	// HighRejectionRateThreshold is the rejection rate threshold for flagging issues
	HighRejectionRateThreshold float64 `toml:"high_rejection_rate_threshold"`
	// DurationVarianceThreshold is the duration variance threshold for detecting misconfigurations
	DurationVarianceThreshold float64 `toml:"duration_variance_threshold"`
	// NotifyChat enables notifications via chat
	NotifyChat bool `toml:"notify_chat"`
	// NotifyCLI enables notifications via CLI
	NotifyCLI bool `toml:"notify_cli"`
	// NotifyMenuBar enables notifications via menu bar
	NotifyMenuBar bool `toml:"notify_menu_bar"`
	// AnalysisDir is the directory for cached analysis results
	AnalysisDir string `toml:"analysis_dir"`
	// OutcomesLog is the path for the outcomes log file
	OutcomesLog string `toml:"outcomes_log"`
}

// SyncConfig holds sync timing and behavior settings.
type SyncConfig struct {
	// HydrateOnClaim fetches relevant memories when a job is claimed
	HydrateOnClaim bool `toml:"hydrate_on_claim"`
	// HydrationLimit is max memories to fetch during hydration
	HydrationLimit int `toml:"hydration_limit"`
	// DistillOnComplete promotes memories when a job completes
	DistillOnComplete bool `toml:"distill_on_complete"`
	// PeriodicDistillIntervalMinutes runs distillation on a timer (0 = disabled)
	PeriodicDistillIntervalMinutes int `toml:"periodic_distill_interval_minutes"`
	// RetryOnFailure queues failed sync operations for retry
	RetryOnFailure bool `toml:"retry_on_failure"`
	// MaxRetries is the max retry attempts for failed operations
	MaxRetries int `toml:"max_retries"`
}

// DistillationConfig controls which memories get promoted to shared storage.
type DistillationConfig struct {
	// PageRankThreshold promotes memories with PageRank above this value
	PageRankThreshold float64 `toml:"pagerank_threshold"`
	// HubConnectivityThreshold promotes memories with degree >= this
	HubConnectivityThreshold int `toml:"hub_connectivity_threshold"`
	// PromoteTaskCompletions always promotes task completion summaries
	PromoteTaskCompletions bool `toml:"promote_task_completions"`
	// CrossAgentReferencesMin promotes memories referenced by >= N other agents
	CrossAgentReferencesMin int `toml:"cross_agent_references_min"`
	// MinMemoryAgeMinutes requires memories to be at least this old
	MinMemoryAgeMinutes int `toml:"min_memory_age_minutes"`
}

// CacheConfig holds settings for tool result caching.
type CacheConfig struct {
	// Enabled turns on tool result caching
	Enabled bool `toml:"enabled"`
	// MaxEntries is the maximum number of cached results
	MaxEntries int `toml:"max_entries"`
	// DefaultTTLSeconds is the default time-to-live for cache entries
	DefaultTTLSeconds int `toml:"default_ttl_seconds"`
	// CleanupFreqSeconds is how often to cleanup expired entries
	CleanupFreqSeconds int `toml:"cleanup_freq_seconds"`
	// EnabledTools is a list of tool names to cache (empty = all tools)
	EnabledTools []string `toml:"enabled_tools"`
}

// AgentConfig holds agent loop settings.
//
//gendoc:section agent
//gendoc:desc Agent configuration including progress streaming, caching, error handling, review workflow, validation, and watchdog monitoring.
//gendoc:example [agent] progress_enabled = true
type AgentConfig struct {
	// ProgressEnabled turns on streaming progress updates
	ProgressEnabled bool `toml:"progress_enabled"`
	// ProgressIntervalSeconds is the minimum interval between progress events
	ProgressIntervalSeconds int `toml:"progress_interval_seconds"`
	// Cache holds tool result caching settings
	Cache CacheConfig `toml:"cache"`
	// Errors holds error handling settings
	Errors ErrorsConfig `toml:"errors"`
	// Review holds code review settings
	Review ReviewConfig `toml:"review"`
	// Validation holds task completion validation settings
	Validation ValidationConfig `toml:"validation"`
	// Watchdog holds agent monitoring settings
	Watchdog WatchdogConfig `toml:"watchdog"`
}

// WatchdogConfig holds agent monitoring and timeout settings.
type WatchdogConfig struct {
	// Enabled turns on watchdog monitoring
	Enabled bool `toml:"enabled"`
	// TimeoutMinutes is the timeout in minutes before aborting (default: 10)
	TimeoutMinutes int `toml:"timeout_minutes"`
	// HeartbeatIntervalSec is the heartbeat interval in seconds (default: 30)
	HeartbeatIntervalSec int `toml:"heartbeat_interval_sec"`
	// MaxIterations is the maximum iterations before aborting (default: 50)
	MaxIterations int `toml:"max_iterations"`
	// StuckIterationCount is iterations without progress before flagged as stuck (default: 5)
	StuckIterationCount int `toml:"stuck_iteration_count"`
}

// ErrorsConfig holds error handling settings.
type ErrorsConfig struct {
	// DetailedErrors enables detailed error messages
	DetailedErrors bool `toml:"detailed_errors"`
	// IncludeExamples adds usage examples to error messages
	IncludeExamples bool `toml:"include_examples"`
	// MaxSuggestionLength limits the length of error suggestions
	MaxSuggestionLength int `toml:"max_suggestion_length"`
}

// ReviewConfig holds code review settings for the multi-agent system.
type ReviewConfig struct {
	// Enabled turns on automatic code review
	Enabled bool `toml:"enabled"`
	// RequireReview lists intent types that require review
	RequireReview []string `toml:"require_review"`
	// SkipReview lists intent types that skip review
	SkipReview []string `toml:"skip_review"`
	// ReviewerMapping maps agent IDs to reviewer agent IDs
	ReviewerMapping map[string]string `toml:"reviewer_mapping"`
	// MaxRevisionCycles is the maximum revision cycles before auto-approval
	MaxRevisionCycles int `toml:"max_revision_cycles"`
	// AutoApprovePatterns lists glob patterns that are auto-approved
	AutoApprovePatterns []string `toml:"auto_approve_patterns"`
}

// ValidationConfig holds task completion validation settings.
type ValidationConfig struct {
	// Enabled turns on task completion validation
	Enabled bool `toml:"enabled"`
	// RequireValidation lists tool hints that require validation
	RequireValidation []string `toml:"require_validation"`
	// SkipValidation lists tool hints that skip validation
	SkipValidation []string `toml:"skip_validation"`
	// SkipValidationAgents lists agents that skip validation
	SkipValidationAgents []string `toml:"skip_validation_agents"`
	// MaxValidationLoops is the maximum validation loops before escalation
	MaxValidationLoops int `toml:"max_validation_loops"`
}

// MultiAgentConfig holds multi-agent orchestration settings.
type MultiAgentConfig struct {
	Enabled            bool   `toml:"enabled"`
	DispatcherModel    string `toml:"dispatcher_model"`
	DefaultModel       string `toml:"default_model"`
	ClassifierModel    string `toml:"classifier_model"` // Model for intent classification (defaults to small_model)
	MaxMemoryRefs      int    `toml:"max_memory_refs"`
	ContextSearchLimit int    `toml:"context_search_limit"`
}

// AgentsConfig holds agent configuration settings.
type AgentsConfig struct {
	// Enabled enables the multi-agent system with TOML-based agent definitions.
	Enabled bool `toml:"enabled"`

	// ConfigDirs are directories to search for agent definition TOML files.
	// Searched in order; later directories override earlier ones.
	ConfigDirs []string `toml:"config_dirs"`

	// PromptsDir is the base directory for prompt components.
	PromptsDir string `toml:"prompts_dir"`

	// DefaultModel is the fallback model for agents that don't specify one.
	DefaultModel string `toml:"default_model"`

	// DispatcherID is the agent ID that handles intake/routing.
	DispatcherID string `toml:"dispatcher_id"`
}

// SecurityConfig holds security settings.
//
//gendoc:section security
//gendoc:desc Security configuration including input sanitization, path restrictions, output monitoring, shell command scanning, and audit logging.
//gendoc:example [security] sanitize_inputs = true
type SecurityConfig struct {
	SanitizeInputs              bool     `toml:"sanitize_inputs"`
	SanitizeStrictness          string   `toml:"sanitize_strictness"` // "permissive", "standard", "strict"
	LLMFilterExternal           bool     `toml:"llm_filter_external"`
	RequireConfirmationHigh     bool     `toml:"require_confirmation_high"`
	RequireConfirmationCritical bool     `toml:"require_confirmation_critical"`
	BlockFinancial              bool     `toml:"block_financial"`
	AllowedPaths                []string `toml:"allowed_paths"`
	BlockedPaths                []string `toml:"blocked_paths"`

	// Output monitoring
	MonitorOutput bool `toml:"monitor_output"` // Enable credential detection in LLM output
	RedactOutput  bool `toml:"redact_output"`  // Automatically redact detected credentials

	// Shell command security
	ScanShellCommands bool   `toml:"scan_shell_commands"` // Enable Tirith command scanning
	TirithBinary      string `toml:"tirith_binary"`       // Path to tirith binary

	// Audit logging
	EnableAuditLog bool   `toml:"enable_audit_log"` // Enable security audit logging
	AuditDBPath    string `toml:"audit_db_path"`    // Path to audit log database

	// Override matching behavior
	// When true, uses strict glob/exact matching for permission overrides.
	// When false (default), uses lenient three-strategy cascade (substring, glob, trimmed substring).
	// Changing this will affect existing overrides - migrate with caution.
	StrictOverrideMatching bool `toml:"strict_override_matching"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Enabled  bool   `toml:"enabled"`
	Timezone string `toml:"timezone"`
}

// QueueConfig holds job queue settings.
type QueueConfig struct {
	DBPath     string `toml:"db_path"`
	MaxRetries int    `toml:"max_retries"`
}

// WorkersConfig holds worker pool settings.
type WorkersConfig struct {
	PoolSize           int      `toml:"pool_size"`
	IdleTimeoutSeconds int      `toml:"idle_timeout_seconds"`
	DefaultCaps        []string `toml:"default_caps"`
}

// IsolationConfig holds task isolation settings.
type IsolationConfig struct {
	BaseDir     string `toml:"base_dir"`
	AutoGitInit bool   `toml:"auto_git_init"`
	AutoTest    bool   `toml:"auto_test"`
}

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	Enabled      bool    `toml:"enabled"`
	Token        string  `toml:"token"`
	CreatorID    int64   `toml:"creator_id"`
	AllowedUsers []int64 `toml:"allowed_users"` // Telegram user IDs allowed to interact (empty = all)
	AllowedChats []int64 `toml:"allowed_chats"` // Telegram chat IDs allowed (empty = all)
	PollTimeout  int     `toml:"poll_timeout"`  // Long polling timeout in seconds
}

// WebConfig holds web API settings.
type WebConfig struct {
	Enabled   bool   `toml:"enabled"`
	Host      string `toml:"host"`
	Port      int    `toml:"port"`
	SecretKey string `toml:"secret_key"`
}

// MCPConfig holds MCP settings.
type MCPConfig struct {
	Enabled    bool   `toml:"enabled"`
	ConfigFile string `toml:"config_file"`
}

// PluginsConfig holds plugin settings.
type PluginsConfig struct {
	Enabled   bool   `toml:"enabled"`
	Directory string `toml:"directory"`
}

// WorkspaceConfig holds workspace settings.
type WorkspaceConfig struct {
	Enabled          bool   `toml:"enabled"`
	BaseDir          string `toml:"base_dir"`
	AutoCommit       bool   `toml:"auto_commit"`
	CommitOnPlan     bool   `toml:"commit_on_plan"`
	CommitOnStep     bool   `toml:"commit_on_step"`
	CleanupCompleted bool   `toml:"cleanup_completed"`
}

// SkillsConfig holds skills settings.
type SkillsConfig struct {
	Enabled     bool     `toml:"enabled"`
	SearchPaths []string `toml:"search_paths"` // Additional skill directories beyond defaults
	AutoReload  bool     `toml:"auto_reload"`  // Watch for skill file changes
	CacheSize   int      `toml:"cache_size"`   // Max skills to cache in lazy loader (default: 50)
}


// SelfImproveConfig holds self-improvement settings.
type SelfImproveConfig struct {
	Enabled               bool            `toml:"enabled"`
	DataDir               string          `toml:"data_dir"`
	MaxIterationsPerCycle int             `toml:"max_iterations_per_cycle"`
	MaxFixesPerCycle      int             `toml:"max_fixes_per_cycle"`
	AutoRunIntervalHours  int             `toml:"auto_run_interval_hours"`
	AIInfra               AIInfraConfig   `toml:"ai_infra"`
	Sandbox               SandboxConfig   `toml:"sandbox"`
	Safety                SafetyConfig    `toml:"safety"`
	Detection             DetectionConfig `toml:"detection"`
}

// AIInfraConfig holds AI infrastructure settings for self-improvement.
type AIInfraConfig struct {
	Enabled         bool    `toml:"enabled"`
	BaseURL         string  `toml:"base_url"`
	APIKeyEnv       string  `toml:"api_key_env"`
	AnalysisModel   string  `toml:"analysis_model"`
	GenerationModel string  `toml:"generation_model"`
	ReviewModel     string  `toml:"review_model"`
	TimeoutSeconds  float64 `toml:"timeout_seconds"`
	MaxRetries      int     `toml:"max_retries"`
}

// SandboxConfig holds sandbox settings for self-improvement.
type SandboxConfig struct {
	WorktreeDir        string  `toml:"worktree_dir"`
	CleanupOnSuccess   bool    `toml:"cleanup_on_success"`
	CleanupOnFailure   bool    `toml:"cleanup_on_failure"`
	MaxWorktrees       int     `toml:"max_worktrees"`
	TestTimeoutSeconds float64 `toml:"test_timeout_seconds"`
}

// SafetyConfig holds safety settings for self-improvement.
type SafetyConfig struct {
	RequireHumanApproval   bool     `toml:"require_human_approval"`
	MaxFilesPerFix         int      `toml:"max_files_per_fix"`
	MaxLinesChangedPerFix  int      `toml:"max_lines_changed_per_fix"`
	BlockedPaths           []string `toml:"blocked_paths"`
	AllowedRiskLevels      []string `toml:"allowed_risk_levels"`
	BlockCriticalRisk      bool     `toml:"block_critical_risk"`
	RequireTestsPass       bool     `toml:"require_tests_pass"`
	MinConfidenceThreshold float64  `toml:"min_confidence_threshold"`
}

// DetectionConfig holds detection settings for self-improvement.
type DetectionConfig struct {
	ScanPytest       bool     `toml:"scan_pytest"`
	ScanRuntimeLogs  bool     `toml:"scan_runtime_logs"`
	ScanTypeCheck    bool     `toml:"scan_type_check"`
	ScanLint         bool     `toml:"scan_lint"`
	LogFile          string   `toml:"log_file"`
	LogLookbackHours int      `toml:"log_lookback_hours"`
	PytestArgs       []string `toml:"pytest_args"`
	MypyArgs         []string `toml:"mypy_args"`
	RuffArgs         []string `toml:"ruff_args"`
}

// OrchestratorConfig holds hierarchical orchestrator settings.
type OrchestratorConfig struct {
	MaxPlanSteps     int `toml:"max_plan_steps"`
	MaxResearchSteps int `toml:"max_research_steps"`
	PlannerTimeout   int `toml:"planner_timeout"`
	TokenBudgetAlert int `toml:"token_budget_alert"`
}

// ShadowConfig holds shadow training settings.
type ShadowConfig struct {
	Enabled   bool                  `toml:"enabled"`
	DataDir   string                `toml:"data_dir"`
	Shadowing ShadowShadowingConfig `toml:"shadowing"`
	Teacher   ShadowTeacherConfig   `toml:"teacher"`
	Quality   ShadowQualityConfig   `toml:"quality"`
	Examples  ShadowExamplesConfig  `toml:"examples"`
	Export    ShadowExportConfig    `toml:"export"`
	Adapters  ShadowAdaptersConfig  `toml:"adapters"`
}

// ShadowShadowingConfig controls when and how responses are shadowed.
type ShadowShadowingConfig struct {
	Mode          string   `toml:"mode"`
	MinComplexity string   `toml:"min_complexity"`
	Domains       []string `toml:"domains"`
	TaskTypes     []string `toml:"task_types"`
	SampleRate    float64  `toml:"sample_rate"`
	QueueSize     int      `toml:"queue_size"`
	WorkerCount   int      `toml:"worker_count"`
}

// ShadowTeacherConfig configures the teacher model.
type ShadowTeacherConfig struct {
	Model             string  `toml:"model"`
	FallbackModel     string  `toml:"fallback_model"`
	Temperature       float64 `toml:"temperature"`
	MaxTokens         int     `toml:"max_tokens"`
	TimeoutSeconds    int     `toml:"timeout_seconds"`
	MaxDailyQueries   int     `toml:"max_daily_queries"`
	MaxDailyCost      float64 `toml:"max_daily_cost"`
	RequestsPerMinute int     `toml:"requests_per_minute"`
}

// ShadowQualityConfig configures quality scoring.
type ShadowQualityConfig struct {
	Method               string                       `toml:"method"`
	HighQualityThreshold float64                      `toml:"high_quality_threshold"`
	TrainableThreshold   float64                      `toml:"trainable_threshold"`
	PreferenceMargin     float64                      `toml:"preference_margin"`
	HeuristicWeights     ShadowHeuristicWeightsConfig `toml:"heuristic_weights"`
	EvalPromptTemplate   string                       `toml:"eval_prompt_template"`
}

// ShadowHeuristicWeightsConfig defines scoring dimension weights.
type ShadowHeuristicWeightsConfig struct {
	Relevance    float64 `toml:"relevance"`
	Completeness float64 `toml:"completeness"`
	Correctness  float64 `toml:"correctness"`
	Style        float64 `toml:"style"`
}

// ShadowExamplesConfig configures few-shot example management.
type ShadowExamplesConfig struct {
	Enabled          bool    `toml:"enabled"`
	MaxPerCategory   int     `toml:"max_per_category"`
	MinQuality       float64 `toml:"min_quality"`
	DefaultCount     int     `toml:"default_count"`
	MaxCount         int     `toml:"max_count"`
	SimilarityWeight float64 `toml:"similarity_weight"`
	RecencyWeight    float64 `toml:"recency_weight"`
	QualityWeight    float64 `toml:"quality_weight"`
	MaxContextTokens int     `toml:"max_context_tokens"`
}

// ShadowExportConfig configures training data export.
type ShadowExportConfig struct {
	OutputDir                string   `toml:"output_dir"`
	Formats                  []string `toml:"formats"`
	MinRecords               int      `toml:"min_records"`
	IncludeLowQuality        bool     `toml:"include_low_quality"`
	Deduplicate              bool     `toml:"deduplicate"`
	DedupSimilarityThreshold float64  `toml:"dedup_similarity_threshold"`
}

// ShadowAdaptersConfig configures adapter management.
type ShadowAdaptersConfig struct {
	Enabled        bool             `toml:"enabled"`
	OllamaEndpoint string           `toml:"ollama_endpoint"`
	AutoTrain      bool             `toml:"auto_train"`
	TrainThreshold int              `toml:"train_threshold"`
	TrainSchedule  string           `toml:"train_schedule"`
	AdapterDir     string           `toml:"adapter_dir"`
	LoRA           ShadowLoRAConfig `toml:"lora"`
	DPO            ShadowDPOConfig  `toml:"dpo"`
}

// ShadowLoRAConfig configures LoRA training parameters.
type ShadowLoRAConfig struct {
	Rank                 int      `toml:"rank"`
	Alpha                int      `toml:"alpha"`
	Dropout              float64  `toml:"dropout"`
	TargetModules        []string `toml:"target_modules"`
	LearningRate         float64  `toml:"learning_rate"`
	Epochs               int      `toml:"epochs"`
	BatchSize            int      `toml:"batch_size"`
	GradientAccumulation int      `toml:"gradient_accumulation"`
	WarmupRatio          float64  `toml:"warmup_ratio"`
	MaxGradNorm          float64  `toml:"max_grad_norm"`
}

// ShadowDPOConfig configures DPO training parameters.
type ShadowDPOConfig struct {
	Beta     float64 `toml:"beta"`
	LossType string  `toml:"loss_type"`
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
