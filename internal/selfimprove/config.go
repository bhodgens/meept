// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"os"
	"path/filepath"
	"time"
)

// Config holds the configuration for the self-improvement system.
type Config struct {
	// General settings
	Enabled              bool          `json:"enabled"`
	DataPath             string        `json:"data_path"`
	MaxIterationsPerCycle int          `json:"max_iterations_per_cycle"`
	MaxFixesPerCycle     int           `json:"max_fixes_per_cycle"`

	// Detection settings
	Detection DetectionConfig `json:"detection"`

	// AI infrastructure settings
	AIInfra AIInfraConfig `json:"ai_infra"`

	// Sandbox settings
	Sandbox SandboxConfig `json:"sandbox"`

	// Safety settings
	Safety SafetyConfig `json:"safety"`
}

// DetectionConfig holds detection-related settings.
type DetectionConfig struct {
	// Log file patterns to scan
	LogPatterns []string `json:"log_patterns"`
	// Error patterns to detect
	ErrorPatterns []string `json:"error_patterns"`
	// Performance thresholds
	SlowQueryThreshold time.Duration `json:"slow_query_threshold"`
	// Metrics to monitor
	Metrics []string `json:"metrics"`
}

// AIInfraConfig holds AI infrastructure settings.
type AIInfraConfig struct {
	// Model to use for analysis
	AnalysisModel string `json:"analysis_model"`
	// Model to use for fix generation
	GenerationModel string `json:"generation_model"`
	// Max tokens for analysis
	MaxAnalysisTokens int `json:"max_analysis_tokens"`
	// Max tokens for generation
	MaxGenerationTokens int `json:"max_generation_tokens"`
	// Temperature for generation
	Temperature float64 `json:"temperature"`
}

// SandboxConfig holds sandbox settings.
type SandboxConfig struct {
	// Sandbox type: "docker", "process", "none"
	Type string `json:"type"`
	// Timeout for sandbox operations
	Timeout time.Duration `json:"timeout"`
	// Resource limits
	MemoryLimit string `json:"memory_limit"`
	CPULimit    string `json:"cpu_limit"`
	// Working directory template
	WorkDirTemplate string `json:"work_dir_template"`
}

// SafetyConfig holds safety settings.
type SafetyConfig struct {
	// Require human approval for fixes
	RequireHumanApproval bool `json:"require_human_approval"`
	// Auto-apply low-risk fixes
	AutoApplyLowRisk bool `json:"auto_apply_low_risk"`
	// Max consecutive failures before circuit breaker
	MaxConsecutiveFailures int `json:"max_consecutive_failures"`
	// Max failures per issue before skipping
	MaxFailuresPerIssue int `json:"max_failures_per_issue"`
	// File patterns to never modify
	ProtectedPatterns []string `json:"protected_patterns"`
	// Require tests to pass
	RequireTestsPass bool `json:"require_tests_pass"`
	// Require build to succeed
	RequireBuildSuccess bool `json:"require_build_success"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	homeDir, _ := os.UserHomeDir()
	return Config{
		Enabled:               false, // Disabled by default
		DataPath:              filepath.Join(homeDir, ".meept", "selfimprove"),
		MaxIterationsPerCycle: 10,
		MaxFixesPerCycle:      5,
		Detection: DetectionConfig{
			LogPatterns: []string{
				"*.log",
				"logs/*.log",
			},
			ErrorPatterns: []string{
				"ERROR",
				"FATAL",
				"panic:",
				"exception:",
			},
			SlowQueryThreshold: 5 * time.Second,
			Metrics: []string{
				"error_rate",
				"latency_p99",
				"memory_usage",
			},
		},
		AIInfra: AIInfraConfig{
			AnalysisModel:       "claude-sonnet-4-5-20250929",
			GenerationModel:     "claude-sonnet-4-5-20250929",
			MaxAnalysisTokens:   4096,
			MaxGenerationTokens: 8192,
			Temperature:         0.2,
		},
		Sandbox: SandboxConfig{
			Type:            "process",
			Timeout:         5 * time.Minute,
			MemoryLimit:     "512m",
			CPULimit:        "1",
			WorkDirTemplate: filepath.Join(homeDir, ".meept", "selfimprove", "sandbox"),
		},
		Safety: SafetyConfig{
			RequireHumanApproval:   true,
			AutoApplyLowRisk:       false,
			MaxConsecutiveFailures: 5,
			MaxFailuresPerIssue:    3,
			ProtectedPatterns: []string{
				"*.key",
				"*.pem",
				".env*",
				"secrets.*",
				"credentials.*",
			},
			RequireTestsPass:    true,
			RequireBuildSuccess: true,
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.MaxIterationsPerCycle <= 0 {
		c.MaxIterationsPerCycle = 10
	}
	if c.MaxFixesPerCycle <= 0 {
		c.MaxFixesPerCycle = 5
	}
	if c.Safety.MaxConsecutiveFailures <= 0 {
		c.Safety.MaxConsecutiveFailures = 5
	}
	if c.Safety.MaxFailuresPerIssue <= 0 {
		c.Safety.MaxFailuresPerIssue = 3
	}
	return nil
}
