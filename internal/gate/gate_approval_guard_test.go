package gate

// Pin test for the RunGate approval-gate guard (audit e25f2512 follow-up):
// an approval gate must NEVER shell out, even if a future call path
// reaches RunGate directly. The guard returns a skipped, failed result
// without touching the execution backend — pinned here with a backend
// that FAILS the test on any Execute call, including the workspaceHash
// pre-pass RunGate normally performs (approval gates run no commands at
// all, git hashing included).

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/runtime"
)

// panicBackend records any Execute call as a test failure and returns a
// passing result (so a stray execution would silently look like a pass —
// only the guard must prevent reaching it).
type panicBackend struct {
	t        *testing.T
	executed []string
}

func (b *panicBackend) Execute(ctx context.Context, cmd runtime.Command) (*runtime.CommandResult, error) {
	b.executed = append(b.executed, cmd.Cmd)
	b.t.Errorf("approval gate executed a command: %q", cmd.Cmd)
	return &runtime.CommandResult{Output: "", ExitCode: 0}, nil
}

func (b *panicBackend) Name() string { return "panic-backend" }
func (b *panicBackend) Close() error { return nil }

// TestRunGateApprovalGateNeverShellsOut pins the local invariant: a
// GateConfig with Type "approval" reaching RunGate is skipped with no
// backend execution — even with a command string present in the config
// (EffectiveCommand() is "" for approval gates).
func TestRunGateApprovalGateNeverShellsOut(t *testing.T) {
	backend := &panicBackend{t: t}
	result, state, err := RunGate(context.Background(), GateConfig{
		Type:              GateTypeApproval,
		Command:           "echo should-never-run",
		SkipWhenUnchanged: true,
	}, backend, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("RunGate returned error for an approval gate: %v", err)
	}
	if result == nil {
		t.Fatal("RunGate returned nil result for an approval gate")
	}
	if !result.Skipped {
		t.Errorf("approval gate result Skipped = false, want true (no gate run)")
	}
	if result.Passed {
		t.Errorf("approval gate result Passed = true, want false (human signoff required)")
	}
	if result.Reason == "" {
		t.Error("approval gate result Reason is empty; want an explanatory skip reason")
	}
	// The workspace hash pre-pass must not run either: no command at all.
	if len(backend.executed) != 0 {
		t.Errorf("approval gate caused %d backend executions, want 0: %v", len(backend.executed), backend.executed)
	}
	// No gate state is threaded (nothing ran).
	if state != (GateState{}) {
		t.Errorf("approval gate returned non-zero GateState: %+v", state)
	}
}
