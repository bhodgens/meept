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
//nolint:revive // stutter with package name is intentional for API clarity
type Config struct {
	Daemon            DaemonConfig            `json:"daemon"             toml:"daemon"`
	Transport         TransportConfig         `json:"transport"          toml:"transport"`
	LLM               LLMConfig               `json:"llm"                toml:"llm"`
	Memory            MemoryConfig            `json:"memory"             toml:"memory"`
	Memvid            MemvidConfig            `json:"memvid"             toml:"memvid"`
	MultiAgent        MultiAgentConfig        `json:"multiagent"         toml:"multiagent"`
	Agents            AgentsConfig            `json:"agents"             toml:"agents"`
	Agent             AgentConfig             `json:"agent"              toml:"agent"`
	Security          SecurityConfig          `json:"security"           toml:"security"`
	Scheduler         SchedulerConfig         `json:"scheduler"          toml:"scheduler"`
	Queue             QueueConfig             `json:"queue"              toml:"queue"`
	Workers           WorkersConfig           `json:"workers"            toml:"workers"`
	Isolation         IsolationConfig         `json:"isolation"          toml:"isolation"`
	Telegram          TelegramConfig          `json:"telegram"           toml:"telegram"`
	Web               WebConfig               `json:"web"                toml:"web"`
	MCP               MCPConfig               `json:"mcp"                toml:"mcp"`
	Plugins           PluginsConfig           `json:"plugins"            toml:"plugins"`
	Workspace         WorkspaceConfig         `json:"workspace"          toml:"workspace"`
	Skills            SkillsConfig            `json:"skills"             toml:"skills"`
	SelfImprove       SelfImproveConfig       `json:"selfimprove"        toml:"selfimprove"`
	Orchestrator      OrchestratorConfig      `json:"orchestrator"       toml:"orchestrator"`
	Shadow            ShadowConfig            `json:"shadow"             toml:"shadow"`
	DistributedMemory DistributedMemoryConfig `json:"distributed_memory" toml:"distributed_memory"`
	QAgent            QAgentConfig            `json:"q_agent"            toml:"q_agent"`
	CodeIntel         CodeIntelConfig         `json:"code_intel"         toml:"code_intel"`
	Calendar          CalendarConfig          `json:"calendar"           toml:"calendar"`
	Tooling           ToolingConfig           `json:"tooling"            toml:"tooling"`
	Compaction        CompactionConfig        `json:"compaction"         toml:"compaction"`
	Session           SessionConfig           `json:"session"            toml:"session"`
	Projects          ProjectsConfig          `json:"projects"           toml:"projects"`
	Plans             PlansConfig             `json:"plans"              toml:"plans"`
	Cluster           ClusterConfig           `json:"cluster"            toml:"cluster"`
}

// ClusterConfig holds distributed cluster settings.
type ClusterConfig struct {
	// Enabled turns on cluster mode
	Enabled bool `json:"enabled" toml:"enabled"`
	// ClusterID is the unique identifier for this cluster
	ClusterID string `json:"cluster_id" toml:"cluster_id"`
	// ClusterName is the human-readable name for the cluster
	ClusterName string `json:"cluster_name" toml:"cluster_name"`
	// NodeID is this node's unique identifier
	NodeID string `json:"node_id" toml:"node_id"`
	// NodeName is this node's human-readable name
	NodeName string `json:"node_name" toml:"node_name"`
	// Network holds WireGuard network settings
	Network ClusterNetworkConfig `json:"network" toml:"network"`
	// Gossip holds gossip protocol settings
	Gossip ClusterGossipConfig `json:"gossip" toml:"gossip"`
	// Queue holds cluster queue settings
	Queue ClusterQueueConfig `json:"queue" toml:"queue"`
	// Git holds git sync settings
	Git ClusterGitConfig `json:"git" toml:"git"`
	// Security holds cluster security settings
	Security ClusterSecurityConfig `json:"security" toml:"security"`
}

// ClusterNetworkConfig holds WireGuard network settings.
type ClusterNetworkConfig struct {
	// WireGuardSubnet is the subnet for the mesh network
	WireGuardSubnet string `json:"wireguard_subnet" toml:"wireguard_subnet"`
	// WireGuardPort is the WireGuard listening port
	WireGuardPort int `json:"wireguard_port" toml:"wireguard_port"`
	// Interface is the WireGuard interface name
	Interface string `json:"interface" toml:"interface"`
}

// ClusterGossipConfig holds gossip protocol settings.
type ClusterGossipConfig struct {
	// HeartbeatInterval is how often to send heartbeats
	HeartbeatInterval time.Duration `json:"heartbeat_interval" toml:"heartbeat_interval"`
	// PeerTimeout is how long before a peer is considered unreachable
	PeerTimeout time.Duration `json:"peer_timeout" toml:"peer_timeout"`
	// EventRetention is how long to keep events
	EventRetention time.Duration `json:"event_retention" toml:"event_retention"`
	// MaxRetryAttempts is the maximum number of retry attempts for failed sends
	MaxRetryAttempts int `json:"max_retry_attempts" toml:"max_retry_attempts"`
}

// ClusterQueueConfig holds cluster queue settings.
type ClusterQueueConfig struct {
	// DefaultClaimTimeout is the default timeout for claimed jobs
	DefaultClaimTimeout time.Duration `json:"default_claim_timeout" toml:"default_claim_timeout"`
	// NodeReachabilityTimeout is how long before a node is considered unreachable
	NodeReachabilityTimeout time.Duration `json:"node_reachability_timeout" toml:"node_reachability_timeout"`
	// FullPayloadReplication enables full payload replication
	FullPayloadReplication bool `json:"full_payload_replication" toml:"full_payload_replication"`
}

// ClusterGitConfig holds git sync settings.
type ClusterGitConfig struct {
	// SyncInterval is how often to sync with the remote
	SyncInterval time.Duration `json:"sync_interval" toml:"sync_interval"`
	// HeartbeatCommit enables periodic heartbeat commits
	HeartbeatCommit bool `json:"heartbeat_commit" toml:"heartbeat_commit"`
	// RemoteURL is the git remote URL for the cluster registry
	RemoteURL string `json:"remote_url" toml:"remote_url"`
}

// ClusterSecurityConfig holds cluster security settings.
type ClusterSecurityConfig struct {
	// RequireNodeSignatures requires all node messages to be signed
	RequireNodeSignatures bool `json:"require_node_signatures" toml:"require_node_signatures"`
	// Ed25519KeyRotationDays is how often to rotate signing keys
	Ed25519KeyRotationDays int `json:"ed25519_key_rotation_days" toml:"ed25519_key_rotation_days"`
}

// DefaultClusterConfig returns default cluster configuration.
func DefaultClusterConfig() ClusterConfig {
	return ClusterConfig{
		Enabled:     false,
		ClusterID:   "",
		ClusterName: "",
		NodeID:      "",
		NodeName:    "",
		Network: ClusterNetworkConfig{
			WireGuardSubnet: "10.200.0.0/24",
			WireGuardPort:   51820,
			Interface:       "wg0",
		},
		Gossip: ClusterGossipConfig{
			HeartbeatInterval: 30 * time.Second,
			PeerTimeout:       2 * time.Minute,
			EventRetention:    1 * time.Hour,
			MaxRetryAttempts:  3,
		},
		Queue: ClusterQueueConfig{
			DefaultClaimTimeout:     5 * time.Minute,
			NodeReachabilityTimeout: 2 * time.Minute,
			FullPayloadReplication:  false,
		},
		Git: ClusterGitConfig{
			SyncInterval:    5 * time.Minute,
			HeartbeatCommit: true,
			RemoteURL:       "",
		},
		Security: ClusterSecurityConfig{
			RequireNodeSignatures:  true,
			Ed25519KeyRotationDays: 90,
		},
	}
}

// CalendarConfig holds Google Calendar integration settings.
type CalendarConfig struct {
	// Enabled turns on Google Calendar integration
	Enabled bool `json:"enabled" toml:"enabled"`
	// ClientID is the Google OAuth2 client ID (supports ${ENV_VAR} expansion)
	ClientID string `json:"client_id" toml:"client_id"`
	// ClientSecret is the Google OAuth2 client secret (supports ${ENV_VAR} expansion)
	ClientSecret string `json:"client_secret" toml:"client_secret"` //nolint:gosec // field name, not a secret
	// CalendarID is the Google Calendar ID (default: "primary")
	CalendarID string `json:"calendar_id" toml:"calendar_id"`
	// RedirectURI is the OAuth2 redirect URI (default: "http://localhost:8888/callback")
	RedirectURI string `json:"redirect_uri" toml:"redirect_uri"`
	// ReminderEnabled turns on the reminder watcher for upcoming events
	ReminderEnabled bool `json:"reminder_enabled" toml:"reminder_enabled"`
	// ReminderCheckInterval is how often to check for upcoming events (default: 5m)
	ReminderCheckInterval string `json:"reminder_check_interval" toml:"reminder_check_interval"`
	// ReminderAdvanceMinutes triggers reminders this many minutes before an event
	ReminderAdvanceMinutes int `json:"reminder_advance_minutes" toml:"reminder_advance_minutes"`
}

// CodeIntelConfig holds code intelligence settings (AST/LSP).
type CodeIntelConfig struct {
	// Enabled turns on code intelligence features
	Enabled bool `json:"enabled" toml:"enabled"`
	// AST holds AST parsing settings
	AST ASTConfig `json:"ast" toml:"ast"`
	// LSP holds LSP client settings
	LSP LSPConfig `json:"lsp" toml:"lsp"`
}

// ToolingConfig holds tool call serialization sidecar settings.
// Used for delegating tool call JSON encoding/decoding to a dedicated agent or service.
type ToolingConfig struct {
	// Enabled turns on the tooling sidecar (default: false)
	Enabled bool `json:"enabled" toml:"enabled"`
	// Mode is "service" (in-process) or "agent" (sidecar agent)
	Mode string `json:"mode" toml:"mode"`
	// AgentID is the agent ID to use when mode is "agent"
	AgentID string `json:"agent_id" toml:"agent_id"`
	// Model is the model override for the tooling agent (empty = default)
	Model string `json:"model" toml:"model"`
	// CacheEnabled enables caching of serialized tool calls
	CacheEnabled bool `json:"cache_enabled" toml:"cache_enabled"`
	// CacheMaxSize is the maximum number of cached serializations
	CacheMaxSize int `json:"cache_max_size" toml:"cache_max_size"`
	// CacheTTLMinutes is how long cached results remain valid
	CacheTTLMinutes int `json:"cache_ttl_minutes" toml:"cache_ttl_minutes"`
	// IncludeSchema includes JSON schema in tool call metadata
	IncludeSchema bool `json:"include_schema" toml:"include_schema"`
	// ValidateOnSerialize validates tool calls against schema before serialization
	ValidateOnSerialize bool `json:"validate_on_serialize" toml:"validate_on_serialize"`
	// LogUnknownTools logs warnings for unrecognized tool types
	LogUnknownTools bool `json:"log_unknown_tools" toml:"log_unknown_tools"`
}

// CompactionConfig configures LLM-based context compaction.
type CompactionConfig struct {
	Enabled           bool    `json:"enabled"             toml:"enabled"`
	Model             string  `json:"model"               toml:"model"`
	ReserveTokens     int     `json:"reserve_tokens"      toml:"reserve_tokens"`
	KeepRecentTokens  int     `json:"keep_recent_tokens"  toml:"keep_recent_tokens"`
	MaxResponseTokens int     `json:"max_response_tokens" toml:"max_response_tokens"`
	SummaryFormat     string  `json:"summary_format"      toml:"summary_format"`
	Strategy          string  `json:"strategy"            toml:"strategy"` // "structured" | "handoff" | "off"
	TriggerRatio      float64 `json:"trigger_ratio"       toml:"trigger_ratio"`
	IterativeUpdates  bool    `json:"iterative_updates"   toml:"iterative_updates"`
	TrackFileOps      bool    `json:"track_file_ops"      toml:"track_file_ops"`
	TimeoutSeconds    int     `json:"timeout_seconds"     toml:"timeout_seconds"`
}

// SessionConfig configures session persistence, branching, and compaction.
type SessionConfig struct {
	// Persistence enables restoring sessions from SQLite on startup (default: true)
	Persistence bool `json:"persistence" toml:"persistence"`
	// Branching enables conversation branching (default: true)
	Branching bool `json:"branching" toml:"branching"`
	// MaxBranches is the maximum number of branches per session (0 = unlimited, default: 20)
	MaxBranches int `json:"max_branches" toml:"max_branches"`
	// BranchSummaryThreshold is the minimum messages in an abandoned branch before auto-summarization (default: 5)
	BranchSummaryThreshold int `json:"branch_summary_threshold" toml:"branch_summary_threshold"`
	// RestoreMessageLimit is the maximum messages to restore on resumption (0 = all, default: 0)
	RestoreMessageLimit int `json:"restore_message_limit" toml:"restore_message_limit"`
	// Compaction enables tree-based compaction entries instead of deleting messages (default: true)
	Compaction bool `json:"compaction" toml:"compaction"`
	// CompactionThreshold is the minimum messages before compaction is considered (default: 50)
	CompactionThreshold int `json:"compaction_threshold" toml:"compaction_threshold"`
	// CompactionTargetRatio is the target compression ratio (0.0-1.0, default: 0.6)
	CompactionTargetRatio float64 `json:"compaction_target_ratio" toml:"compaction_target_ratio"`
	// AutoFork controls auto-fork behavior: "never", "ask", or "always" (default: "ask")
	AutoFork string `json:"auto_fork" toml:"auto_fork"`
	// LegacyTruncation reverts to old message deletion behavior instead of compaction (default: false)
	LegacyTruncation bool `json:"legacy_truncation" toml:"legacy_truncation"`
}

// ProjectsConfig holds project management and worktree settings.
type ProjectsConfig struct {
	// Enabled turns on project management features (default: true)
	Enabled bool `json:"enabled" toml:"enabled"`
	// BaseDir is the root directory for project data (default: "~/.meept/projects")
	BaseDir string `json:"base_dir" toml:"base_dir"`
	// AutoDetect enables automatic project detection from the working directory (default: true)
	AutoDetect bool `json:"auto_detect" toml:"auto_detect"`
	// WorktreePerPlan controls when a git worktree is created per plan: "auto", "always", or "never" (default: "auto")
	WorktreePerPlan string `json:"worktree_per_plan" toml:"worktree_per_plan"`
	// WorktreeIsolationThreshold is the minimum number of changed files before auto-creating a worktree (default: 5)
	WorktreeIsolationThreshold int `json:"worktree_isolation_threshold" toml:"worktree_isolation_threshold"`
	// MaxWorktreesPerProject limits concurrent worktrees per project (default: 10)
	MaxWorktreesPerProject int `json:"max_worktrees_per_project" toml:"max_worktrees_per_project"`
	// CleanupOrphanedWorktrees removes worktrees left behind after plan completion (default: true)
	CleanupOrphanedWorktrees bool `json:"cleanup_orphaned_worktrees" toml:"cleanup_orphaned_worktrees"`
	// FenceEnabled enables the project fence that restricts file access to the project root (default: true)
	FenceEnabled bool `json:"fence_enabled" toml:"fence_enabled"`
	// AllowReadSystemPaths lists system paths that may be read even when the fence is active (default: ["/usr", "/etc", "/tmp"])
	AllowReadSystemPaths []string `json:"allow_read_system_paths" toml:"allow_read_system_paths"`
	// AutoSyncOnAttach syncs the project state when attaching to an existing session (default: false)
	AutoSyncOnAttach bool `json:"auto_sync_on_attach" toml:"auto_sync_on_attach"`
	// DefaultBranch is the default git branch name for new projects (default: "main")
	DefaultBranch string `json:"default_branch" toml:"default_branch"`
}

// PlansConfig configures the plan system.
type PlansConfig struct {
	Mode         string                  `json:"mode"          toml:"mode"`
	Threshold    PlansThresholdConfig    `json:"threshold"     toml:"threshold"`
	Storage      PlansStorageConfig      `json:"storage"       toml:"storage"`
	Approval     PlansApprovalConfig     `json:"approval"      toml:"approval"`
	Confirmation PlansConfirmationConfig `json:"confirmation"  toml:"confirmation"`
}

// PlansThresholdConfig holds plan creation threshold settings.
type PlansThresholdConfig struct {
	MinSteps           int      `json:"min_steps"            toml:"min_steps"`
	ComplexityKeywords []string `json:"complexity_keywords"  toml:"complexity_keywords"`
	AlwaysPlanIntents  []string `json:"always_plan_intents"  toml:"always_plan_intents"`
}

// PlansStorageConfig holds plan storage path settings.
type PlansStorageConfig struct {
	DefaultPath      string `json:"default_path"       toml:"default_path"`
	ExternalPath     string `json:"external_path"      toml:"external_path"`
	FilenameTemplate string `json:"filename_template"  toml:"filename_template"`
}

// PlansApprovalConfig holds plan approval workflow settings.
type PlansApprovalConfig struct {
	RequireApproval   bool `json:"require_approval"    toml:"require_approval"`
	AutoApproveSimple bool `json:"auto_approve_simple" toml:"auto_approve_simple"`
	AllowRevision     bool `json:"allow_revision"      toml:"allow_revision"`
	MaxRevisions      int  `json:"max_revisions"       toml:"max_revisions"`
}

// PlansConfirmationConfig holds plan confirmation and signoff settings.
type PlansConfirmationConfig struct {
	RequireSignoff    bool `json:"require_signoff"     toml:"require_signoff"`
	AutoConfirmPhases bool `json:"auto_confirm_phases" toml:"auto_confirm_phases"`
}

// ASTConfig holds AST parsing settings.
type ASTConfig struct {
	// CacheEnabled enables parse result caching
	CacheEnabled bool `json:"cache_enabled" toml:"cache_enabled"`
	// CacheMaxSize is the maximum number of cached parse results
	CacheMaxSize int `json:"cache_max_size" toml:"cache_max_size"`
	// CacheTTLMinutes is how long cached results remain valid
	CacheTTLMinutes int `json:"cache_ttl_minutes" toml:"cache_ttl_minutes"`
}

// LSPConfig holds LSP client settings.
type LSPConfig struct {
	// Enabled turns on LSP client features
	Enabled bool `json:"enabled" toml:"enabled"`
	// Servers maps language IDs to server configurations
	Servers map[string]LSPServerConfig `json:"servers" toml:"servers"`
	// AutoStartServers starts LSP servers on demand
	AutoStartServers bool `json:"auto_start_servers" toml:"auto_start_servers"`
	// ConnectionTimeoutSeconds is the timeout for connecting to LSP servers
	ConnectionTimeoutSeconds int `json:"connection_timeout_seconds" toml:"connection_timeout_seconds"`
	// FormatOnWrite requests LSP formatting after file writes (default: false)
	FormatOnWrite bool `json:"format_on_write" toml:"format_on_write"`
	// DiagnosticsOnWrite waits for LSP diagnostics after file writes (default: false)
	DiagnosticsOnWrite bool `json:"diagnostics_on_write" toml:"diagnostics_on_write"`
	// DiagnosticsTimeout is the max seconds to wait for diagnostics (default: 5)
	DiagnosticsTimeout int `json:"diagnostics_timeout" toml:"diagnostics_timeout"`
}

// LSPServerConfig configures a single LSP server.
type LSPServerConfig struct {
	// Command is the command to start the server
	Command string `json:"command" toml:"command"`
	// Args are command line arguments
	Args []string `json:"args" toml:"args"`
	// Transport is "stdio" or "tcp"
	Transport string `json:"transport" toml:"transport"`
	// Host is the TCP host (for tcp transport)
	Host string `json:"host" toml:"host"`
	// Port is the TCP port (for tcp transport)
	Port int `json:"port" toml:"port"`
	// Languages are the language IDs this server handles
	Languages []string `json:"languages" toml:"languages"`
}

// DaemonConfig holds daemon-specific settings.
//
//gendoc:section daemon
//gendoc:desc Configuration for the daemon process including socket, logging, and data directory.
//gendoc:example [daemon] socket_path = "~/.meept/meept.sock"
type DaemonConfig struct {
	SocketPath string `json:"socket_path" toml:"socket_path"`
	PIDFile    string `json:"pid_file"    toml:"pid_file"`
	LogLevel   string `json:"log_level"   toml:"log_level"`
	DataDir    string `json:"data_dir"    toml:"data_dir"`
}

// TransportConfig controls which transports the daemon exposes.
// Clients can connect via either transport based on preference/availability.
type TransportConfig struct {
	RPC  RPCTransportConfig  `json:"rpc"  toml:"rpc"`
	HTTP HTTPTransportConfig `json:"http" toml:"http"`
}

// RPCTransportConfig configures the Unix socket RPC transport.
type RPCTransportConfig struct {
	Enabled    bool   `json:"enabled"     toml:"enabled"`     // Enable Unix socket RPC (default: true)
	SocketPath string `json:"socket_path" toml:"socket_path"` // Unix socket path (default: "~/.meept/meept.sock")
}

// HTTPTransportConfig configures the HTTP transport with modular endpoint support.
// TLS is always enabled; there are no flags to disable HTTPS.
type HTTPTransportConfig struct {
	Enabled     bool     `json:"enabled"       toml:"enabled"`       // Enable HTTP server (default: false)
	Addr        string   `json:"addr"          toml:"addr"`          // Listen address (default: ":8081")
	RequireAuth bool     `json:"require_auth"  toml:"require_auth"`  // Require API key authentication (default: true)
	APIKeys     []string `json:"api_keys"      toml:"api_keys"`      // Valid API keys for authentication
	TLSCertFile string   `json:"tls_cert_file" toml:"tls_cert_file"` // TLS certificate file path (default: ~/.meept/tls/cert.pem)
	TLSKeyFile  string   `json:"tls_key_file"  toml:"tls_key_file"`  // TLS private key file path (default: ~/.meept/tls/key.pem)

	// Modular endpoints - enable/disable individual transport features
	REST      bool   `json:"rest"       toml:"rest"`      // Enable REST API at /api/v1/* (default: true)
	WebSocket bool   `json:"websocket"  toml:"websocket"` // Enable WebSocket at /ws (default: false)
	WSPath    string `json:"ws_path"    toml:"ws_path"`   // WebSocket endpoint path (default: "/ws")
	MCP       bool   `json:"mcp"        toml:"mcp"`       // Enable MCP over HTTP+SSE at /mcp (default: false)
	MCPPath   string `json:"mcp_path"   toml:"mcp_path"`  // MCP endpoint path (default: "/mcp")

	// TLS hardening
	TLSMinVersion string `json:"tls_min_version" toml:"tls_min_version"` // "tls1.2" or "tls1.3" (default: "tls1.2")
}

// LLMConfig holds LLM configuration including budget, broker, and metrics.
//
//gendoc:section llm
//gendoc:desc LLM subsystem configuration including token budget, model broker, adaptive timeout, context firewall, and metrics.
//gendoc:example [llm] budget.hourly_token_limit = 100000
type LLMConfig struct {
	Budget          BudgetConfig             `json:"budget"           toml:"budget"`
	Broker          LLMBrokerConfig          `json:"broker"           toml:"broker"`
	AdaptiveTimeout LLMAdaptiveTimeoutConfig `json:"adaptive_timeout" toml:"adaptive_timeout"`
	ContextFirewall LLMContextFirewallConfig `json:"context_firewall" toml:"context_firewall"`
	Metrics         LLMMetricsConfig         `json:"metrics"          toml:"metrics"`
	Cache           LLMSimpleFeatureConfig   `json:"cache"            toml:"cache"`
}

// LLMSimpleFeatureConfig configures a simple feature with enabled flag and optional settings.
type LLMSimpleFeatureConfig struct {
	Enabled       bool   `json:"enabled"         toml:"enabled"`
	L1MaxEntries  int    `json:"l1_max_entries"  toml:"l1_max_entries"`
	L2Enabled     bool   `json:"l2_enabled"      toml:"l2_enabled"`
	L2DBPath      string `json:"l2_db_path"      toml:"l2_db_path"`
	DefaultTTLMin int    `json:"default_ttl_min" toml:"default_ttl_min"`
}

// LLMBrokerConfig configures the model broker.
type LLMBrokerConfig struct {
	MaxErrorRate    float64 `json:"max_error_rate"     toml:"max_error_rate"`     // default 0.10
	MaxP95LatencyMS float64 `json:"max_p95_latency_ms" toml:"max_p95_latency_ms"` // default 30000
	FallbackEnabled bool    `json:"fallback_enabled"   toml:"fallback_enabled"`   // default true
}

// LLMAdaptiveTimeoutConfig configures adaptive timeout calculation.
type LLMAdaptiveTimeoutConfig struct {
	Enabled                bool    `json:"enabled"                   toml:"enabled"`                   // default true
	StddevMultiplier       float64 `json:"stddev_multiplier"         toml:"stddev_multiplier"`         // default 3.0
	StddevTokenRateTimeout bool    `json:"stddev_token_rate_timeout" toml:"stddev_token_rate_timeout"` // default true
	MinTimeoutSeconds      int     `json:"min_timeout_seconds"       toml:"min_timeout_seconds"`       // default 10
	MaxTimeoutSeconds      int     `json:"max_timeout_seconds"       toml:"max_timeout_seconds"`       // default 300
	WarmupRequests         int     `json:"warmup_requests"           toml:"warmup_requests"`           // default 20
	WindowHours            int     `json:"window_hours"              toml:"window_hours"`              // default 24
}

// LLMContextFirewallConfig configures context budget management.
type LLMContextFirewallConfig struct {
	Enabled                    bool    `json:"enabled"                       toml:"enabled"`                       // default true
	SummarizeHistory           bool    `json:"summarize_history"             toml:"summarize_history"`             // default true
	SmallModelContextThreshold int     `json:"small_model_context_threshold" toml:"small_model_context_threshold"` // default 32768
	IterationBudgetRatio       float64 `json:"iteration_budget_ratio"        toml:"iteration_budget_ratio"`        // default 0.30
	ConversationBudgetRatio    float64 `json:"conversation_budget_ratio"     toml:"conversation_budget_ratio"`     // default 0.50
	ChunkLargeInputs           bool    `json:"chunk_large_inputs"            toml:"chunk_large_inputs"`            // default true
	ChunkThresholdRatio        float64 `json:"chunk_threshold_ratio"         toml:"chunk_threshold_ratio"`         // default 0.25
	// WrapUpThreshold is the "soft" limit (0.0-1.0) where wrap-up suggestions are injected
	WrapUpThreshold float64 `json:"wrap_up_threshold" toml:"wrap_up_threshold"` // default 0.50
	// HardLimit is the "hard" limit (0.0-1.0) where context is dropped and reattempted
	HardLimit float64 `json:"hard_limit" toml:"hard_limit"` // default 0.80
	// DropContextOnHardLimit enables context dropping when hard limit is hit
	DropContextOnHardLimit bool `json:"drop_context_on_hard_limit" toml:"drop_context_on_hard_limit"` // default true
	// ProactiveCompression enables the multi-stage ContextCompressor inside the
	// firewall. When true, the compressor runs before the legacy
	// chunk/summarize/drop pipeline.
	ProactiveCompression bool `json:"proactive_compression" toml:"proactive_compression"` // default false
	// ModelContextLimit overrides the model's ContextLimit for the compressor.
	// When zero, model.ContextLimit is used.
	ModelContextLimit int `json:"model_context_limit" toml:"model_context_limit"`
	// HierarchicalSummarization enables recursive re-summarization where
	// summaries that exceed SummaryLevelThreshold tokens are themselves
	// summarized at the next level up to MaxSummaryLevel.
	HierarchicalSummarization bool `json:"hierarchical_summarization" toml:"hierarchical_summarization"`
	// MaxSummaryLevel is the maximum recursion depth for hierarchical
	// summarization (default 3).
	MaxSummaryLevel int `json:"max_summary_level" toml:"max_summary_level"`
	// SummaryLevelThreshold is the token count at which a summary is
	// re-summarized at the next level (default 500).
	SummaryLevelThreshold int `json:"summary_level_threshold" toml:"summary_level_threshold"`
}

// LLMMetricsConfig configures HTTP-level metrics collection.
type LLMMetricsConfig struct {
	Enabled             bool   `json:"enabled"               toml:"enabled"`               // default true
	DBPath              string `json:"db_path"               toml:"db_path"`               // default "~/.meept/metrics.db"
	RetentionDays       int    `json:"retention_days"        toml:"retention_days"`        // default 7
	StatsRefreshMinutes int    `json:"stats_refresh_minutes" toml:"stats_refresh_minutes"` // default 5
}

// BudgetConfig holds token budget settings.
type BudgetConfig struct {
	HourlyTokenLimit     int     `json:"hourly_token_limit" toml:"hourly_token_limit"`
	DailyTokenLimit      int     `json:"daily_token_limit"  toml:"daily_token_limit"`
	DailyCostLimit       float64 `json:"daily_cost_limit"   toml:"daily_cost_limit"`    // Max USD per UTC day (0 = no limit)
	HourlyCostLimit      float64 `json:"hourly_cost_limit"  toml:"hourly_cost_limit"`   // Max USD per sliding hour (0 = no limit)
	RateLimitRPM         int     `json:"rate_limit_rpm"     toml:"rate_limit_rpm"`
	Aggressiveness       float64 `json:"aggressiveness"     toml:"aggressiveness"`
	PerTaskTokenLimit    int     `json:"per_task_token_limit"  toml:"per_task_token_limit"`      // max tokens per single task (0 = no cap)
	PerSessionTokenLimit int     `json:"per_session_token_limit" toml:"per_session_token_limit"` // max tokens per single session (0 = no cap)
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
	Enabled bool `json:"enabled" toml:"enabled"`
	// FailClosed blocks memory operations when scanner is unavailable (default: true)
	FailClosed bool `json:"fail_closed" toml:"fail_closed"`
	// LogBlocked logs blocked memory store attempts
	LogBlocked bool `json:"log_blocked" toml:"log_blocked"`
}

// MemoryCachingConfig holds memory prefix caching settings (Hermes pattern).
type MemoryCachingConfig struct {
	// Enabled turns on frozen snapshot prefix caching
	Enabled bool `json:"enabled" toml:"enabled"`
	// RefreshOnSessionEnd refreshes the snapshot at session end
	RefreshOnSessionEnd bool `json:"refresh_on_session_end" toml:"refresh_on_session_end"`
}

// MemoryCategoryLimit holds character limit settings for a memory category.
type MemoryCategoryLimit struct {
	Enabled        bool `json:"enabled"         toml:"enabled"`
	CharacterLimit int  `json:"character_limit" toml:"character_limit"`
}

// MemoryLimitsConfig holds character limit settings for different memory categories.
type MemoryLimitsConfig struct {
	Episodic     MemoryCategoryLimit `json:"episodic"      toml:"episodic"`
	TaskCode     MemoryCategoryLimit `json:"task_code"     toml:"task_code"`
	TaskGeneral  MemoryCategoryLimit `json:"task_general"  toml:"task_general"`
	TaskCommands MemoryCategoryLimit `json:"task_commands" toml:"task_commands"`
	Personality  MemoryCategoryLimit `json:"personality"   toml:"personality"`
}

// MemoryExpirationConfig holds memory expiration settings.
type MemoryExpirationConfig struct {
	// Enabled turns on access-based expiration
	Enabled bool `json:"enabled" toml:"enabled"`
	// AccessExpirationDays is the number of days without access before expiration (0 = disabled)
	AccessExpirationDays int `json:"access_expiration_days" toml:"access_expiration_days"`
	// SummarizeBeforeDelete creates a summary before deleting expired memories
	SummarizeBeforeDelete bool `json:"summarize_before_delete" toml:"summarize_before_delete"`
	// SummaryCategory is the category for summary memories
	SummaryCategory string `json:"summary_category" toml:"summary_category"`
}

// MemoryVersioningConfig holds versioned memory settings.
type MemoryVersioningConfig struct {
	Enabled     bool `json:"enabled"      toml:"enabled"`
	MaxVersions int  `json:"max_versions" toml:"max_versions"`
}

// MemoryConfig holds memory subsystem settings.
//
//gendoc:section memory
//gendoc:desc Memory subsystem configuration including backend selection, consolidation, episodic/task/personality memory types, embeddings, security, caching, limits, expiration, and versioning.
//gendoc:example [memory] backend = "memvid"
type MemoryConfig struct {
	// Backend specifies the storage backend: "memvid" (default) or "sqlite"
	Backend                    MemoryBackend     `json:"backend"                      toml:"backend"`
	DataDir                    string            `json:"data_dir"                     toml:"data_dir"`
	ConsolidationIntervalHours int               `json:"consolidation_interval_hours" toml:"consolidation_interval_hours"`
	Episodic                   EpisodicConfig    `json:"episodic"                     toml:"episodic"`
	Task                       TaskMemoryConfig  `json:"task"                         toml:"task"`
	Personality                PersonalityConfig `json:"personality"                  toml:"personality"`
	Embeddings                 EmbeddingConfig   `json:"embeddings"                   toml:"embeddings"`
	// Security holds memory security settings
	Security MemorySecurityConfig `json:"security" toml:"security"`
	// Caching holds memory prefix caching settings
	Caching MemoryCachingConfig `json:"caching" toml:"caching"`
	// Limits holds character limit settings for memory categories
	Limits MemoryLimitsConfig `json:"limits" toml:"limits"`
	// Expiration holds memory expiration settings
	Expiration MemoryExpirationConfig `json:"expiration" toml:"expiration"`
	// Versioning holds versioned memory settings
	Versioning MemoryVersioningConfig `json:"versioning" toml:"versioning"`
	// ProjectOverrides allows per-project character limit overrides
	ProjectOverrides map[string]MemoryLimitsConfig `json:"project_overrides" toml:"project_overrides"`
}

// EpisodicConfig holds episodic memory settings.
type EpisodicConfig struct {
	Enabled         bool `json:"enabled"           toml:"enabled"`
	MaxContextItems int  `json:"max_context_items" toml:"max_context_items"`
}

// TaskMemoryConfig holds task memory settings.
type TaskMemoryConfig struct {
	Enabled bool     `json:"enabled" toml:"enabled"`
	Domains []string `json:"domains" toml:"domains"`
}

// PersonalityConfig holds personality memory settings.
type PersonalityConfig struct {
	Enabled                     bool `json:"enabled"                       toml:"enabled"`
	UpdateIntervalConversations int  `json:"update_interval_conversations" toml:"update_interval_conversations"`
}

// EmbeddingConfig holds vector embedding settings for semantic memory search.
type EmbeddingConfig struct {
	Enabled   bool   `json:"enabled"   toml:"enabled"`
	Provider  string `json:"provider"  toml:"provider"` // "openai" or "ollama"
	APIKey    string `json:"api_key"   toml:"api_key"`  //nolint:gosec // field name, not a secret
	BaseURL   string `json:"base_url"  toml:"base_url"`
	Model     string `json:"model"     toml:"model"`
	Dimension int    `json:"dimension" toml:"dimension"`
}

// MemvidConfig holds memvid service settings.
type MemvidConfig struct {
	Enabled  bool   `json:"enabled"         toml:"enabled"`
	Endpoint string `json:"endpoint"        toml:"endpoint"`
	DataDir  string `json:"data_dir"        toml:"data_dir"`
	Timeout  int    `json:"timeout_seconds" toml:"timeout_seconds"`
}

// DistributedMemoryConfig holds settings for 2-tier distributed memory sync.
type DistributedMemoryConfig struct {
	// Enabled turns on distributed memory synchronization
	Enabled bool `json:"enabled" toml:"enabled"`
	// Mode is "local" (default, no sync) or "distributed" (sync with memvid)
	Mode string `json:"mode" toml:"mode"`
	// Sync configures synchronization behavior
	Sync SyncConfig `json:"sync" toml:"sync"`
	// Distillation configures which memories to promote
	Distillation DistillationConfig `json:"distillation" toml:"distillation"`
}

// QAgentConfig holds configuration for the Q Agent (meta-agent for agent creation and optimization).
type QAgentConfig struct {
	// Enabled turns on Q Agent analysis and recommendations
	Enabled bool `json:"enabled" toml:"enabled"`
	// SessionIdleTriggerHours is the idle time before a session is considered complete for analysis
	SessionIdleTriggerHours int `json:"session_idle_trigger_hours" toml:"session_idle_trigger_hours"`
	// AnalysisTimeoutMinutes is the maximum duration for analysis runs
	AnalysisTimeoutMinutes int `json:"analysis_timeout_minutes" toml:"analysis_timeout_minutes"`
	// MinSessionsForPattern is the minimum sessions required to detect a pattern
	MinSessionsForPattern int `json:"min_sessions_for_pattern" toml:"min_sessions_for_pattern"`
	// MinConfidenceScore is the minimum confidence score for recommendations
	MinConfidenceScore float64 `json:"min_confidence_score" toml:"min_confidence_score"`
	// HighErrorRateThreshold is the error rate threshold for flagging issues
	HighErrorRateThreshold float64 `json:"high_error_rate_threshold" toml:"high_error_rate_threshold"`
	// HighRejectionRateThreshold is the rejection rate threshold for flagging issues
	HighRejectionRateThreshold float64 `json:"high_rejection_rate_threshold" toml:"high_rejection_rate_threshold"`
	// DurationVarianceThreshold is the duration variance threshold for detecting misconfigurations
	DurationVarianceThreshold float64 `json:"duration_variance_threshold" toml:"duration_variance_threshold"`
	// NotifyChat enables notifications via chat
	NotifyChat bool `json:"notify_chat" toml:"notify_chat"`
	// NotifyCLI enables notifications via CLI
	NotifyCLI bool `json:"notify_cli" toml:"notify_cli"`
	// NotifyMenuBar enables notifications via menu bar
	NotifyMenuBar bool `json:"notify_menu_bar" toml:"notify_menu_bar"`
	// AnalysisDir is the directory for cached analysis results
	AnalysisDir string `json:"analysis_dir" toml:"analysis_dir"`
	// OutcomesLog is the path for the outcomes log file
	OutcomesLog string `json:"outcomes_log" toml:"outcomes_log"`
}

// SyncConfig holds sync timing and behavior settings.
type SyncConfig struct {
	// HydrateOnClaim fetches relevant memories when a job is claimed
	HydrateOnClaim bool `json:"hydrate_on_claim" toml:"hydrate_on_claim"`
	// HydrationLimit is max memories to fetch during hydration
	HydrationLimit int `json:"hydration_limit" toml:"hydration_limit"`
	// DistillOnComplete promotes memories when a job completes
	DistillOnComplete bool `json:"distill_on_complete" toml:"distill_on_complete"`
	// PeriodicDistillIntervalMinutes runs distillation on a timer (0 = disabled)
	PeriodicDistillIntervalMinutes int `json:"periodic_distill_interval_minutes" toml:"periodic_distill_interval_minutes"`
	// RetryOnFailure queues failed sync operations for retry
	RetryOnFailure bool `json:"retry_on_failure" toml:"retry_on_failure"`
	// MaxRetries is the max retry attempts for failed operations
	MaxRetries int `json:"max_retries" toml:"max_retries"`
}

// DistillationConfig controls which memories get promoted to shared storage.
type DistillationConfig struct {
	// PageRankThreshold promotes memories with PageRank above this value
	PageRankThreshold float64 `json:"pagerank_threshold" toml:"pagerank_threshold"`
	// HubConnectivityThreshold promotes memories with degree >= this
	HubConnectivityThreshold int `json:"hub_connectivity_threshold" toml:"hub_connectivity_threshold"`
	// PromoteTaskCompletions always promotes task completion summaries
	PromoteTaskCompletions bool `json:"promote_task_completions" toml:"promote_task_completions"`
	// CrossAgentReferencesMin promotes memories referenced by >= N other agents
	CrossAgentReferencesMin int `json:"cross_agent_references_min" toml:"cross_agent_references_min"`
	// MinMemoryAgeMinutes requires memories to be at least this old
	MinMemoryAgeMinutes int `json:"min_memory_age_minutes" toml:"min_memory_age_minutes"`
}

// CacheConfig holds settings for tool result caching.
type CacheConfig struct {
	// Enabled turns on tool result caching
	Enabled bool `json:"enabled" toml:"enabled"`
	// MaxEntries is the maximum number of cached results
	MaxEntries int `json:"max_entries" toml:"max_entries"`
	// DefaultTTLSeconds is the default time-to-live for cache entries
	DefaultTTLSeconds int `json:"default_ttl_seconds" toml:"default_ttl_seconds"`
	// CleanupFreqSeconds is how often to cleanup expired entries
	CleanupFreqSeconds int `json:"cleanup_freq_seconds" toml:"cleanup_freq_seconds"`
	// EnabledTools is a list of tool names to cache (empty = all tools)
	EnabledTools []string `json:"enabled_tools" toml:"enabled_tools"`
}

// AgentConfig holds agent loop settings.
//
//gendoc:section agent
//gendoc:desc Agent configuration including progress streaming, caching, error handling, review workflow, validation, and watchdog monitoring.
//gendoc:example [agent] progress_enabled = true
type AgentConfig struct {
	// ProgressEnabled turns on streaming progress updates
	ProgressEnabled bool `json:"progress_enabled" toml:"progress_enabled"`
	// ProgressIntervalSeconds is the minimum interval between progress events
	ProgressIntervalSeconds int `json:"progress_interval_seconds" toml:"progress_interval_seconds"`
	// Cache holds tool result caching settings
	Cache CacheConfig `json:"cache" toml:"cache"`
	// Errors holds error handling settings
	Errors ErrorsConfig `json:"errors" toml:"errors"`
	// Review holds code review settings
	Review ReviewConfig `json:"review" toml:"review"`
	// Validation holds task completion validation settings
	Validation ValidationConfig `json:"validation" toml:"validation"`
	// Watchdog holds agent monitoring settings
	Watchdog WatchdogConfig `json:"watchdog" toml:"watchdog"`
	// Queues holds steering and follow-up message queue settings
	Queues AgentQueuesConfig `json:"queues" toml:"queues"`
}

// WatchdogConfig holds agent monitoring and timeout settings.
type WatchdogConfig struct {
	// Enabled turns on watchdog monitoring
	Enabled bool `json:"enabled" toml:"enabled"`
	// TimeoutMinutes is the timeout in minutes before aborting (default: 10)
	TimeoutMinutes int `json:"timeout_minutes" toml:"timeout_minutes"`
	// HeartbeatIntervalSec is the heartbeat interval in seconds (default: 30)
	HeartbeatIntervalSec int `json:"heartbeat_interval_sec" toml:"heartbeat_interval_sec"`
	// MaxIterations is the maximum iterations before aborting (default: 50)
	MaxIterations int `json:"max_iterations" toml:"max_iterations"`
	// StuckIterationCount is iterations without progress before flagged as stuck (default: 5)
	StuckIterationCount int `json:"stuck_iteration_count" toml:"stuck_iteration_count"`
}

// AgentQueuesConfig holds steering and follow-up message queue settings.
type AgentQueuesConfig struct {
	// Enabled controls whether the steering/follow-up queue feature is active.
	// When false, agent loops are created without message queues and all
	// messages go through the standard synchronous path.
	Enabled bool `json:"enabled" toml:"enabled"`
	// SteeringDrain controls how the steering queue drains.
	// Always "one" -- only one steering message is valid at a time.
	SteeringDrain string `json:"steering_drain" toml:"steering_drain"`
	// FollowUpDrain controls how the follow-up queue drains.
	// "one" returns a single message, "all" returns all pending.
	FollowUpDrain string `json:"followup_drain" toml:"followup_drain"`
	// MaxSteering is the maximum number of steering messages allowed.
	// Low value -- they're urgent and should interrupt immediately.
	MaxSteering int `json:"max_steering" toml:"max_steering"`
	// MaxFollowUp is the maximum number of follow-up messages allowed.
	MaxFollowUp int `json:"max_followup" toml:"max_followup"`
	// PersistFollowUp enables hybrid persistence (write-behind to SQLite).
	PersistFollowUp bool `json:"persist_followup" toml:"persist_followup"`
	// FlushDelayMs is the write-behind flush delay in milliseconds.
	// When PersistFollowUp is true, messages are batched in memory and
	// flushed to disk after this delay or on daemon shutdown.
	FlushDelayMs int `json:"flush_delay_ms" toml:"flush_delay_ms"`
}

// ErrorsConfig holds error handling settings.
type ErrorsConfig struct {
	// DetailedErrors enables detailed error messages
	DetailedErrors bool `json:"detailed_errors" toml:"detailed_errors"`
	// IncludeExamples adds usage examples to error messages
	IncludeExamples bool `json:"include_examples" toml:"include_examples"`
	// MaxSuggestionLength limits the length of error suggestions
	MaxSuggestionLength int `json:"max_suggestion_length" toml:"max_suggestion_length"`
}

// ReviewConfig holds code review settings for the multi-agent system.
type ReviewConfig struct {
	// Enabled turns on automatic code review
	Enabled bool `json:"enabled" toml:"enabled"`
	// RequireReview lists intent types that require review
	RequireReview []string `json:"require_review" toml:"require_review"`
	// SkipReview lists intent types that skip review
	SkipReview []string `json:"skip_review" toml:"skip_review"`
	// ReviewerMapping maps agent IDs to reviewer agent IDs
	ReviewerMapping map[string]string `json:"reviewer_mapping" toml:"reviewer_mapping"`
	// MaxRevisionCycles is the maximum revision cycles before auto-approval
	MaxRevisionCycles int `json:"max_revision_cycles" toml:"max_revision_cycles"`
	// AutoApprovePatterns lists glob patterns that are auto-approved
	AutoApprovePatterns []string `json:"auto_approve_patterns" toml:"auto_approve_patterns"`
}

// ValidationConfig holds task completion validation settings.
type ValidationConfig struct {
	// Enabled turns on task completion validation
	Enabled bool `json:"enabled" toml:"enabled"`
	// RequireValidation lists tool hints that require validation
	RequireValidation []string `json:"require_validation" toml:"require_validation"`
	// SkipValidation lists tool hints that skip validation
	SkipValidation []string `json:"skip_validation" toml:"skip_validation"`
	// SkipValidationAgents lists agents that skip validation
	SkipValidationAgents []string `json:"skip_validation_agents" toml:"skip_validation_agents"`
	// MaxValidationLoops is the maximum validation loops before escalation
	MaxValidationLoops int `json:"max_validation_loops" toml:"max_validation_loops"`
}

// MultiAgentConfig holds multi-agent orchestration settings.
type MultiAgentConfig struct {
	Enabled            bool   `json:"enabled"              toml:"enabled"`
	DispatcherModel    string `json:"dispatcher_model"     toml:"dispatcher_model"`
	DefaultModel       string `json:"default_model"        toml:"default_model"`
	ClassifierModel    string `json:"classifier_model"     toml:"classifier_model"` // Model for intent classification (defaults to small_model)
	MaxMemoryRefs      int    `json:"max_memory_refs"      toml:"max_memory_refs"`
	ContextSearchLimit int    `json:"context_search_limit" toml:"context_search_limit"`
}

// AgentsConfig holds agent configuration settings.
type AgentsConfig struct {
	// Enabled enables the multi-agent system with TOML-based agent definitions.
	Enabled bool `json:"enabled" toml:"enabled"`

	// ConfigDirs are directories to search for agent definition TOML files.
	// Searched in order; later directories override earlier ones.
	ConfigDirs []string `json:"config_dirs" toml:"config_dirs"`

	// PromptsDir is the base directory for prompt components.
	PromptsDir string `json:"prompts_dir" toml:"prompts_dir"`

	// DefaultModel is the fallback model for agents that don't specify one.
	DefaultModel string `json:"default_model" toml:"default_model"`

	// DispatcherID is the agent ID that handles intake/routing.
	DispatcherID string `json:"dispatcher_id" toml:"dispatcher_id"`
}

// SecurityConfig holds security settings.
//
//gendoc:section security
//gendoc:desc Security configuration including input sanitization, path restrictions, output monitoring, shell command scanning, and audit logging.
//gendoc:example [security] sanitize_inputs = true
type SecurityConfig struct {
	SanitizeInputs              bool     `json:"sanitize_inputs"               toml:"sanitize_inputs"`
	SanitizeStrictness          string   `json:"sanitize_strictness"           toml:"sanitize_strictness"` // "permissive", "standard", "strict"
	LLMFilterExternal           bool     `json:"llm_filter_external"           toml:"llm_filter_external"`
	RequireConfirmationHigh     bool     `json:"require_confirmation_high"     toml:"require_confirmation_high"`
	RequireConfirmationCritical bool     `json:"require_confirmation_critical" toml:"require_confirmation_critical"`
	BlockFinancial              bool     `json:"block_financial"               toml:"block_financial"`
	AllowedPaths                []string `json:"allowed_paths"                 toml:"allowed_paths"`
	BlockedPaths                []string `json:"blocked_paths"                 toml:"blocked_paths"`

	// Output monitoring
	MonitorOutput bool `json:"monitor_output" toml:"monitor_output"` // Enable credential detection in LLM output
	RedactOutput  bool `json:"redact_output"  toml:"redact_output"`  //nolint:gosec // field name, not a secret // Automatically redact detected credentials

	// Shell command security
	ScanShellCommands bool   `json:"scan_shell_commands" toml:"scan_shell_commands"` // Enable Tirith command scanning
	TirithBinary      string `json:"tirith_binary"       toml:"tirith_binary"`       // Path to tirith binary

	// Audit logging
	EnableAuditLog bool   `json:"enable_audit_log" toml:"enable_audit_log"` // Enable security audit logging
	AuditDBPath    string `json:"audit_db_path"    toml:"audit_db_path"`    // Path to audit log database

	// Override matching behavior
	// When true, uses strict glob/exact matching for permission overrides.
	// When false (default), uses lenient three-strategy cascade (substring, glob, trimmed substring).
	// Changing this will affect existing overrides - migrate with caution.
	StrictOverrideMatching bool `json:"strict_override_matching" toml:"strict_override_matching"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Enabled  bool   `json:"enabled"  toml:"enabled"`
	Timezone string `json:"timezone" toml:"timezone"`
}

// QueueConfig holds job queue settings.
type QueueConfig struct {
	DBPath     string `json:"db_path"     toml:"db_path"`
	MaxRetries int    `json:"max_retries" toml:"max_retries"`
}

// WorkersConfig holds worker pool settings.
type WorkersConfig struct {
	PoolSize           int      `json:"pool_size"            toml:"pool_size"`
	IdleTimeoutSeconds int      `json:"idle_timeout_seconds" toml:"idle_timeout_seconds"`
	DefaultCaps        []string `json:"default_caps"         toml:"default_caps"`
}

// IsolationConfig holds task isolation settings.
type IsolationConfig struct {
	BaseDir     string `json:"base_dir"      toml:"base_dir"`
	AutoGitInit bool   `json:"auto_git_init" toml:"auto_git_init"`
	AutoTest    bool   `json:"auto_test"     toml:"auto_test"`
}

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	Enabled      bool    `json:"enabled"       toml:"enabled"`
	Token        string  `json:"token"         toml:"token"`
	CreatorID    int64   `json:"creator_id"    toml:"creator_id"`
	AllowedUsers []int64 `json:"allowed_users" toml:"allowed_users"` // Telegram user IDs allowed to interact (empty = all)
	AllowedChats []int64 `json:"allowed_chats" toml:"allowed_chats"` // Telegram chat IDs allowed (empty = all)
	PollTimeout  int     `json:"poll_timeout"  toml:"poll_timeout"`  // Long polling timeout in seconds
}

// WebConfig holds web API settings.
type WebConfig struct {
	Enabled   bool   `json:"enabled"    toml:"enabled"`
	Host      string `json:"host"       toml:"host"`
	Port      int    `json:"port"       toml:"port"`
	SecretKey string `json:"secret_key" toml:"secret_key"` //nolint:gosec // field name, not a secret
}

// MCPConfig holds MCP settings.
type MCPConfig struct {
	Enabled    bool   `json:"enabled"     toml:"enabled"`
	ConfigFile string `json:"config_file" toml:"config_file"`
}

// PluginsConfig holds plugin settings.
type PluginsConfig struct {
	Enabled   bool   `json:"enabled"   toml:"enabled"`
	Directory string `json:"directory" toml:"directory"`
}

// WorkspaceConfig holds workspace settings.
type WorkspaceConfig struct {
	Enabled          bool   `json:"enabled"           toml:"enabled"`
	BaseDir          string `json:"base_dir"          toml:"base_dir"`
	AutoCommit       bool   `json:"auto_commit"       toml:"auto_commit"`
	CommitOnPlan     bool   `json:"commit_on_plan"    toml:"commit_on_plan"`
	CommitOnStep     bool   `json:"commit_on_step"    toml:"commit_on_step"`
	CleanupCompleted bool   `json:"cleanup_completed" toml:"cleanup_completed"`
}

// SkillsConfig holds skills settings.
type SkillsConfig struct {
	Enabled     bool     `json:"enabled"           toml:"enabled"`
	SearchPaths []string `json:"search_paths"      toml:"search_paths"`      // Additional skill directories beyond defaults
	AutoReload  bool     `json:"auto_reload"       toml:"auto_reload"`       // Watch for skill file changes
	CacheSize   int      `json:"max_cached_skills" toml:"max_cached_skills"` // Max skills to cache in lazy loader (default: 50)
}

// SelfImproveConfig holds self-improvement settings.
type SelfImproveConfig struct {
	Enabled               bool            `json:"enabled"                  toml:"enabled"`
	DataDir               string          `json:"data_dir"                 toml:"data_dir"`
	MaxIterationsPerCycle int             `json:"max_iterations_per_cycle" toml:"max_iterations_per_cycle"`
	MaxFixesPerCycle      int             `json:"max_fixes_per_cycle"      toml:"max_fixes_per_cycle"`
	AutoRunIntervalHours  int             `json:"auto_run_interval_hours"  toml:"auto_run_interval_hours"`
	AIInfra               AIInfraConfig   `json:"ai_infra"                 toml:"ai_infra"`
	Sandbox               SandboxConfig   `json:"sandbox"                  toml:"sandbox"`
	Safety                SafetyConfig    `json:"safety"                   toml:"safety"`
	Detection             DetectionConfig `json:"detection"                toml:"detection"`
}

// AIInfraConfig holds AI infrastructure settings for self-improvement.
type AIInfraConfig struct {
	Enabled         bool    `json:"enabled"          toml:"enabled"`
	BaseURL         string  `json:"base_url"         toml:"base_url"`
	APIKeyEnv       string  `json:"api_key_env"      toml:"api_key_env"` //nolint:gosec // field name, not a secret
	AnalysisModel   string  `json:"analysis_model"   toml:"analysis_model"`
	GenerationModel string  `json:"generation_model" toml:"generation_model"`
	ReviewModel     string  `json:"review_model"     toml:"review_model"`
	TimeoutSeconds  float64 `json:"timeout_seconds"  toml:"timeout_seconds"`
	MaxRetries      int     `json:"max_retries"      toml:"max_retries"`
}

// SandboxConfig holds sandbox settings for self-improvement.
type SandboxConfig struct {
	WorktreeDir        string  `json:"worktree_dir"         toml:"worktree_dir"`
	CleanupOnSuccess   bool    `json:"cleanup_on_success"   toml:"cleanup_on_success"`
	CleanupOnFailure   bool    `json:"cleanup_on_failure"   toml:"cleanup_on_failure"`
	MaxWorktrees       int     `json:"max_worktrees"        toml:"max_worktrees"`
	TestTimeoutSeconds float64 `json:"test_timeout_seconds" toml:"test_timeout_seconds"`
}

// SafetyConfig holds safety settings for self-improvement.
type SafetyConfig struct {
	RequireHumanApproval   bool     `json:"require_human_approval"    toml:"require_human_approval"`
	MaxFilesPerFix         int      `json:"max_files_per_fix"         toml:"max_files_per_fix"`
	MaxLinesChangedPerFix  int      `json:"max_lines_changed_per_fix" toml:"max_lines_changed_per_fix"`
	BlockedPaths           []string `json:"blocked_paths"             toml:"blocked_paths"`
	AllowedRiskLevels      []string `json:"allowed_risk_levels"       toml:"allowed_risk_levels"`
	BlockCriticalRisk      bool     `json:"block_critical_risk"       toml:"block_critical_risk"`
	RequireTestsPass       bool     `json:"require_tests_pass"        toml:"require_tests_pass"`
	MinConfidenceThreshold float64  `json:"min_confidence_threshold"  toml:"min_confidence_threshold"`
}

// DetectionConfig holds detection settings for self-improvement.
type DetectionConfig struct {
	ScanPytest       bool     `json:"scan_pytest"        toml:"scan_pytest"`
	ScanRuntimeLogs  bool     `json:"scan_runtime_logs"  toml:"scan_runtime_logs"`
	ScanTypeCheck    bool     `json:"scan_type_check"    toml:"scan_type_check"`
	ScanLint         bool     `json:"scan_lint"          toml:"scan_lint"`
	LogFile          string   `json:"log_file"           toml:"log_file"`
	LogLookbackHours int      `json:"log_lookback_hours" toml:"log_lookback_hours"`
	PytestArgs       []string `json:"pytest_args"        toml:"pytest_args"`
	MypyArgs         []string `json:"mypy_args"          toml:"mypy_args"`
	RuffArgs         []string `json:"ruff_args"          toml:"ruff_args"`

	// Code-scanning error patterns (FIX #0056)
	CodeErrorPatterns []string `json:"code_error_patterns" toml:"code_error_patterns"`
	// TODO de-duplication
	MaxCodeIssuesPerFile int  `json:"max_code_issues_per_file" toml:"max_code_issues_per_file"`
	DeduplicateTODOs     bool `json:"deduplicate_todos" toml:"deduplicate_todos"`
}

// OrchestratorConfig holds hierarchical orchestrator settings.
type OrchestratorConfig struct {
	MaxPlanSteps       int  `json:"max_plan_steps"        toml:"max_plan_steps"`
	MaxResearchSteps   int  `json:"max_research_steps"    toml:"max_research_steps"`
	PlannerTimeout     int  `json:"planner_timeout"       toml:"planner_timeout"`
	TokenBudgetAlert   int  `json:"token_budget_alert"    toml:"token_budget_alert"`
	MaxHandoffSteps    int  `json:"max_handoff_steps"     toml:"max_handoff_steps"`
	HandoffUseAmendment bool `json:"handoff_use_amendment" toml:"handoff_use_amendment"`
}

// ShadowConfig holds shadow training settings.
type ShadowConfig struct {
	Enabled   bool                  `json:"enabled"   toml:"enabled"`
	DataDir   string                `json:"data_dir"  toml:"data_dir"`
	Shadowing ShadowShadowingConfig `json:"shadowing" toml:"shadowing"`
	Teacher   ShadowTeacherConfig   `json:"teacher"   toml:"teacher"`
	Quality   ShadowQualityConfig   `json:"quality"   toml:"quality"`
	Examples  ShadowExamplesConfig  `json:"examples"  toml:"examples"`
	Export    ShadowExportConfig    `json:"export"    toml:"export"`
	Adapters  ShadowAdaptersConfig  `json:"adapters"  toml:"adapters"`
}

// ShadowShadowingConfig controls when and how responses are shadowed.
type ShadowShadowingConfig struct {
	Mode          string   `json:"mode"           toml:"mode"`
	MinComplexity string   `json:"min_complexity" toml:"min_complexity"`
	Domains       []string `json:"domains"        toml:"domains"`
	TaskTypes     []string `json:"task_types"     toml:"task_types"`
	SampleRate    float64  `json:"sample_rate"    toml:"sample_rate"`
	QueueSize     int      `json:"queue_size"     toml:"queue_size"`
	WorkerCount   int      `json:"worker_count"   toml:"worker_count"`
}

// ShadowTeacherConfig configures the teacher model.
type ShadowTeacherConfig struct {
	Model             string  `json:"model"               toml:"model"`
	FallbackModel     string  `json:"fallback_model"      toml:"fallback_model"`
	Temperature       float64 `json:"temperature"         toml:"temperature"`
	MaxTokens         int     `json:"max_tokens"          toml:"max_tokens"`
	TimeoutSeconds    int     `json:"timeout_seconds"     toml:"timeout_seconds"`
	MaxDailyQueries   int     `json:"max_daily_queries"   toml:"max_daily_queries"`
	MaxDailyCost      float64 `json:"max_daily_cost"      toml:"max_daily_cost"`
	RequestsPerMinute int     `json:"requests_per_minute" toml:"requests_per_minute"`
}

// ShadowQualityConfig configures quality scoring.
type ShadowQualityConfig struct {
	Method               string                       `json:"method"                 toml:"method"`
	HighQualityThreshold float64                      `json:"high_quality_threshold" toml:"high_quality_threshold"`
	TrainableThreshold   float64                      `json:"trainable_threshold"    toml:"trainable_threshold"`
	PreferenceMargin     float64                      `json:"preference_margin"      toml:"preference_margin"`
	HeuristicWeights     ShadowHeuristicWeightsConfig `json:"heuristic_weights"      toml:"heuristic_weights"`
	EvalPromptTemplate   string                       `json:"eval_prompt_template"   toml:"eval_prompt_template"`
}

// ShadowHeuristicWeightsConfig defines scoring dimension weights.
type ShadowHeuristicWeightsConfig struct {
	Relevance    float64 `json:"relevance"    toml:"relevance"`
	Completeness float64 `json:"completeness" toml:"completeness"`
	Correctness  float64 `json:"correctness"  toml:"correctness"`
	Style        float64 `json:"style"        toml:"style"`
}

// ShadowExamplesConfig configures few-shot example management.
type ShadowExamplesConfig struct {
	Enabled          bool    `json:"enabled"            toml:"enabled"`
	MaxPerCategory   int     `json:"max_per_category"   toml:"max_per_category"`
	MinQuality       float64 `json:"min_quality"        toml:"min_quality"`
	DefaultCount     int     `json:"default_count"      toml:"default_count"`
	MaxCount         int     `json:"max_count"          toml:"max_count"`
	SimilarityWeight float64 `json:"similarity_weight"  toml:"similarity_weight"`
	RecencyWeight    float64 `json:"recency_weight"     toml:"recency_weight"`
	QualityWeight    float64 `json:"quality_weight"     toml:"quality_weight"`
	MaxContextTokens int     `json:"max_context_tokens" toml:"max_context_tokens"`
}

// ShadowExportConfig configures training data export.
type ShadowExportConfig struct {
	OutputDir                string   `json:"output_dir"                 toml:"output_dir"`
	Formats                  []string `json:"formats"                    toml:"formats"`
	MinRecords               int      `json:"min_records"                toml:"min_records"`
	IncludeLowQuality        bool     `json:"include_low_quality"        toml:"include_low_quality"`
	Deduplicate              bool     `json:"deduplicate"                toml:"deduplicate"`
	DedupSimilarityThreshold float64  `json:"dedup_similarity_threshold" toml:"dedup_similarity_threshold"`
}

// ShadowAdaptersConfig configures adapter management.
type ShadowAdaptersConfig struct {
	Enabled        bool             `json:"enabled"         toml:"enabled"`
	OllamaEndpoint string           `json:"ollama_endpoint" toml:"ollama_endpoint"`
	AutoTrain      bool             `json:"auto_train"      toml:"auto_train"`
	TrainThreshold int              `json:"train_threshold" toml:"train_threshold"`
	TrainSchedule  string           `json:"train_schedule"  toml:"train_schedule"`
	AdapterDir     string           `json:"adapter_dir"     toml:"adapter_dir"`
	LoRA           ShadowLoRAConfig `json:"lora"            toml:"lora"`
	DPO            ShadowDPOConfig  `json:"dpo"             toml:"dpo"`
}

// ShadowLoRAConfig configures LoRA training parameters.
type ShadowLoRAConfig struct {
	Rank                 int      `json:"rank"                  toml:"rank"`
	Alpha                int      `json:"alpha"                 toml:"alpha"`
	Dropout              float64  `json:"dropout"               toml:"dropout"`
	TargetModules        []string `json:"target_modules"        toml:"target_modules"`
	LearningRate         float64  `json:"learning_rate"         toml:"learning_rate"`
	Epochs               int      `json:"epochs"                toml:"epochs"`
	BatchSize            int      `json:"batch_size"            toml:"batch_size"`
	GradientAccumulation int      `json:"gradient_accumulation" toml:"gradient_accumulation"`
	WarmupRatio          float64  `json:"warmup_ratio"          toml:"warmup_ratio"`
	MaxGradNorm          float64  `json:"max_grad_norm"         toml:"max_grad_norm"`
}

// ShadowDPOConfig configures DPO training parameters.
type ShadowDPOConfig struct {
	Beta     float64 `json:"beta"      toml:"beta"`
	LossType string  `json:"loss_type" toml:"loss_type"`
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
			LogLevel:   LogLevelInfo,
			DataDir:    "~/.meept",
		},
		Transport: TransportConfig{
			RPC: RPCTransportConfig{
				Enabled:    true,
				SocketPath: "~/.meept/meept.sock",
			},
			HTTP: HTTPTransportConfig{
				Enabled:     false,
				Addr:        ":8081",
				RequireAuth: true,
				TLSCertFile: "~/.meept/tls/cert.pem",
				TLSKeyFile:  "~/.meept/tls/key.pem",
				REST:        true,  // REST API enabled by default when HTTP is enabled
				WebSocket:   false, // WebSocket disabled by default
				WSPath:      "/ws",
				MCP:         false, // MCP disabled by default
				MCPPath:     "/mcp",
			},
		},
		LLM: LLMConfig{
			Budget: BudgetConfig{
				HourlyTokenLimit:     100000,
				DailyTokenLimit:      1000000,
				RateLimitRPM:         30,
				Aggressiveness:       0.5,
				PerTaskTokenLimit:    50000,  // 50% of hourly budget max per task
				PerSessionTokenLimit: 100000, // 100% of hourly budget max per session
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
				WrapUpThreshold:            0.50,
				HardLimit:                  0.80,
				DropContextOnHardLimit:     true,
				ProactiveCompression:       false,
				MaxSummaryLevel:            3,
				SummaryLevelThreshold:      500,
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
				Domains: []string{"general", DomainCode, "commands"},
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
			DispatcherID: AgentIDDispatcher,
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
				RequireReview: []string{HintCode, HintRefactor, HintDebug, HintGit},
				SkipReview:    []string{"chat", "report", "recall", "search"},
				ReviewerMapping: map[string]string{
					AgentIDCoder:     "code-reviewer",
					AgentIDDebugger:  "debug-reviewer",
					AgentIDPlanner:   "planner-reviewer",
					AgentIDAnalyst:   "analyst-reviewer",
					AgentIDCommitter: "code-reviewer",
				},
				MaxRevisionCycles:   3,
				AutoApprovePatterns: []string{"*.md", "LICENSE"},
			},
			Validation: ValidationConfig{
				Enabled:              true,
				RequireValidation:    []string{HintCode, HintRefactor, HintDebug, HintGit, HintFix, HintCommit},
				SkipValidation:       []string{"chat", "report", "recall", "search", "analyze", "platform"},
				SkipValidationAgents: []string{AgentIDChat, AgentIDAnalyst},
				MaxValidationLoops:   3,
			},
			Watchdog: WatchdogConfig{
				Enabled:              true,
				TimeoutMinutes:       10,
				HeartbeatIntervalSec: 30,
				MaxIterations:        50,
				StuckIterationCount:  5,
			},
			Queues: AgentQueuesConfig{
				Enabled:         true,
				SteeringDrain:   "one",
				FollowUpDrain:   "one",
				MaxSteering:     1,
				MaxFollowUp:     20,
				PersistFollowUp: true,
				FlushDelayMs:    5000,
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
			DefaultCaps:        []string{CapCode, CapReasoning},
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
			CacheSize:   50,
		},
		SelfImprove: SelfImproveConfig{
			Enabled:               false,
			DataDir:               "~/.meept/selfimprove",
			MaxIterationsPerCycle: 5,
			MaxFixesPerCycle:      10,
			AutoRunIntervalHours:  0,
			//nolint:gosec // field name, not a secret
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
			MaxPlanSteps:        10,
			MaxResearchSteps:    3,
			PlannerTimeout:      120,
			TokenBudgetAlert:    5000,
			MaxHandoffSteps:     5,
			HandoffUseAmendment: true,
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
				FormatOnWrite:            false,
				DiagnosticsOnWrite:       false,
				DiagnosticsTimeout:       5,
			},
		},
		Tooling: ToolingConfig{
			Enabled:             false,
			Mode:                "service",
			AgentID:             "tooling",
			Model:               "",
			CacheEnabled:        true,
			CacheMaxSize:        500,
			CacheTTLMinutes:     60,
			IncludeSchema:       true,
			ValidateOnSerialize: false,
			LogUnknownTools:     true,
		},
		Calendar: CalendarConfig{
			Enabled:                false,
			CalendarID:             "primary",
			RedirectURI:            "http://localhost:8888/callback",
			ReminderEnabled:        false,
			ReminderCheckInterval:  "5m",
			ReminderAdvanceMinutes: 10,
		},
		Compaction: CompactionConfig{
			Enabled:           false,
			ReserveTokens:     16384,
			KeepRecentTokens:  20000,
			MaxResponseTokens: 13107,
			SummaryFormat:     "structured",
			Strategy:          "structured",
			TriggerRatio:      0.60,
			IterativeUpdates:  true,
			TrackFileOps:      true,
			TimeoutSeconds:    30,
		},
		Session: SessionConfig{
			Persistence:            true,
			Branching:              true,
			MaxBranches:            20,
			BranchSummaryThreshold: 5,
			RestoreMessageLimit:    0,
			Compaction:             true,
			CompactionThreshold:    50,
			CompactionTargetRatio:  0.6,
			AutoFork:               "ask",
			LegacyTruncation:       false,
		},
		Projects: ProjectsConfig{
			Enabled:                    true,
			BaseDir:                    "~/.meept/projects",
			AutoDetect:                 true,
			WorktreePerPlan:            "auto",
			WorktreeIsolationThreshold: 5,
			MaxWorktreesPerProject:     10,
			CleanupOrphanedWorktrees:   true,
			FenceEnabled:               true,
			AllowReadSystemPaths:       []string{"/usr", "/etc", "/tmp"},
			AutoSyncOnAttach:           false,
			DefaultBranch:              "main",
		},
		Plans: PlansConfig{
			Mode: "threshold",
			Threshold: PlansThresholdConfig{
				MinSteps: 3,
				ComplexityKeywords: []string{
					"refactor", "migrate", "implement", "redesign",
					"rewrite", "integrate", "architect",
				},
				AlwaysPlanIntents: []string{"plan", "implement", "build"},
			},
			Storage: PlansStorageConfig{
				DefaultPath:      "docs/plans",
				FilenameTemplate: "{{slug}}.md",
			},
			Approval: PlansApprovalConfig{
				RequireApproval: true,
				AllowRevision:   true,
				MaxRevisions:    3,
			},
			Confirmation: PlansConfirmationConfig{
				RequireSignoff: true,
			},
		},
		Cluster: DefaultClusterConfig(),
	}
}

// Tool hint constants used in review and validation policy defaults.
const (
	HintCode     = "code"
	HintRefactor = "refactor"
	HintDebug    = "debug"
	HintGit      = "git"
	HintFix      = "fix"
	HintCommit   = "commit"
)

// Capability and domain constants used in default configurations.
const (
	CapCode      = "code"
	CapReasoning = "reasoning"
	DomainCode   = "code"
)

// Log level constants used for configuration.
const (
	LogLevelInfo  = "INFO"
	LogLevelDebug = "DEBUG"
	LogLevelWarn  = "WARN"
	LogLevelError = "ERROR"
)

// ParseLogLevel converts a string log level to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch level {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn, "WARNING":
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ShutdownTimeout returns the default shutdown timeout.
func (c *Config) ShutdownTimeout() time.Duration {
	return 10 * time.Second
}
