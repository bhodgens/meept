package daemon

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// TestStepJobTurnContextIsPerTurn pins F14 (bughunt 2026-09-12 wave) at the
// daemon boundary. A step job's turn must carry the AUTONOMOUS marker, and the
// marker must live on the JOB's context — not on the agent loop, which for the
// fallback branches is the process-wide interactive loop ChatHandler serves.
// The behavioural half of the pin (tools observe it; the next interactive turn
// on the same loop does not) lives in
// internal/agent/autonomous_context_test.go.
func TestStepJobTurnContextIsPerTurn(t *testing.T) {
	base := context.Background()

	if tools.AutonomousFromContext(stepJobTurnContext(base, false)) {
		t.Error("non-step job turn marked autonomous: an interactive/legacy job turn would skip the pending-change preview")
	}

	step := stepJobTurnContext(base, true)
	if !tools.AutonomousFromContext(step) {
		t.Error("step job turn NOT marked autonomous: file_write/file_edit would stage a change nothing accepts (e2e run 8 regression)")
	}

	// The marker is scoped to the derived context: the caller's context (and
	// therefore every other turn sharing the loop) stays interactive.
	if tools.AutonomousFromContext(base) {
		t.Error("parent context mutated: the autonomous marker leaked outside the job's turn")
	}

	// Values already on the job context survive the marker (per-turn context
	// values such as the working dir are injected the same way).
	type ctxKey struct{}
	withValue := context.WithValue(base, ctxKey{}, "keep-me")
	marked := stepJobTurnContext(withValue, true)
	if got := marked.Value(ctxKey{}); got != "keep-me" {
		t.Errorf("step-job context dropped a parent value: got %v, want keep-me", got)
	}
	if !tools.AutonomousFromContext(marked) {
		t.Error("step-job context lost the autonomous marker when the parent carried values")
	}
}
