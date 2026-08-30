package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/gate"
	"github.com/caimlas/meept/internal/runtime"
)

// rosterGateMaxOutput caps gate output fed back into the loop. Matches the
// leaf contract (4KB) and RunGate's internal truncation.
const rosterGateMaxOutput = 4096

// rosterMutatingTools is the set of tool names that count as workspace
// mutations for gate triggering (contract C2). shell_execute is treated as
// mutating unconditionally: it can write files, and proving "read-only" for
// an arbitrary shell command is not tractable.
var rosterMutatingTools = map[string]bool{
	"file_write":    true,
	"file_edit":     true,
	"file_delete":   true,
	"shell_execute": true,
}

// RosterGateConfig is the per-agent roster quality gate carried on AgentSpec
// (converted from agents.GateMetadata at registry time). A nil *RosterGate on
// the spec means no gate; a non-nil config with an empty Command is
// equivalent to no gate (Evaluate reports skipped/not-applicable).
type RosterGateConfig struct {
	// Command is the shell command run in the session workdir after a turn
	// that mutated the workspace. Empty = no gate.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// TimeoutSeconds kills the gate command after this many seconds.
	// Zero/negative = gate.DefaultGateTimeoutSeconds (300).
	TimeoutSeconds int `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	// SkipWhenUnchanged skips re-running a previously FAILED gate when the
	// workspace (git status + HEAD) hash is unchanged since that failure.
	SkipWhenUnchanged bool `json:"skip_when_unchanged" yaml:"skip_when_unchanged"`
}

// RosterGate is the per-conversation runtime wrapper around
// gate.RunGate. It holds the cross-turn GateState needed for
// skip-when-unchanged and serializes gate execution within one loop.
// Construction via NewRosterGate; each AgentLoop owns its own instance.
type RosterGate struct {
	cfg               RosterGateConfig
	backend           runtime.ExecutionBackend // nil = RunGate's local fallback
	skipWhenUnchanged bool

	mu    sync.Mutex
	state *gate.GateState // last gate run state (nil = never ran)
}

// NewRosterGate builds a RosterGate from converted spec config. Returns nil
// when Command is empty (no gate configured) so callers can use the
// `spec.Gate != nil` fast path.
func NewRosterGate(cfg RosterGateConfig) *RosterGate {
	if cfg.Command == "" {
		return nil
	}
	return &RosterGate{
		cfg:               cfg,
		skipWhenUnchanged: cfg.SkipWhenUnchanged,
	}
}

// SetBackend overrides the execution backend (tests inject a stub). When nil,
// RunGate falls back to a local backend.
func (g *RosterGate) SetBackend(b runtime.ExecutionBackend) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.backend = b
}

// Evaluate runs the roster gate after a completed turn.
//
//   - No command configured            => skipped, not applicable.
//   - !turnHadMutatingTool             => skipped, read-only turn.
//   - Otherwise: RunGate runs cfg.Command in workdir (the SESSION working
//     directory — callers must pass the loop's session workdir, never the
//     daemon CWD / os.Getwd).
//
// The returned output is capped at 4KB (rosterGateMaxOutput) so the loop can
// feed a failure back into the next turn; gate output is never logged above
// debug. passed=false means the turn must not be reported as success; the
// caller decides how to surface the failure.
func (g *RosterGate) Evaluate(ctx context.Context, workdir string, turnHadMutatingTool bool) (passed bool, output string, skipped bool, err error) {
	if g == nil || g.cfg.Command == "" {
		return false, "", true, nil
	}
	if !turnHadMutatingTool {
		// Read-only turn: no workspace mutation to verify.
		return false, "", true, nil
	}

	g.mu.Lock()
	prev := g.state
	backend := g.backend
	g.mu.Unlock()

	result, state, err := gate.RunGate(ctx, gate.GateConfig{
		Command:           g.cfg.Command,
		TimeoutSeconds:    g.cfg.TimeoutSeconds,
		SkipWhenUnchanged: g.skipWhenUnchanged,
	}, backend, workdir, prev)
	if err != nil {
		return false, "", false, fmt.Errorf("roster gate: %w", err)
	}

	g.mu.Lock()
	g.state = &state
	g.mu.Unlock()

	if result.Skipped {
		slog.Debug("roster gate skipped",
			"command", g.cfg.Command,
			"reason", result.Reason,
		)
		return false, "", true, nil
	}

	out := truncateRosterGateOutput(result.Output)

	if result.Passed {
		// Lowercase debug log only — no user-visible banner on pass.
		slog.Debug("roster gate passed", "command", g.cfg.Command)
		return true, out, false, nil
	}

	// Never log gate output above debug (it may contain code/secrets).
	slog.Debug("roster gate failed", "command", g.cfg.Command)
	return false, out, false, nil
}

// truncateRosterGateOutput caps gate output at rosterGateMaxOutput bytes with
// an elision marker.
func truncateRosterGateOutput(s string) string {
	if len(s) <= rosterGateMaxOutput {
		return s
	}
	return strings.TrimSpace(s[:rosterGateMaxOutput]) + "\n... (gate output truncated)"
}

// rosterGateFailureMessage builds the turn-facing text for a failed gate. The
// (4KB-capped) gate output is fed back so the agent can fix the failure.
func rosterGateFailureMessage(command string, output string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ROSTER GATE FAILED: `%s` exited non-zero. Fix the issue before reporting success.\n", command)
	if output != "" {
		b.WriteString("\nGate output:\n```\n")
		b.WriteString(output)
		b.WriteString("\n```\n")
	}
	return b.String()
}
