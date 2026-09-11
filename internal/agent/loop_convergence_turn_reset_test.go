package agent

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// Pins for e2e run 3 (2026-09-10) Finding B1: T2's quickplan handoff aborted
// with "Convergence detected in responses ... count=3" at ITERATION=1 —
// meaning the detector's window held two identical entries recorded during
// EARLIER turns on the same session-persistent chat loop. Convergence is a
// within-turn semantic; resetTurnGuards must clear both detectors so a fresh
// turn starts with an empty history.

// A fresh turn (resetTurnGuards) must not inherit the previous turn's
// response history: two identical no-tool replies from turn N plus one
// distinct reply in turn N+1 must NOT abort turn N+1.
func TestResetTurnGuards_ClearsConvergenceHistory(t *testing.T) {
	loop := NewAgentLoop("sess", ".") //nolint:govet // fieldalignment not enforced here
	cd := loop.convergenceDetector
	if cd == nil {
		t.Fatal("NewAgentLoop must initialize convergenceDetector")
	}

	// Turn N: two identical no-tool replies fill the window.
	if cd.recordResponse("stale reply from previous turn", false) {
		t.Fatal("unexpected convergence on first stale reply")
	}
	if cd.recordResponse("stale reply from previous turn", false) {
		t.Fatal("unexpected convergence on second stale reply")
	}

	// New turn begins: guards reset.
	loop.resetTurnGuards()

	// Turn N+1: one fresh distinct reply — with stale history this is the
	// third identical entry and would have aborted (run 3's count=3 at
	// iteration=1).
	if cd.recordResponse("a genuinely new answer", false) {
		t.Fatal("stale history tripped convergence in a fresh turn; resetTurnGuards must clear the detector")
	}
}

// Symmetric pin for the cycle detector: identical tool calls from a
// previous turn must not count toward this turn's cycle veto.
func TestResetTurnGuards_ClearsCycleHistory(t *testing.T) {
	loop := NewAgentLoop("sess", ".") //nolint:govet // fieldalignment not enforced here
	cyc := loop.cycleDetector
	if cyc == nil {
		t.Fatal("NewAgentLoop must initialize cycleDetector")
	}

	if cyc.recordCall("web_search", `{"query":"x"}`) {
		t.Fatal("unexpected cycle on first stale call")
	}
	if cyc.recordCall("web_search", `{"query":"x"}`) {
		t.Fatal("unexpected cycle on second stale call")
	}

	loop.resetTurnGuards()

	if cyc.recordCall("web_search", `{"query":"x"}`) {
		t.Fatal("stale history tripped cycle detection in a fresh turn; resetTurnGuards must clear the cycle detector")
	}
}

// Reset must actually empty the window, not just silence it: after Reset,
// even repeated identical replies need a full threshold of NEW entries
// before converging.
func TestConvergenceDetector_ResetRestartsStreak(t *testing.T) {
	cd := newTestConvergenceDetector()

	for i := 0; i < 2; i++ {
		if cd.recordResponse("same old answer", false) {
			t.Fatalf("entry %d: premature convergence", i+1)
		}
	}
	if !cd.recordResponse("same old answer", false) {
		t.Fatal("expected convergence after threshold identical replies")
	}

	cd.Reset()

	// Post-reset, a fresh streak must build from zero: the first two new
	// entries must NOT converge (pre-reset leftovers would make them).
	for i := 0; i < 2; i++ {
		if cd.recordResponse("same old answer", false) {
			t.Fatalf("post-reset entry %d converged early; Reset must clear history", i+1)
		}
	}
	if !cd.recordResponse("same old answer", false) {
		t.Fatal("expected convergence after post-reset threshold")
	}
}

// ---- sync-wait bound pins (Finding B3) ----

// waitForTaskCompletion must return a degraded reply at the sync ceiling
// instead of holding the reply channel for 10 minutes. Run 3's T2 replan
// storm held every subsequent turn until the CLI's ~120s socket read died.
func TestChatHandler_WaitForTaskCompletion_BoundedByCeiling(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.syncWaitCeiling = 200 * time.Millisecond

	tk := task.NewTask("never completes", "stuck task pin")
	// StatePending: never terminal during the test.
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	start := time.Now()
	reply := h.waitForTaskCompletion(context.Background(), tk.ID)
	elapsed := time.Since(start)

	if elapsed >= 5*time.Second {
		t.Fatalf("waitForTaskCompletion held %.1fs; sync ceiling must bound the wait", elapsed.Seconds())
	}
	if reply == "" {
		t.Fatal("expected a degraded still-running reply, got empty string")
	}
	if got, err := h.taskStore.GetByID(tk.ID); err != nil || got == nil || got.State != task.StatePending {
		t.Fatalf("task must remain non-terminal after ceiling (state=%v err=%v)", tk.State, err)
	}
}

// A task that terminalizes before the ceiling still returns its real step
// result (existing behavior preserved).
func TestChatHandler_WaitForTaskCompletion_CompletesBeforeCeiling(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.syncWaitCeiling = 5 * time.Second

	taskID := seedCompletedTaskWithStepResult(t, h, "the actual step result")

	reply := h.waitForTaskCompletion(context.Background(), taskID)
	if reply != "the actual step result" {
		t.Fatalf("reply = %q, want the seeded step result", reply)
	}
}

// Pins e2e run 5 (2026-09-11) A4/T1: at the ceiling the task may not be
// terminal yet, but an APPROVED step can already hold the user's answer —
// the wait must return that result instead of the generic still-running
// stub. Run 5 shipped the stub 22s before the task finalized while step 2
// held the file path the user asked for.
func TestChatHandler_WaitForTaskCompletion_CeilingReturnsBestStepResult(t *testing.T) {
	h := newTestChatHandlerWithStores(t)
	h.syncWaitCeiling = 200 * time.Millisecond

	tk := task.NewTask("slow bookkeeping", "best-step-at-ceiling pin")
	// StateExecuting: never terminal during the test.
	tk.State = task.StateExecuting
	if err := h.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	step := task.NewTaskStep(tk.ID, "create hello.txt", 2)
	step.State = task.StepApproved
	step.Result = "/var/.../project/hello.txt contains hello"
	if err := h.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	// An earlier needs-info step must NOT win (bestStepResult prefers the
	// highest completed/approved sequence).
	early := task.NewTaskStep(tk.ID, "write file", 0)
	early.State = task.StepCompleted
	early.Result = "Insufficient evidence to confirm completion."
	if err := h.stepStore.Create(early); err != nil {
		t.Fatalf("create early step: %v", err)
	}

	reply := h.waitForTaskCompletion(context.Background(), tk.ID)
	if reply != "/var/.../project/hello.txt contains hello" {
		t.Fatalf("reply = %q, want the approved step's result at ceiling", reply)
	}
}
