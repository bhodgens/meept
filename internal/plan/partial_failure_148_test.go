package plan

// Issue #148 (3): ApprovePlan per-goal outcomes. The public signature stays
// ApprovePlan(ctx, planID, sessionID, by) error — every plan approve is its
// own call (PlanDisposer, RPC plan.approve, services, plan-mode tool), so
// "siblings proceed" is achieved by the already-committed approval standing
// regardless of THIS plan's synthesis failure, and the failed plan is marked
// on its row (synthesis_error column) with the concrete error instead of the
// outcome evaporating into a log line. A successful (re-)synthesis clears
// the mark. These tests pin that shape.

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// TestApprovePlan_SynthesisFailureMarksRow pins the per-goal failure mark:
// the approve call still returns nil (the committed approval stands, and a
// sibling goal's separate approve is never aborted by this plan's expansion
// failure), the row stays approved, and synthesis_error carries WHY.
func TestApprovePlan_SynthesisFailureMarksRow(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()
	creator := &countingCreator{alwaysFailPar: true}
	mgr.taskCreator = creator

	p, err := mgr.CreatePlan(ctx, "Failing expand", "synthesis error", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if err := mgr.ApprovePlan(ctx, p.ID, "sess-001", "reviewer"); err != nil {
		t.Fatalf("ApprovePlan must return nil on synthesis failure: %v", err)
	}

	got, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateApproved {
		t.Errorf("state = %q, want %q (approval stands)", got.State, StateApproved)
	}
	if !strings.Contains(got.SynthesisError, errSyntheticCreate.Error()) {
		t.Errorf("synthesis_error = %q, want the concrete synthesis failure %q",
			got.SynthesisError, errSyntheticCreate.Error())
	}
}

// TestApprovePlan_PartialFailureSiblingsProceed is the partial-failure test:
// goal A's plan fails synthesis, goal B's plan (a sibling, approved through
// its own call) still expands and runs — A's failure never aborts B — and the
// aggregated view (row state + synthesis_error) distinguishes the two
// outcomes. A re-drive of A that succeeds clears the mark.
func TestApprovePlan_PartialFailureSiblingsProceed(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()
	// The FIRST parent task creation fails (goal A's expansion); every
	// later one succeeds (goal B + A's re-drive).
	creator := &countingCreator{failParentAt: 1}
	mgr.taskCreator = creator

	mkApproved := func(title string) *Plan {
		t.Helper()
		p, err := mgr.CreatePlan(ctx, title, "expand me", "proj-1", dir, "sess-001")
		if err != nil {
			t.Fatalf("CreatePlan(%s): %v", title, err)
		}
		if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
			t.Fatalf("SubmitPlan(%s): %v", title, err)
		}
		if err := mgr.ApprovePlan(ctx, p.ID, "sess-001", "reviewer"); err != nil {
			t.Fatalf("ApprovePlan(%s) must not fail the call: %v", title, err)
		}
		return p
	}

	goalA := mkApproved("Goal A (synthesis fails)")
	goalB := mkApproved("Goal B (sibling proceeds)")

	a, err := mgr.store.GetPlan(ctx, goalA.ID)
	if err != nil {
		t.Fatalf("GetPlan A: %v", err)
	}
	if a.SynthesisError == "" {
		t.Error("goal A must carry its synthesis failure on the row")
	}
	if a.State != StateApproved {
		t.Errorf("goal A state = %q, want %q (failed expansion does not undo the approval)", a.State, StateApproved)
	}

	b, err := mgr.store.GetPlan(ctx, goalB.ID)
	if err != nil {
		t.Fatalf("GetPlan B: %v", err)
	}
	// The sibling is the point: goal A's failure never touched goal B.
	if b.State != StateExecuting {
		t.Errorf("sibling goal B state = %q, want %q — a failed sibling must not abort it", b.State, StateExecuting)
	}
	if b.TaskID == "" {
		t.Error("sibling goal B never expanded (no task_id)")
	}
	if b.SynthesisError != "" {
		t.Errorf("sibling goal B carries a spurious synthesis_error: %q", b.SynthesisError)
	}

	// Re-drive A (what the synthesis reclaimer does): this time expansion
	// succeeds, and the failure mark must clear with it.
	if err := mgr.Synthesize(ctx, goalA.ID); err != nil {
		t.Fatalf("re-drive Synthesize(A): %v", err)
	}
	aRedriven, err := mgr.store.GetPlan(ctx, goalA.ID)
	if err != nil {
		t.Fatalf("GetPlan A after re-drive: %v", err)
	}
	if aRedriven.State != StateExecuting {
		t.Errorf("A after re-drive state = %q, want %q", aRedriven.State, StateExecuting)
	}
	if aRedriven.SynthesisError != "" {
		t.Errorf("A after successful re-drive still carries synthesis_error: %q", aRedriven.SynthesisError)
	}
}

// TestSetPlanPrompt_RoundTrips pins the storage half of the resume-prompt
// fix (issue #148): PlanManager.SetPlanPrompt persists the candidate goal
// prompt on the plan row and GetPlan carries it back — the exact value the
// daemon's planBriefLookup feeds a resumed ApproveAndExecute.
func TestSetPlanPrompt_RoundTrips(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()
	p, err := mgr.CreatePlan(ctx, "Prompted plan", "desc", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	const prompt = "perform the risky escresume-escalate action now"
	if err := mgr.SetPlanPrompt(ctx, p.ID, prompt); err != nil {
		t.Fatalf("SetPlanPrompt: %v", err)
	}
	got, err := mgr.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Prompt != prompt {
		t.Errorf("stored prompt = %q, want %q", got.Prompt, prompt)
	}
	// The write is targeted: the title (untouched column) survives.
	if got.Title != "Prompted plan" {
		t.Errorf("title clobbered: %q", got.Title)
	}
	// Unknown plan surfaces an error (the GoalLoop logs it, non-fatal).
	if err := mgr.SetPlanPrompt(ctx, "plan-does-not-exist", "x"); err == nil {
		t.Error("SetPlanPrompt on an unknown plan must error")
	}
}

// TestPromptSurvivesStoreReopen pins persistence across reopen — the resume
// may happen after a daemon restart, and the prompt must still be there.
func TestPromptSurvivesStoreReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reopen.db")
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	p := NewPlan("Persist across restart", "d", "proj", filepath.Join(dir, "p.md"), "sess")
	p.Prompt = "resume me verbatim"
	if err := store.CreatePlan(context.Background(), p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.GetPlan(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("GetPlan after reopen: %v", err)
	}
	if got.Prompt != "resume me verbatim" {
		t.Errorf("prompt after reopen = %q, want the persisted value", got.Prompt)
	}
}
