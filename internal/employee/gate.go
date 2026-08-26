// Quality-gated autonomy (loop-economics leaf 08).
//
// This file implements completion gating for employee goals: an autonomous
// run may only claim success after a user-defined shell check passes, and a
// failed gate is not re-run while the workspace is unchanged.
//
// This is ORTHOGONAL to the enforcement engine (enforcement.go): enforcement
// polices individual actions pre-execution; the quality gate decides whether
// a goal may be marked complete after execution. Do not conflate them.
package employee

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/runtime"
)

// DefaultGateTimeoutSeconds is used when GateConfig.TimeoutSeconds is unset.
const DefaultGateTimeoutSeconds = 300

// GateConfig configures the quality gate for a goal. An empty Command means
// no gate is configured (legacy behaviour: the model's own judgment completes
// the goal).
type GateConfig struct {
	// Command is the shell command run in the employee's workspace. Empty
	// means no gate (legacy).
	Command string `json:"command"`
	// TimeoutSeconds kills the gate command after this many seconds.
	// Zero/negative means DefaultGateTimeoutSeconds (300).
	TimeoutSeconds int `json:"timeout_seconds"`
	// SkipWhenUnchanged skips re-running a previously FAILED gate when the
	// workspace hash is unchanged since the failure. Callers loading config
	// from JSON5/TOML should default this to true explicitly (a plain bool
	// cannot distinguish unset from false in Go).
	SkipWhenUnchanged bool `json:"skip_when_unchanged"`
}

// timeout returns the effective timeout.
func (c GateConfig) timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return DefaultGateTimeoutSeconds * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// GateResult reports the outcome of a gate run.
type GateResult struct {
	// Passed is true iff the gate command exited 0.
	Passed bool
	// Output is the combined command output (may contain code — never log
	// above debug level).
	Output string
	// Skipped is true when the gate was skipped (workspace unchanged since
	// a previous failure and SkipWhenUnchanged is set).
	Skipped bool
	// Reason explains a skip (or other non-run outcome).
	Reason string
}

// GateState carries the cross-round memory needed for skip-on-unchanged.
type GateState struct {
	// WorkspaceHash is the sha256 over `git status --porcelain` output plus
	// the HEAD sha of the workspace at the time of the last gate run.
	WorkspaceHash string
	// LastFailedOutput is the (possibly truncated) output of the last failed
	// gate run. Empty when the last run passed.
	LastFailedOutput string
}

// workspaceHash computes a cheap determinism hash over the workspace's git
// state: sha256(`git status --porcelain` output || HEAD sha). Commands run
// through the provided backend in workdir. If the directory is not a git
// repository the commands still produce deterministic output (git error text),
// which is hashed as-is — the hash remains stable across no-op rounds either
// way.
func workspaceHash(ctx context.Context, backend runtime.ExecutionBackend, workdir string) (string, error) {
	var combined string
	for _, cmd := range []string{"git status --porcelain", "git rev-parse HEAD"} {
		res, err := backend.Execute(ctx, runtime.Command{
			Cmd: cmd,
			Dir: workdir,
		})
		if err != nil {
			return "", fmt.Errorf("gate hash %q: %w", cmd, err)
		}
		combined += res.Output + "\x00"
	}
	sum := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(sum[:]), nil
}

// RunGate executes the configured quality gate in the given workspace.
//
// Behaviour:
//   - Computes the current workspace hash first.
//   - If cfg.SkipWhenUnchanged is set and prev records a failed gate against
//     an identical hash, returns a skipped result WITHOUT re-running the
//     command (prime-agent semantics: don't burn cycles re-proving a known
//     failure).
//   - Otherwise runs cfg.Command via the backend with the configured timeout;
//     the timeout kills the command.
//
// prev may be nil (first round). The returned GateState must be threaded back
// into the next call by the caller. backend may be nil — RunGate falls back
// to a local backend (with a debug-level warning).
func RunGate(ctx context.Context, cfg GateConfig, backend runtime.ExecutionBackend, workdir string, prev *GateState) (*GateResult, GateState, error) {
	logger := slog.Default()

	if backend == nil {
		logger.Debug("gate: no execution backend configured; falling back to local backend")
		backend = runtime.NewLocalBackend(runtime.Config{}, os.Environ())
	}

	hash, err := workspaceHash(ctx, backend, workdir)
	if err != nil {
		return nil, GateState{}, fmt.Errorf("gate: compute workspace hash: %w", err)
	}
	state := GateState{WorkspaceHash: hash}

	// Skip-on-unchanged: only when there IS a previous FAILURE and the
	// workspace has not changed since.
	if cfg.SkipWhenUnchanged && prev != nil && prev.LastFailedOutput != "" && prev.WorkspaceHash == hash {
		logger.Debug("gate: skipping, workspace unchanged since last failure")
		return &GateResult{
			Passed:  false,
			Skipped: true,
			Reason:  "workspace unchanged since last gate failure",
		}, state, nil
	}

	res, err := backend.Execute(ctx, runtime.Command{
		Cmd:     cfg.Command,
		Dir:     workdir,
		Timeout: cfg.timeout(),
	})
	if err != nil {
		return nil, state, fmt.Errorf("gate: execute %q: %w", cfg.Command, err)
	}

	result := &GateResult{
		Passed: res.ExitCode == 0,
		Output: res.Output,
	}
	if !result.Passed {
		state.LastFailedOutput = truncate(res.Output, 4096)
	}
	logger.Debug("gate run complete",
		"passed", result.Passed,
		"exit_code", res.ExitCode,
		"output_len", len(result.Output))
	return result, state, nil
}
