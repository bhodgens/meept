// Package config provides configuration loading and validation for meept.
package config

import (
	"log/slog"
	"time"
)

// Config is the root configuration structure loaded from meept.toml.
type Config struct {
	Daemon      DaemonConfig      `toml:"daemon"`
	LLM         LLMConfig         `toml:"llm"`
	Memory      MemoryConfig      `toml:"memory"`
	Security    SecurityConfig    `toml:"security"`
	Scheduler   SchedulerConfig   `toml:"scheduler"`
	Queue       QueueConfig       `toml:"queue"`
	Workers     WorkersConfig     `toml:"workers"`
	Isolation   IsolationConfig   `toml:"isolation"`
	Telegram    TelegramConfig    `toml:"telegram"`
	Web         WebConfig         `toml:"web"`
	MCP         MCPConfig         `toml:"mcp"`
	Plugins     PluginsConfig     `toml:"plugins"`
	Workspace   WorkspaceConfig   `toml:"workspace"`
	Skills      SkillsConfig      `toml:"skills"`
	ClawSkills  ClawSkillsConfig  `toml:"clawskills"`
	SelfImprove SelfImproveConfig `toml:"selfimprove"`
}

// DaemonConfig holds daemon-specific settings.
type DaemonConfig struct {
	SocketPath string `toml:"socket_path"`
	PIDFile    string `toml:"pid_file"`
	LogLevel   string `toml:"log_level"`
	DataDir    string `toml:"data_dir"`
}

// LLMConfig holds LLM configuration including budget.
type LLMConfig struct {
	Budget BudgetConfig `toml:"budget"`
}

// BudgetConfig holds token budget settings.
type BudgetConfig struct {
	HourlyTokenLimit int     `toml:"hourly_token_limit"`
	DailyTokenLimit  int     `toml:"daily_token_limit"`
	RateLimitRPM     int     `toml:"rate_limit_rpm"`
	Aggressiveness   float64 `toml:"aggressiveness"`
}

// MemoryConfig holds memory subsystem settings.
type MemoryConfig struct {
	DataDir                    string              `toml:"data_dir"`
	ConsolidationIntervalHours int                 `toml:"consolidation_interval_hours"`
	Episodic                   EpisodicConfig      `toml:"episodic"`
	Task                       TaskMemoryConfig    `toml:"task"`
	Personality                PersonalityConfig   `toml:"personality"`
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
	Enabled                    bool `toml:"enabled"`
	UpdateIntervalConversations int  `toml:"update_interval_conversations"`
}

// SecurityConfig holds security settings.
type SecurityConfig struct {
	SanitizeInputs             bool     `toml:"sanitize_inputs"`
	LLMFilterExternal          bool     `toml:"llm_filter_external"`
	RequireConfirmationHigh    bool     `toml:"require_confirmation_high"`
	RequireConfirmationCritical bool    `toml:"require_confirmation_critical"`
	BlockFinancial             bool     `toml:"block_financial"`
	AllowedPaths               []string `toml:"allowed_paths"`
	BlockedPaths               []string `toml:"blocked_paths"`
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
	Enabled   bool   `toml:"enabled"`
	Token     string `toml:"token"`
	CreatorID int64  `toml:"creator_id"`
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
	Enabled bool `toml:"enabled"`
}

// ClawSkillsConfig holds ClawSkills settings.
type ClawSkillsConfig struct {
	Enabled          bool     `toml:"enabled"`
	RegistryURL      string   `toml:"registry_url"`
	InstallDir       string   `toml:"install_dir"`
	AutoUpdate       bool     `toml:"auto_update"`
	MaxInstalled     int      `toml:"max_installed"`
	DefaultRiskLevel string   `toml:"default_risk_level"`
	MaxIterations    int      `toml:"max_iterations"`
	BlockedSlugs     []string `toml:"blocked_slugs"`
}

// SelfImproveConfig holds self-improvement settings.
type SelfImproveConfig struct {
	Enabled              bool                  `toml:"enabled"`
	DataDir              string                `toml:"data_dir"`
	MaxIterationsPerCycle int                  `toml:"max_iterations_per_cycle"`
	MaxFixesPerCycle     int                   `toml:"max_fixes_per_cycle"`
	AutoRunIntervalHours int                   `toml:"auto_run_interval_hours"`
	AIInfra              AIInfraConfig         `toml:"ai_infra"`
	Sandbox              SandboxConfig         `toml:"sandbox"`
	Safety               SafetyConfig          `toml:"safety"`
	Detection            DetectionConfig       `toml:"detection"`
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
	RequireHumanApproval     bool     `toml:"require_human_approval"`
	MaxFilesPerFix           int      `toml:"max_files_per_fix"`
	MaxLinesChangedPerFix    int      `toml:"max_lines_changed_per_fix"`
	BlockedPaths             []string `toml:"blocked_paths"`
	AllowedRiskLevels        []string `toml:"allowed_risk_levels"`
	BlockCriticalRisk        bool     `toml:"block_critical_risk"`
	RequireTestsPass         bool     `toml:"require_tests_pass"`
	MinConfidenceThreshold   float64  `toml:"min_confidence_threshold"`
}

// DetectionConfig holds detection settings for self-improvement.
type DetectionConfig struct {
	ScanPytest        bool     `toml:"scan_pytest"`
	ScanRuntimeLogs   bool     `toml:"scan_runtime_logs"`
	ScanTypeCheck     bool     `toml:"scan_type_check"`
	ScanLint          bool     `toml:"scan_lint"`
	LogFile           string   `toml:"log_file"`
	LogLookbackHours  int      `toml:"log_lookback_hours"`
	PytestArgs        []string `toml:"pytest_args"`
	MypyArgs          []string `toml:"mypy_args"`
	RuffArgs          []string `toml:"ruff_args"`
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
		},
		Memory: MemoryConfig{
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
				Enabled:                    true,
				UpdateIntervalConversations: 10,
			},
		},
		Security: SecurityConfig{
			SanitizeInputs:              true,
			LLMFilterExternal:           false,
			RequireConfirmationHigh:     true,
			RequireConfirmationCritical: true,
			BlockFinancial:              true,
			AllowedPaths:                []string{"~/*"},
			BlockedPaths:                []string{"~/.ssh/*", "~/.gnupg/*", "~/.meept/meept.toml"},
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
			Enabled:   false,
			Token:     "",
			CreatorID: 0,
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
			Enabled: false,
		},
		ClawSkills: ClawSkillsConfig{
			Enabled:          false,
			RegistryURL:      "https://clawhub.ai",
			InstallDir:       "~/.meept/clawskills",
			AutoUpdate:       false,
			MaxInstalled:     50,
			DefaultRiskLevel: "high",
			MaxIterations:    10,
			BlockedSlugs:     []string{},
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
