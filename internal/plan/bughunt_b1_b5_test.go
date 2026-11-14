package plan

// Bughunt 2026-11-10 fix-wave pin tests (findings B1, B3, B4, B5).
//
//   B1: OnTaskCompleted must honor the task.completed payload status — a
//       "failed" completion lands in PhaseFailed (never stamping a false
//       PhaseCompleted over an existing PhaseFailed), and only genuine
//       success writes PhaseCompleted. This is the a94995c8 honest-failure
//       contract at the task-completion boundary.
//   B3: planning must not be a dead-end — revise -> complete revision ->
//       submit -> approve round-trips, and stale planning rows are
//       prunable via retention.
//   B4: FK enforcement rides the DSN so EVERY pooled connection cascades
//       plan deletions (verified through a FRESH store handle).
//   B5: Synthesize persists through the updatePlanColumns re-read seam, so
//       a concurrent SetPlanGoal stamp survives.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// TestOnTaskCompletedFailedStatusFailsPhase pins B1: a failed completion
// (status "failed", exactly what internal/agent/tactical.go publishes) must
// flip the phase to PhaseFailed, not PhaseCompleted.
func TestOnTaskCompletedFailedStatusFailsPhase(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p := NewPlan("b1-failed-completion", "d", "", "", "")
	p.State = StateExecuting
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 1)
	ph.State = PhaseInProgress
	ph.TaskID = "task-b1"
	if err := mgr.store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	mgr.mu.Lock()
	mgr.phaseTaskMap["task-b1"] = ph.ID
	mgr.taskPlanMap["task-b1"] = p.ID
	mgr.mu.Unlock()

	if err := mgr.OnTaskCompleted(ctx, "task-b1", "failed"); err != nil {
		t.Fatalf("OnTaskCompleted(failed): %v", err)
	}

	phases, err := mgr.store.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 1 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	if phases[0].State != PhaseFailed {
		t.Errorf("failed completion left phase in %s, want %s — the false-success a94995c8 prevents",
			phases[0].State, PhaseFailed)
	}
	if phases[0].FailedSteps != 1 {
		t.Errorf("failed_steps = %d, want 1 (failure must be recorded, not just re-stamped)", phases[0].FailedSteps)
	}
}

// TestOnTaskCompletedFailedDoesNotOverwritePhaseFailed pins B1's no-stomp
// half: an existing PhaseFailed (written by OnStepFailed) must survive a
// failed task-completion event, and a SUCCESS completion must not resurrect
// it either.
func TestOnTaskCompletedFailedDoesNotOverwritePhaseFailed(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p := NewPlan("b1-no-stomp", "d", "", "", "")
	p.State = StateExecuting
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 2)
	ph.State = PhaseFailed
	ph.FailedSteps = 2
	ph.TaskID = "task-b1b"
	if err := mgr.store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	mgr.mu.Lock()
	mgr.phaseTaskMap["task-b1b"] = ph.ID
	mgr.taskPlanMap["task-b1b"] = p.ID
	mgr.mu.Unlock()

	// A duplicate failure event must not stomp the honest record.
	if err := mgr.OnTaskCompleted(ctx, "task-b1b", "failed"); err != nil {
		t.Fatalf("OnTaskCompleted(failed): %v", err)
	}
	phases, err := mgr.store.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 1 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	if phases[0].State != PhaseFailed {
		t.Errorf("phase failed -> %s after duplicate failed completion, want %s preserved",
			phases[0].State, PhaseFailed)
	}
	if phases[0].FailedSteps != 2 {
		t.Errorf("failed_steps = %d, want 2 (duplicate failure must not double-count)", phases[0].FailedSteps)
	}

	// A success event on the same task must NOT overwrite PhaseFailed.
	if err := mgr.OnTaskCompleted(ctx, "task-b1b", "completed"); err != nil {
		t.Fatalf("OnTaskCompleted(completed): %v", err)
	}
	phases, err = mgr.store.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 1 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	if phases[0].State != PhaseFailed {
		t.Errorf("phase failed -> %s after success completion, want %s preserved",
			phases[0].State, PhaseFailed)
	}
}

// TestOnTaskCompletedSuccessCompletesPhase pins the success half of B1: a
// genuine success still writes PhaseCompleted and drives the honest
// completion evaluation (plan fails if any OTHER phase failed).
func TestOnTaskCompletedSuccessCompletesPhase(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p := NewPlan("b1-success", "d", "", "", "")
	p.State = StateExecuting
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 1)
	ph.State = PhaseInProgress
	ph.TaskID = "task-b1c"
	if err := mgr.store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	mgr.mu.Lock()
	mgr.phaseTaskMap["task-b1c"] = ph.ID
	mgr.taskPlanMap["task-b1c"] = p.ID
	mgr.mu.Unlock()

	if err := mgr.OnTaskCompleted(ctx, "task-b1c", "completed"); err != nil {
		t.Fatalf("OnTaskCompleted(completed): %v", err)
	}
	phases, err := mgr.store.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 1 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	if phases[0].State != PhaseCompleted {
		t.Errorf("successful completion left phase in %s, want %s", phases[0].State, PhaseCompleted)
	}
	if phases[0].FailedSteps != 0 {
		t.Errorf("failed_steps = %d, want 0 for a successful completion", phases[0].FailedSteps)
	}
	// All-phases-terminal eval fired: single-phase plan completes honestly.
	final, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if final.State != StateCompleted {
		t.Errorf("plan with all-completed phases ended %s, want %s", final.State, StateCompleted)
	}
}

// TestPlanningRoundTrip pins B3: a revised plan (RevisePlan -> planning) is
// no longer stranded — CompletePlanningRevision returns it to draft, and
// the normal submit -> approve lifecycle runs to completion.
func TestPlanningRoundTrip(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	plan, err := mgr.CreatePlan(ctx, "b3-roundtrip", "d", "", "", "sess-b3")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	// Revision sends it back to planning.
	if err := mgr.RevisePlan(ctx, plan.ID, "sess-b3", "rework"); err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StatePlanning {
		t.Fatalf("state after revise = %s, want %s", got.State, StatePlanning)
	}

	// The revision writer returns it to draft; the lifecycle resumes.
	if err := mgr.CompletePlanningRevision(ctx, plan.ID); err != nil {
		t.Fatalf("CompletePlanningRevision: %v", err)
	}
	got, err = mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateDraft {
		t.Fatalf("state after revision completion = %s, want %s", got.State, StateDraft)
	}
	if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
		t.Fatalf("SubmitPlan (round-trip): %v", err)
	}
	// ApprovePlan synthesizes with a nil taskCreator. Synthesis failure is
	// non-fatal once approval is committed (ApprovePlan returns nil);
	// runSynthesisReclaimPass retries approved plans with no task. The plan
	// must sit in StateApproved (or executing if a creator ran).
	if err := mgr.ApprovePlan(ctx, plan.ID, "sess-b3", "reviewer"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	got, err = mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateApproved && got.State != StateExecuting {
		t.Errorf("round-trip final state = %s, want approved (or executing if synthesis ran)", got.State)
	}
}

// TestPruneStalePlansCancelsStalePlanning pins B3's retention half: a stale
// planning row is cancelled (then deletable next pass), not warn-and-skip.
func TestPruneStalePlansCancelsStalePlanning(t *testing.T) {
	m, _ := setupTestManagerWithDir(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -30)

	seedPlan(t, m, "b3-stale-planning", StatePlanning, old, "")
	seedPlan(t, m, "b3-fresh-planning", StatePlanning, time.Now().UTC(), "")

	deleted, cancelled, err := m.PruneStalePlans(ctx, 7)
	if err != nil {
		t.Fatalf("PruneStalePlans: %v", err)
	}
	_ = deleted
	if cancelled == 0 {
		t.Fatal("stale planning row was not cancelled — planning rows still strand")
	}
	got, err := m.store.GetPlan(ctx, "b3-stale-planning")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateCancelled {
		t.Errorf("stale planning row state = %s, want %s", got.State, StateCancelled)
	}
	fresh, err := m.store.GetPlan(ctx, "b3-fresh-planning")
	if err != nil {
		t.Fatalf("GetPlan fresh: %v", err)
	}
	if fresh.State != StatePlanning {
		t.Errorf("fresh planning row was touched: %s", fresh.State)
	}
}

// TestDeletePlanCascadesViaPooledConn pins B4: FK enforcement must hold on
// ANY pooled connection, not just migrate()'s. The delete runs on a
// DIFFERENT handle than the insert, and the orphan check runs through a
// THIRD fresh handle — the exact shape that silently no-op'd when only one
// conn had PRAGMA foreign_keys=ON.
func TestDeletePlanCascadesViaPooledConn(t *testing.T) {
	ctx := t.Context()
	logger := slog.Default()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "b4.db")

	writer, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore(writer): %v", err)
	}
	p := NewPlan("b4-cascade", "d", "", "", "")
	if err := writer.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for i := 0; i < 3; i++ {
		ph := NewPlanPhase(p.ID, "Phase", i+1, 0)
		if err := writer.CreatePhase(ctx, ph); err != nil {
			t.Fatalf("CreatePhase: %v", err)
		}
	}
	writer.Close()

	// FRESH handle: its pool has never run migrate()'s PRAGMA.
	deleter, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore(deleter): %v", err)
	}
	defer deleter.Close()
	if err := deleter.DeletePlan(ctx, p.ID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	deleter.Close()

	// THIRD fresh handle proves the rows are gone on disk.
	verifier, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore(verifier): %v", err)
	}
	defer verifier.Close()
	phases, err := verifier.GetPhases(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPhases: %v", err)
	}
	if len(phases) != 0 {
		t.Errorf("delete left %d orphan phase rows — FK CASCADE no-op'd on the pooled conn", len(phases))
	}
}

// countingGoalCreator is a countingCreator variant that records goal stamps
// so the B5 race can interleave SetPlanGoal mid-synthesis.
type countingGoalCreator struct {
	countingCreator
}

// goalStampingCreator stamps the plan's goal from INSIDE CreateTask — a
// deterministic mid-synthesis stamp. The manager reads its plan snapshot
// BEFORE task creation and persists after it, exactly the window the stale
// full-row write used to revert.
type goalStampingCreator struct {
	countingCreator
	mgr    *PlanManager
	planID string
	stamp  string
}

func (c *goalStampingCreator) CreateTask(ctx context.Context, name, description string) (*task.Task, error) {
	_ = c.mgr.SetPlanGoal(ctx, c.planID, c.stamp)
	return c.countingCreator.CreateTask(ctx, name, description)
}

// TestSynthesizePreservesConcurrentGoalStamp pins B5: the synthesis tail
// write must go through the re-read seam, so a goal stamped by SetPlanGoal
// DURING task creation survives the task_id persistence. The interleaving
// is deterministic: the stamp fires inside CreateTask, strictly after the
// manager's snapshot read and strictly before its tail write.
func TestSynthesizePreservesConcurrentGoalStamp(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	plan, err := mgr.CreatePlan(ctx, "b5-goal-survival", "d", "", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.store.SetPlanState(ctx, plan.ID, StateApproved); err != nil {
		t.Fatalf("SetPlanState: %v", err)
	}
	mgr.taskCreator = &goalStampingCreator{mgr: mgr, planID: plan.ID, stamp: "goal-b5"}

	if err := mgr.Synthesize(ctx, plan.ID); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.TaskID == "" {
		t.Fatal("synthesis did not persist task_id")
	}
	if got.GoalID != "goal-b5" {
		t.Errorf("goal_id = %q after concurrent synthesis, want goal-b5 — the stale-snapshot write reverted it", got.GoalID)
	}
	if got.State != StateExecuting {
		t.Errorf("post-synthesis state = %s, want %s", got.State, StateExecuting)
	}
}
