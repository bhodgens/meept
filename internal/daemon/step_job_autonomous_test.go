package daemon

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/queue"
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

// TestProcessRunsMarkedStepJobTurn pins the WIRING half of the same finding:
// AgentJobProcessor.Process must hand the loop a context that carries the
// AUTONOMOUS marker for a step job, and must NOT mark a legacy/interactive
// job's turn. stepJobTurnContext can be perfectly correct while the
// assignment is dropped (or moved below the RunOnce call) — this observes the
// context at the loop boundary through the processor's test seam.
//
// RunOnce itself fails here (the loop has no LLM client), which is fine: the
// observer runs immediately before the call, so the assertion is about what
// the loop WOULD have received.
func TestProcessRunsMarkedStepJobTurn(t *testing.T) {
	var seen []bool
	loop := agent.NewAgentLoop("sess-process-autonomous", t.TempDir())
	p := &AgentJobProcessor{agentLoop: loop, logger: testLogger(t)}
	p.turnContextObserver = func(ctx context.Context) {
		seen = append(seen, tools.AutonomousFromContext(ctx))
	}

	stepJob := &queue.Job{
		ID:      "job-step",
		Payload: []byte(`{"step_id":"st1","task_id":"t1","description":"write the file"}`),
	}
	if _, _ = p.Process(context.Background(), stepJob); len(seen) != 1 {
		t.Fatalf("loop reached %d times for a step job, want 1 (observer saw %v)", len(seen), seen)
	}
	if !seen[0] {
		t.Error("Process handed the loop an UNMARKED step-job turn: file_write/file_edit would stage a change nothing accepts (e2e run 8)")
	}

	// A legacy (non-step) job's turn stays interactive: marking it would
	// silently bypass the pending-change preview for a user-facing job.
	seen = nil
	legacyJob := &queue.Job{ID: "job-legacy", Payload: []byte(`{"prompt":"hello"}`)}
	if _, _ = p.Process(context.Background(), legacyJob); len(seen) != 1 {
		t.Fatalf("loop reached %d times for a legacy job, want 1 (observer saw %v)", len(seen), seen)
	}
	if seen[0] {
		t.Error("Process marked a legacy/interactive job turn autonomous: the pending-change preview/accept workflow is bypassed")
	}

	// The marker must live on the TURN, never on the loop: the loop-level
	// latch (SetAutonomous) has no reset, so setting it here would disable
	// the preview for every later interactive turn on this loop — the F14
	// defect the context marker replaced. SetAutonomous stays exported as an
	// escape hatch for genuinely headless loops; it is not this path's.
	if loop.LoopAutonomousMarker() {
		t.Error("Process set the loop-level autonomous latch: it is never cleared, so every later interactive turn on this loop would skip the pending-change preview")
	}
}
