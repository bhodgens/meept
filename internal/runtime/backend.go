// Package runtime provides isolated execution backends for shell commands.
package runtime

import (
	"context"
	"time"
)

// Command represents a command to execute.
type Command struct {
	Cmd         string            // Shell command to run
	Dir         string            // Working directory
	Env         map[string]string // Environment variables
	Timeout     time.Duration     // Execution timeout (0 = no timeout)
	Interactive bool              // PTY mode for interactive tools
}

// CommandResult holds execution results.
type CommandResult struct {
	Output    string
	ExitCode  int
	Duration  time.Duration
	WasCached bool // True if result came from cache
}

// ExecutionBackend defines the interface for command execution.
type ExecutionBackend interface {
	// Execute runs a command and returns the result.
	Execute(ctx context.Context, cmd Command) (*CommandResult, error)

	// Name returns the backend identifier (e.g., "local", "docker").
	Name() string

	// Close cleans up backend resources.
	Close() error
}

// Config holds runtime configuration.
type Config struct {
	// DefaultBackend is the default backend to use ("local" or "docker").
	DefaultBackend string `json:"default_backend"`
	// Docker holds Docker-specific settings.
	Docker DockerConfig `json:"docker"`
	// EnvPolicy controls child environment construction (allowlist vs
	// inherit). Zero value means "unset"; LocalBackend applies the secure
	// allowlist default when Mode is empty.
	EnvPolicy EnvPolicyConfig `json:"env_policy"`
	// Sandbox controls sandboxed-backend selection. Zero Order ("") is
	// treated as auto by ResolveBackend.
	Sandbox ResolverConfig `json:"sandbox"`
	// Bwrap holds bubblewrap backend settings used when the resolver
	// constructs a bwrap backend.
	Bwrap BwrapConfig `json:"bwrap"`
}

// DockerConfig holds Docker backend configuration.
type DockerConfig struct {
	// Image is the container image to use (e.g., "golang:1.24").
	Image string `json:"image"`
	// Workdir is the working directory inside the container.
	Workdir string `json:"workdir"`
	// VolumeBinds maps host paths to container paths.
	VolumeBinds []string `json:"volume_binds"`
	// NetworkMode sets the container network mode.
	NetworkMode string `json:"network_mode"`
	// Timeout is the default command timeout.
	Timeout time.Duration `json:"timeout"`
}
