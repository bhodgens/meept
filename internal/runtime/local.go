package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrTimeout is returned when a command exceeds its configured timeout.
var ErrTimeout = errors.New("command timed out")

// ErrCanceled is returned when a command is canceled via context cancellation.
var ErrCanceled = errors.New("command canceled")

// LocalBackend executes commands on the local system using exec.Command.
type LocalBackend struct {
	defaultEnv map[string]string
	// envPolicy is the resolved child-environment policy applied to every
	// command this backend runs.
	envPolicy EnvPolicyConfig
}

// NewLocalBackend creates a new local execution backend. It captures
// os.Environ() exactly ONCE at construction: child environments are filtered
// from this snapshot for consistency across the backend's lifetime (a later
// change to the daemon's environ does not retroactively leak into children)
// and for deterministic test injection. parentEnv may be nil, which falls
// back to capturing os.Environ() here. The policy defaults to secure
// allowlist mode when cfg.EnvPolicy.Mode is empty; pass SetEnvPolicy to
// replace it after construction.
func NewLocalBackend(cfg Config, parentEnv []string) *LocalBackend {
	if parentEnv == nil {
		parentEnv = os.Environ()
	}
	policy := cfg.EnvPolicy
	if policy.Mode == "" {
		policy.Mode = EnvModeAllowlist
	}
	defaultEnv := make(map[string]string, len(parentEnv))
	for _, entry := range parentEnv {
		if eq := strings.IndexByte(entry, '='); eq > 0 {
			defaultEnv[entry[:eq]] = entry[eq+1:]
		}
	}
	return &LocalBackend{
		defaultEnv: defaultEnv,
		envPolicy:  policy,
	}
}

// SetEnvPolicy replaces the child environment policy. It takes the policy by
// VALUE, so no typed-nil deref is possible.
func (b *LocalBackend) SetEnvPolicy(policy EnvPolicyConfig) {
	b.envPolicy = policy
}

// Name returns the backend identifier.
func (b *LocalBackend) Name() string {
	return "local"
}

// Execute runs a command locally and returns the result.
func (b *LocalBackend) Execute(ctx context.Context, cmd Command) (*CommandResult, error) {
	execCtx := ctx
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}

	command := exec.CommandContext(execCtx, "/bin/sh", "-c", cmd.Cmd)

	// Set working directory
	if cmd.Dir != "" {
		command.Dir = cmd.Dir
	}

	// Set environment variables
	command.Env = b.buildEnv(cmd.Env)

	start := time.Now()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	duration := time.Since(start)

	var exitCode int
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
		// Don't return error for non-zero exit codes - caller handles them
		err = nil
	} else if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %s", ErrTimeout, cmd.Cmd)
		}
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("%w: %s", ErrCanceled, cmd.Cmd)
		}
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}

	return &CommandResult{
		Output:   output,
		ExitCode: exitCode,
		Duration: duration,
	}, err
}

// Close cleans up resources (no-op for local backend).
func (b *LocalBackend) Close() error {
	return nil
}

// buildEnv builds the child environment for a command by delegating to
// BuildChildEnv over the environ snapshot captured at backend construction
// and the backend's resolved EnvPolicy.
func (b *LocalBackend) buildEnv(cmdEnv map[string]string) []string {
	// Reconstruct the parent environ in insertion order is unnecessary:
	// BuildChildEnv only needs name/value pairs, so the map captured at
	// construction is converted back to entries here. Deterministic output
	// order comes from BuildChildEnv itself (cmdEnv-only vars sorted).
	parent := make([]string, 0, len(b.defaultEnv))
	for k, v := range b.defaultEnv {
		parent = append(parent, k+"="+v)
	}
	env, _ := BuildChildEnv(b.envPolicy, parent, cmdEnv)
	return env
}
