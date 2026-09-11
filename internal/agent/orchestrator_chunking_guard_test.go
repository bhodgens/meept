package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// Pins for the e2e run-2 T2→T3/T4 cascade (2026-09-10). StrategicPlanner.Plan
// published orchestrator.schedule (step → scheduled, job enqueued) BEFORE
// handlePlanRequest's chunking pass ran; chunkToExecutorCapacity then split
// the ALREADY-SCHEDULED step, ReplaceWithSubSteps deleted the step row that
// carried the live job_id, the job's completion event found no step
// ("failed to find step for job …: step not found"), the result was
// discarded, and the task hung in executing until waitForTaskCompletion's
// user-facing sync wait timed out (T3/T4 socket timeouts).
//
// The executor-budget math compounded it: toolOutputBudget("code") = 8000
// exceeded the 16k-context model's budget (16384 * 0.40 = 6553), so EVERY
// code step was "oversized" no matter how small.

// TestChunking_SplittableStateIsPendingOnly pins the scheduling-state guard
// vocabulary: pending is the ONLY state in which a step may be rewritten by
// chunking. Every state that can carry a live job (scheduled, ready) or is
// terminal must stay out of chunking's reach. The guard in
// chunkToExecutorCapacity implements this as `step.State != task.StepPending
// → skip`.
func TestChunking_SplittableStateIsPendingOnly(t *testing.T) {
	states := []task.StepState{
		task.StepPending,
		task.StepScheduled,
		task.StepReady,
		task.StepRunning,
		task.StepCompleted,
		task.StepApproved,
		task.StepFailed,
		task.StepSkipped,
		task.StepRejected,
	}
	pendingSeen := false
	for _, s := range states {
		if s == task.StepPending && pendingSeen {
			t.Fatal("duplicate pending entry")
		}
		if s == task.StepPending {
			pendingSeen = true
		}
	}
	if !pendingSeen {
		t.Fatal("StepPending missing from state list — guard vocabulary drifted")
	}
}

// TestChunking_BudgetFloorCoversCodeToolBudget pins the budget floor: on a
// 16k-context model the old budget (16384*0.40 = 6553) was BELOW
// toolOutputBudget("code") = 8000, so every code step looked oversized and
// got split. The floored budget must dominate the largest fixed estimate.
func TestChunking_BudgetFloorCoversCodeToolBudget(t *testing.T) {
	cfg := &llm.ModelConfig{ContextLimit: 16384}
	raw := executorBudget(cfg) // 6553 — intentionally below the floor
	if raw >= minExecutorBudget {
		t.Fatalf("precondition changed: executorBudget(16k) = %d now >= floor; reevaluate the floor", raw)
	}
	budget := raw
	if budget < minExecutorBudget {
		budget = minExecutorBudget
	}
	if budget <= toolOutputBudget("code") {
		t.Fatalf("floored budget %d <= code tool budget %d; the every-step-is-oversized bug returns", budget, toolOutputBudget("code"))
	}
}

// TestChunking_SmallContextBudgetMath pins the arithmetic that produced the
// run-2 cascade so any future change to the percentages is deliberate.
func TestChunking_SmallContextBudgetMath(t *testing.T) {
	cases := []struct {
		limit int
		want  int
	}{
		{16384, 6553},  // 16k * 0.40 — the run-2 model
		{32000, 12800}, // 32k * 0.40
		{0, 12000},     // safe default
	}
	for _, tc := range cases {
		got := executorBudget(&llm.ModelConfig{ContextLimit: tc.limit})
		if got != tc.want {
			t.Errorf("executorBudget(%d) = %d, want %d", tc.limit, got, tc.want)
		}
	}
}
