package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LocalBackend executes commands on the local system using exec.Command.
type LocalBackend struct {
	defaultEnv map[string]string
}

// NewLocalBackend creates a new local execution backend.
func NewLocalBackend() *LocalBackend {
	return &LocalBackend{
		defaultEnv: make(map[string]string),
	}
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
	command.Env = buildEnv(cmd.Env)

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

// buildEnv combines current environment with command-specific variables.
func buildEnv(cmdEnv map[string]string) []string {
	// Start with current environment
	env := os.Environ()

	// Add/override with command-specific
	for k, v := range cmdEnv {
		set := false
		for i, existing := range env {
			if strings.HasPrefix(existing, k+"=") {
				env[i] = fmt.Sprintf("%s=%s", k, v)
				set = true
				break
			}
		}
		if !set {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return env
}
