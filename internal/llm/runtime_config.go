package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/pathutil"
)

// RuntimeLifecycleConfig holds configuration for local LLM runtime management.
type RuntimeLifecycleConfig struct {
	Runtime        string              `json:"runtime"`           // "llama-cpp" or "mlx"
	ModelPath      string              `json:"model_path"`        // Path to model file
	AutoStart      bool                `json:"auto_start"`        // Auto-start on daemon startup
	AutoStopOnExit bool                `json:"auto_stop_on_exit"` // Stop on daemon shutdown
	PIDFile        string              `json:"pid_file"`          // Path to PID file
	SpawnCommand   []string            `json:"spawn_command"`     // Command and args to spawn runtime
	SpawnTimeout   int                 `json:"spawn_timeout_seconds"`
	HealthCheck    HealthCheckConfig   `json:"health_check"`
	RestartPolicy  RestartPolicyConfig `json:"restart_policy"`
}

// HealthCheckConfig holds health check configuration.
type HealthCheckConfig struct {
	Endpoint           string `json:"endpoint"`
	IntervalSeconds    int    `json:"interval_seconds"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	UnhealthyThreshold int    `json:"unhealthy_threshold"`
}

// RestartPolicyConfig holds restart policy configuration.
type RestartPolicyConfig struct {
	Enabled           bool `json:"enabled"`             // Enable auto-restart on unhealthy
	MaxAttempts       int  `json:"max_attempts"`        // Max restart attempts (default: 3)
	CooldownSeconds   int  `json:"cooldown_seconds"`    // Seconds between restart attempts (default: 30)
	ResetAfterSeconds int  `json:"reset_after_seconds"` // Reset failure count after this many seconds of healthy (default: 300)
}

// RuntimeType represents a supported LLM runtime.
type RuntimeType string

const (
	RuntimeLlamaCpp RuntimeType = "llama-cpp"
	RuntimeMLX      RuntimeType = "mlx"
)

// RuntimeConfig holds validated runtime configuration.
type RuntimeConfig struct {
	Type               RuntimeType
	ModelPath          string
	PIDFile            string
	AutoStart          bool
	AutoStop           bool
	SpawnCommand       []string
	SpawnTimeout       time.Duration
	HealthEndpoint     string
	HealthInterval     time.Duration
	HealthTimeout      time.Duration
	HealthThreshold    int
	RestartEnabled     bool
	RestartMaxAttempts int
	RestartCooldown    time.Duration
	RestartResetAfter  time.Duration
}

// ValidateAndNormalize validates the config and expands paths.
func ValidateAndNormalize(cfg RuntimeLifecycleConfig) (*RuntimeConfig, error) {
	// Validate runtime type
	var rt RuntimeType
	switch cfg.Runtime {
	case "llama-cpp":
		rt = RuntimeLlamaCpp
	case "mlx":
		rt = RuntimeMLX
	default:
		return nil, fmt.Errorf("unsupported runtime: %s", cfg.Runtime)
	}

	// Expand model path
	modelPath := pathutil.ExpandPath(cfg.ModelPath)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model file not found: %s: %w", modelPath, err)
	}

	// Expand PID file path
	pidFile := pathutil.ExpandPath(cfg.PIDFile)
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create PID directory: %w", err)
	}

	// Build spawn command with variable expansion
	spawnCmd := make([]string, len(cfg.SpawnCommand))
	for i, part := range cfg.SpawnCommand {
		spawnCmd[i] = os.Expand(part, func(key string) string {
			if key == "MODEL_PATH" {
				return modelPath
			}
			return os.Getenv(key)
		})
	}

	// Set defaults
	spawnTimeout := 60 * time.Second
	if cfg.SpawnTimeout > 0 {
		spawnTimeout = time.Duration(cfg.SpawnTimeout) * time.Second
	}

	healthInterval := 10 * time.Second
	if cfg.HealthCheck.IntervalSeconds > 0 {
		healthInterval = time.Duration(cfg.HealthCheck.IntervalSeconds) * time.Second
	}

	healthTimeout := 5 * time.Second
	if cfg.HealthCheck.TimeoutSeconds > 0 {
		healthTimeout = time.Duration(cfg.HealthCheck.TimeoutSeconds) * time.Second
	}

	healthThreshold := 3
	if cfg.HealthCheck.UnhealthyThreshold > 0 {
		healthThreshold = cfg.HealthCheck.UnhealthyThreshold
	}

	restartMaxAttempts := 3
	if cfg.RestartPolicy.MaxAttempts > 0 {
		restartMaxAttempts = cfg.RestartPolicy.MaxAttempts
	}
	restartCooldown := 30 * time.Second
	if cfg.RestartPolicy.CooldownSeconds > 0 {
		restartCooldown = time.Duration(cfg.RestartPolicy.CooldownSeconds) * time.Second
	}
	restartResetAfter := 300 * time.Second
	if cfg.RestartPolicy.ResetAfterSeconds > 0 {
		restartResetAfter = time.Duration(cfg.RestartPolicy.ResetAfterSeconds) * time.Second
	}

	return &RuntimeConfig{
		Type:               rt,
		ModelPath:          modelPath,
		PIDFile:            pidFile,
		AutoStart:          cfg.AutoStart,
		AutoStop:           cfg.AutoStopOnExit,
		SpawnCommand:       spawnCmd,
		SpawnTimeout:       spawnTimeout,
		HealthEndpoint:     cfg.HealthCheck.Endpoint,
		HealthInterval:     healthInterval,
		HealthTimeout:      healthTimeout,
		HealthThreshold:    healthThreshold,
		RestartEnabled:     cfg.RestartPolicy.Enabled,
		RestartMaxAttempts: restartMaxAttempts,
		RestartCooldown:    restartCooldown,
		RestartResetAfter:  restartResetAfter,
	}, nil
}

// SupportedRuntimes returns the list of supported runtime types.
func SupportedRuntimes() []string {
	return []string{string(RuntimeLlamaCpp), string(RuntimeMLX)}
}

// IsSupportedRuntime checks if the given runtime string is supported.
func IsSupportedRuntime(rt string) bool {
	return rt == "llama-cpp" || rt == "mlx"
}

// HasLifecycle returns true if the provider config has lifecycle management enabled.
func (p ProviderConfig) HasLifecycle() bool {
	return p.Lifecycle != nil && p.Lifecycle.AutoStart
}

// ExpandModelPath expands tilde in the model path and returns the cleaned path.
// Returns an empty string if no lifecycle config exists.
func (p ProviderConfig) ExpandModelPath() string {
	if p.Lifecycle == nil {
		return ""
	}
	return pathutil.ExpandPath(p.Lifecycle.ModelPath)
}

// ValidateModelExists checks if the model file exists for this provider's lifecycle config.
func (p ProviderConfig) ValidateModelExists() error {
	if p.Lifecycle == nil {
		return nil
	}
	modelPath := pathutil.ExpandPath(p.Lifecycle.ModelPath)
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("model file not found: %s: %w", modelPath, err)
	}
	return nil
}

// ExpandSpawnCommand expands $MODEL_PATH and environment variables in spawn command.
func (p ProviderConfig) ExpandSpawnCommand() ([]string, error) {
	if p.Lifecycle == nil {
		return nil, nil
	}

	modelPath := pathutil.ExpandPath(p.Lifecycle.ModelPath)
	cmds := make([]string, len(p.Lifecycle.SpawnCommand))
	for i, part := range p.Lifecycle.SpawnCommand {
		cmds[i] = os.Expand(part, func(key string) string {
			if key == "MODEL_PATH" {
				return modelPath
			}
			return os.Getenv(key)
		})
	}
	return cmds, nil
}

// PIDDir returns the directory of the PID file, expanding tildes.
func (p ProviderConfig) PIDDir() string {
	if p.Lifecycle == nil || p.Lifecycle.PIDFile == "" {
		return ""
	}
	return filepath.Dir(pathutil.ExpandPath(p.Lifecycle.PIDFile))
}

// DefaultSpawnTimeout returns the spawn timeout, defaulting to 60s.
func (p ProviderConfig) DefaultSpawnTimeout() time.Duration {
	if p.Lifecycle == nil || p.Lifecycle.SpawnTimeout <= 0 {
		return 60 * time.Second
	}
	return time.Duration(p.Lifecycle.SpawnTimeout) * time.Second
}

// HealthCheckInterval returns the health check interval, defaulting to 10s.
func (p ProviderConfig) HealthCheckInterval() time.Duration {
	if p.Lifecycle == nil || p.Lifecycle.HealthCheck.IntervalSeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(p.Lifecycle.HealthCheck.IntervalSeconds) * time.Second
}

// HealthCheckTimeout returns the health check timeout, defaulting to 5s.
func (p ProviderConfig) HealthCheckTimeout() time.Duration {
	if p.Lifecycle == nil || p.Lifecycle.HealthCheck.TimeoutSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(p.Lifecycle.HealthCheck.TimeoutSeconds) * time.Second
}

// HealthCheckEndpoint returns the health check endpoint URL.
func (p ProviderConfig) HealthCheckEndpoint() string {
	if p.Lifecycle == nil {
		return ""
	}
	return p.Lifecycle.HealthCheck.Endpoint
}

// UnhealthyThreshold returns the unhealthy threshold, defaulting to 3.
func (p ProviderConfig) UnhealthyThreshold() int {
	if p.Lifecycle == nil || p.Lifecycle.HealthCheck.UnhealthyThreshold <= 0 {
		return 3
	}
	return p.Lifecycle.HealthCheck.UnhealthyThreshold
}

// EnsurePIDDir creates the PID directory with restricted permissions.
func (p ProviderConfig) EnsurePIDDir() error {
	dir := p.PIDDir()
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

// ValidatePIDPath checks that the PID file path starts with a slash.
func (p ProviderConfig) ValidatePIDPath() error {
	if p.Lifecycle == nil || p.Lifecycle.PIDFile == "" {
		return nil
	}
	if filepath.IsAbs(p.Lifecycle.PIDFile) {
		return nil
	}
	return fmt.Errorf("PID file path must be absolute: %s", p.Lifecycle.PIDFile)
}

// IsAutoStart returns whether the runtime should auto-start.
func (p ProviderConfig) IsAutoStart() bool {
	if p.Lifecycle == nil {
		return false
	}
	return p.Lifecycle.AutoStart
}

// IsAutoStopOnExit returns whether the runtime should auto-stop on daemon shutdown.
func (p ProviderConfig) IsAutoStopOnExit() bool {
	if p.Lifecycle == nil {
		return false
	}
	return p.Lifecycle.AutoStopOnExit
}

// String returns a human-readable representation of the runtime type.
func (rt RuntimeType) String() string {
	return string(rt)
}

// ParseRuntimeType parses a runtime type string.
func ParseRuntimeType(s string) (RuntimeType, error) {
	switch s {
	case "llama-cpp":
		return RuntimeLlamaCpp, nil
	case "mlx":
		return RuntimeMLX, nil
	default:
		return "", fmt.Errorf("unsupported runtime: %s", s)
	}
}

// IsValid checks if the runtime type string is valid.
func (rt RuntimeType) IsValid() bool {
	return rt == RuntimeLlamaCpp || rt == RuntimeMLX
}

// FormatPIDFilePath validates and returns an absolute PID file path.
func FormatPIDFilePath(pidFile string) (string, error) {
	expanded := pathutil.ExpandPath(pidFile)
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("PID file path must be absolute: %s", pidFile)
	}
	// Prevent path traversal
	if filepath.Base(expanded) != filepath.Base(pidFile) {
		return "", fmt.Errorf("disallowed characters in PID file path: %s", pidFile)
	}
	return expanded, nil
}

// ContainsSupportedRuntime checks if any runtime in the list is supported.
func ContainsSupportedRuntime(runtimes []string) bool {
	for _, rt := range runtimes {
		if rt == "llama-cpp" || rt == "mlx" {
			return true
		}
	}
	return false
}
