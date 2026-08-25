package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// BwrapConfig configures the external-binary bubblewrap backend.
type BwrapConfig struct {
	BinaryPath string   `json:"binary_path"` // default "bwrap"
	ExtraArgs  []string `json:"extra_args"`
	TmpfsDirs  []string `json:"tmpfs_dirs"` // default ["/tmp"]
}

// bwrapRunner executes a fully-assembled argv. It returns combined output,
// the child's exit code, and an error ONLY for start failures or timeouts
// (non-zero command exit is a result, not an error — mirrors LocalBackend).
type bwrapRunner func(ctx context.Context, path string, args []string, dir string, env []string) (out []byte, exitCode int, err error)

// realBwrapRunner runs the process with combined output capture.
func realBwrapRunner(ctx context.Context, path string, args []string, dir string, env []string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Non-zero command exit is reported as a RESULT for callers to act on.
		return out, exitErr.ExitCode(), nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return out, -1, fmt.Errorf("%w: %s", ErrTimeout, path)
	}
	return out, -1, fmt.Errorf("bwrap: failed to execute %s: %w", path, err)
}

// BwrapBackend jails commands via the EXTERNAL bubblewrap binary (user
// namespaces). The binary is exec'd as-is — no bubblewrap source is vendored.
type BwrapBackend struct {
	cfg       BwrapConfig
	envPolicy EnvPolicyConfig
	parentEnv []string // captured once at construction, like LocalBackend
	logger    *slog.Logger
	run       bwrapRunner
}

// NewBwrapBackend creates a new bwrap-backed ExecutionBackend. It errors on
// non-Linux platforms or when the configured binary cannot be located on
// PATH.
func NewBwrapBackend(cfg BwrapConfig, envPolicy EnvPolicyConfig, logger *slog.Logger) (*BwrapBackend, error) {
	return newBwrapBackendWithProbes(cfg, envPolicy, logger, runtime.GOOS, exec.LookPath, realBwrapRunner)
}

// newBwrapBackendWithProbes is the injectable constructor used by tests.
func newBwrapBackendWithProbes(
	cfg BwrapConfig,
	envPolicy EnvPolicyConfig,
	logger *slog.Logger,
	goos string,
	lookPath func(string) (string, error),
	run bwrapRunner,
) (*BwrapBackend, error) {
	if goos != "linux" {
		return nil, fmt.Errorf("bwrap backend unsupported on %s", goos)
	}

	path := cfg.BinaryPath
	if path == "" {
		path = "bwrap"
	}
	if _, err := lookPath(path); err != nil {
		return nil, fmt.Errorf("bwrap binary %q not found on PATH: %w", path, err)
	}

	policy := envPolicy
	if policy.Mode == "" {
		policy.Mode = EnvModeAllowlist
	}

	if len(cfg.TmpfsDirs) == 0 {
		cfg.TmpfsDirs = []string{"/tmp"}
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &BwrapBackend{
		cfg:       cfg,
		envPolicy: policy,
		parentEnv: os.Environ(), // captured exactly once
		logger:    logger,
		run:       run,
	}, nil
}

// Name returns the backend identifier.
func (b *BwrapBackend) Name() string { return "bwrap" }

// SetEnvPolicy replaces the child environment policy. Takes the policy by
// VALUE, so no typed-nil deref is possible.
func (b *BwrapBackend) SetEnvPolicy(policy EnvPolicyConfig) {
	b.envPolicy = policy
}

// Close releases resources held by the backend. bwrap processes are
// short-lived per Execute call, so nothing persistent needs stopping.
func (b *BwrapBackend) Close() error { return nil }

// Execute runs cmd inside a bubblewrap jail built from a strict read-only
// base bind set plus a writable working directory.
//
// Argv layout (all elements passed as discrete array entries — never string-
// concatenated — so shell metacharacters in cmd.Cmd cannot escape the argv
// boundary):
//
//	bwrap --ro-bind <dir> <dir> ...   for each of /usr,/bin,/lib,/lib64 present
//	      --bind <workdir> <workdir>  writable workspace
//	      --dev /dev --proc /proc
//	      [--tmpfs <dir>]             per cfg.TmpfsDirs (default /tmp)
//	      <cfg.ExtraArgs...>
//	      -- sh -c <cmd.Cmd>
//
// Environment is filtered through BuildChildEnv(envPolicy, parentSnapshot,
// cmd.Env).
func (b *BwrapBackend) Execute(ctx context.Context, cmd Command) (*CommandResult, error) {
	args := make([]string, 0, 32+len(b.cfg.ExtraArgs))

	for _, dir := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if !dirExists(dir) {
			continue
		}
		args = append(args, "--ro-bind", dir, dir)
	}

	workDir := cmd.Dir
	if workDir == "" {
		// No explicit workdir: fall back to the daemon CWD so the child has
		// SOME writable location; bwrap refuses to start without one.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("bwrap: resolve default workdir: %w", err)
		}
		workDir = cwd
	}
	if !dirExists(workDir) {
		return nil, fmt.Errorf("bwrap: working directory does not exist: %s", workDir)
	}
	args = append(args, "--bind", workDir, workDir)

	args = append(args, "--dev", "/dev")
	args = append(args, "--proc", "/proc")

	for _, dir := range b.cfg.TmpfsDirs {
		args = append(args, "--tmpfs", dir)
	}

	args = append(args, b.cfg.ExtraArgs...)

	// Terminate option parsing; everything after "--" is the jailed command.
	args = append(args, "--", "sh", "-c", cmd.Cmd)

	childEnv, _ := BuildChildEnv(b.envPolicy, b.parentEnv, cmd.Env)

	path := b.cfg.BinaryPath
	if path == "" {
		path = "bwrap"
	}

	start := time.Now()
	out, exitCode, runErr := b.run(ctx, path, args, workDir, childEnv)
	duration := time.Since(start)

	// A start failure must never look like a successful execution.
	if runErr != nil && exitCode == 0 {
		exitCode = -1
	}

	return &CommandResult{
		Output:   string(out),
		ExitCode: exitCode,
		Duration: duration,
	}, runErr
}

// dirExists reports whether path exists as any filesystem entry.
func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
