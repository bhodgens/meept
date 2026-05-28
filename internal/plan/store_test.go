package plan

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

// setupTestStore creates a fresh SQLiteStore backed by a temporary database.
func setupTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	logger := slog.Default()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// newTestPlan creates a Plan with sensible defaults for testing.
func newTestPlan(title string) *Plan {
	return NewPlan(title, "test description", "test-project", "/tmp/plans/test.md", "sess-test")
}

// ---------- Tests ----------

func TestCreateAndGetPlan(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	original := newTestPlan("test plan")
	if err := store.CreatePlan(ctx, original); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	got, err := store.GetPlan(ctx, original.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}

	// Verify all fields match.
	if got.ID != original.ID {
		t.Errorf("ID: got %q, want %q", got.ID, original.ID)
	}
	if got.Title != original.Title {
		t.Errorf("Title: got %q, want %q", got.Title, original.Title)
	}
	if got.Description != original.Description {
		t.Errorf("Description: got %q, want %q", got.Description, original.Description)
	}
	if got.FilePath != original.FilePath {
		t.Errorf("FilePath: got %q, want %q", got.FilePath, original.FilePath)
	}
	if got.ProjectID != original.ProjectID {
		t.Errorf("ProjectID: got %q, want %q", got.ProjectID, original.ProjectID)
	}
	if got.State != original.State {
		t.Errorf("State: got %q, want %q", got.State, original.State)
	}
	if got.SourceSession != original.SourceSession {
		t.Errorf("SourceSession: got %q, want %q", got.SourceSession, original.SourceSession)
	}
	if got.RevisionCount != original.RevisionCount {
		t.Errorf("RevisionCount: got %d, want %d", got.RevisionCount, original.RevisionCount)
	}

	// Timestamps should be close (RFC3339 round-trip loses sub-second precision).
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}

	// Non-existent plan should return ErrPlanNotFound.
	_, err = store.GetPlan(ctx, "nonexistent")
	if err != ErrPlanNotFound {
		t.Errorf("GetPlan(nonexistent): got err=%v, want ErrPlanNotFound", err)
	}
}

func TestUpdatePlanState(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	p := newTestPlan("state machine test")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	transitions := []PlanState{
		StateDraft,
		StatePendingApproval,
		StateApproved,
		StateExecuting,
		StateCompleted,
		StateConfirmed,
	}

	for _, want := range transitions {
		if err := store.SetPlanState(ctx, p.ID, want); err != nil {
			t.Fatalf("SetPlanState(%s): %v", want, err)
		}
		got, err := store.GetPlan(ctx, p.ID)
		if err != nil {
			t.Fatalf("GetPlan after transition to %s: %v", want, err)
		}
		if got.State != want {
			t.Errorf("state after transition: got %q, want %q", got.State, want)
		}
	}
}

func TestCreateAndGetPhases(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	p := newTestPlan("phase test")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Create 3 phases: Design (3 steps), Implementation (4 steps), Testing (2 steps).
	phases := []*PlanPhase{
		NewPlanPhase(p.ID, "Design", 1, 3),
		NewPlanPhase(p.ID, "Implementation", 2, 4),
		NewPlanPhase(p.ID, "Testing", 3, 2),
	}
	for _, ph := range phases {
		if err := store.CreatePhase(ctx, ph); err != nil {
			t.Fatalf("CreatePhase(%s): %v", ph.Name, err)
		}
	}

	got, err := store.GetPhases(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPhases: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("GetPhases: got %d phases, want 3", len(got))
	}

	// Verify ordering by sequence.
	wantNames := []string{"Design", "Implementation", "Testing"}
	wantSteps := []int{3, 4, 2}
	for i, ph := range got {
		if ph.Name != wantNames[i] {
			t.Errorf("phase[%d].Name: got %q, want %q", i, ph.Name, wantNames[i])
		}
		if ph.TotalSteps != wantSteps[i] {
			t.Errorf("phase[%d].TotalSteps: got %d, want %d", i, ph.TotalSteps, wantSteps[i])
		}
		if ph.Sequence != i+1 {
			t.Errorf("phase[%d].Sequence: got %d, want %d", i, ph.Sequence, i+1)
		}
		if ph.State != PhasePending {
			t.Errorf("phase[%d].State: got %q, want %q", i, ph.State, PhasePending)
		}
		if ph.CompletedSteps != 0 {
			t.Errorf("phase[%d].CompletedSteps: got %d, want 0", i, ph.CompletedSteps)
		}
		if ph.FailedSteps != 0 {
			t.Errorf("phase[%d].FailedSteps: got %d, want 0", i, ph.FailedSteps)
		}
	}
}

func TestPhaseProgress(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	p := newTestPlan("progress test")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	phase := NewPlanPhase(p.ID, "Build", 1, 5)
	if err := store.CreatePhase(ctx, phase); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}

	// Increment completed_steps by 3.
	if err := store.IncrementPhaseProgress(ctx, phase.ID, "completed_steps", 3); err != nil {
		t.Fatalf("IncrementPhaseProgress(completed_steps, 3): %v", err)
	}

	phases, err := store.GetPhases(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPhases: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("GetPhases: got %d phases, want 1", len(phases))
	}
	if phases[0].CompletedSteps != 3 {
		t.Errorf("CompletedSteps after +3: got %d, want 3", phases[0].CompletedSteps)
	}
	if phases[0].TotalSteps != 5 {
		t.Errorf("TotalSteps: got %d, want 5", phases[0].TotalSteps)
	}

	// Increment failed_steps by 1.
	if err := store.IncrementPhaseProgress(ctx, phase.ID, "failed_steps", 1); err != nil {
		t.Fatalf("IncrementPhaseProgress(failed_steps, 1): %v", err)
	}

	phases, err = store.GetPhases(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPhases: %v", err)
	}
	if phases[0].FailedSteps != 1 {
		t.Errorf("FailedSteps after +1: got %d, want 1", phases[0].FailedSteps)
	}
	// CompletedSteps should still be 3.
	if phases[0].CompletedSteps != 3 {
		t.Errorf("CompletedSteps after failed increment: got %d, want 3", phases[0].CompletedSteps)
	}

	// Invalid field should error.
	if err := store.IncrementPhaseProgress(ctx, phase.ID, "invalid_field", 1); err == nil {
		t.Error("IncrementPhaseProgress with invalid field should return error")
	}
}

func TestSessionLinking(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	plan1 := newTestPlan("plan one")
	if err := store.CreatePlan(ctx, plan1); err != nil {
		t.Fatalf("CreatePlan(plan1): %v", err)
	}

	// Link plan1 to session "sess-001".
	if err := store.LinkSession(ctx, plan1.ID, "sess-001"); err != nil {
		t.Fatalf("LinkSession: %v", err)
	}

	plans, err := store.GetPlansForSession(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetPlansForSession: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("GetPlansForSession after first link: got %d plans, want 1", len(plans))
	}
	if plans[0].ID != plan1.ID {
		t.Errorf("plan ID: got %q, want %q", plans[0].ID, plan1.ID)
	}

	// Link another plan to the same session.
	plan2 := newTestPlan("plan two")
	if err := store.CreatePlan(ctx, plan2); err != nil {
		t.Fatalf("CreatePlan(plan2): %v", err)
	}
	if err := store.LinkSession(ctx, plan2.ID, "sess-001"); err != nil {
		t.Fatalf("LinkSession(plan2): %v", err)
	}

	plans, err = store.GetPlansForSession(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetPlansForSession: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("GetPlansForSession after second link: got %d plans, want 2", len(plans))
	}

	// Unlink plan1.
	if err := store.UnlinkSession(ctx, plan1.ID, "sess-001"); err != nil {
		t.Fatalf("UnlinkSession: %v", err)
	}

	plans, err = store.GetPlansForSession(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetPlansForSession after unlink: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("GetPlansForSession after unlink: got %d plans, want 1", len(plans))
	}
	if plans[0].ID != plan2.ID {
		t.Errorf("remaining plan ID: got %q, want %q", plans[0].ID, plan2.ID)
	}
}

func TestSignoffs(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	planA := newTestPlan("plan A")
	if err := store.CreatePlan(ctx, planA); err != nil {
		t.Fatalf("CreatePlan(A): %v", err)
	}
	planB := newTestPlan("plan B")
	if err := store.CreatePlan(ctx, planB); err != nil {
		t.Fatalf("CreatePlan(B): %v", err)
	}

	// Create an approve signoff on planA.
	approve := NewPlanSignoff(planA.ID, "", "sess-001", "alice", "approved", "looks good")
	if err := store.CreateSignoff(ctx, approve); err != nil {
		t.Fatalf("CreateSignoff(approve): %v", err)
	}

	// Create a reject signoff on planB.
	reject := NewPlanSignoff(planB.ID, "", "sess-002", "bob", "rejected", "needs work")
	if err := store.CreateSignoff(ctx, reject); err != nil {
		t.Fatalf("CreateSignoff(reject): %v", err)
	}

	// Verify planA has exactly one signoff with action "approved".
	signoffsA, err := store.GetSignoffs(ctx, planA.ID)
	if err != nil {
		t.Fatalf("GetSignoffs(A): %v", err)
	}
	if len(signoffsA) != 1 {
		t.Fatalf("plan A signoffs: got %d, want 1", len(signoffsA))
	}
	if signoffsA[0].Action != "approved" {
		t.Errorf("plan A signoff action: got %q, want %q", signoffsA[0].Action, "approved")
	}
	if signoffsA[0].By != "alice" {
		t.Errorf("plan A signoff by: got %q, want %q", signoffsA[0].By, "alice")
	}
	if signoffsA[0].Comment != "looks good" {
		t.Errorf("plan A signoff comment: got %q, want %q", signoffsA[0].Comment, "looks good")
	}

	// Verify planB has exactly one signoff with action "rejected".
	signoffsB, err := store.GetSignoffs(ctx, planB.ID)
	if err != nil {
		t.Fatalf("GetSignoffs(B): %v", err)
	}
	if len(signoffsB) != 1 {
		t.Fatalf("plan B signoffs: got %d, want 1", len(signoffsB))
	}
	if signoffsB[0].Action != "rejected" {
		t.Errorf("plan B signoff action: got %q, want %q", signoffsB[0].Action, "rejected")
	}
	if signoffsB[0].By != "bob" {
		t.Errorf("plan B signoff by: got %q, want %q", signoffsB[0].By, "bob")
	}
}

func TestRevisionCount(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	p := newTestPlan("revision test")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Create 3 "revision_requested" signoffs.
	for i := 0; i < 3; i++ {
		so := NewPlanSignoff(p.ID, "", "sess-001", "reviewer", "revision_requested", "fix stuff")
		if err := store.CreateSignoff(ctx, so); err != nil {
			t.Fatalf("CreateSignoff(%d): %v", i, err)
		}
	}

	count, err := store.GetRevisionCount(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetRevisionCount: %v", err)
	}
	if count != 3 {
		t.Errorf("revision count: got %d, want 3", count)
	}

	// Add an "approved" signoff — revision count should still be 3.
	approve := NewPlanSignoff(p.ID, "", "sess-001", "reviewer", "approved", "ok now")
	if err := store.CreateSignoff(ctx, approve); err != nil {
		t.Fatalf("CreateSignoff(approved): %v", err)
	}

	count, err = store.GetRevisionCount(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetRevisionCount after approve: %v", err)
	}
	if count != 3 {
		t.Errorf("revision count after adding approved: got %d, want 3", count)
	}
}

func TestCountBySessionAndState(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	// Create 3 plans linked to the same session with different states.
	states := []PlanState{StateExecuting, StateCompleted, StatePendingApproval}
	plans := make([]*Plan, 3)

	for i, state := range states {
		p := newTestPlan("plan " + string(state))
		if err := store.CreatePlan(ctx, p); err != nil {
			t.Fatalf("CreatePlan(%s): %v", state, err)
		}
		if err := store.SetPlanState(ctx, p.ID, state); err != nil {
			t.Fatalf("SetPlanState(%s): %v", state, err)
		}
		if err := store.LinkSession(ctx, p.ID, "sess-shared"); err != nil {
			t.Fatalf("LinkSession(%s): %v", state, err)
		}
		plans[i] = p
	}

	counts, err := store.CountPlansBySessionAndState(ctx, "sess-shared")
	if err != nil {
		t.Fatalf("CountPlansBySessionAndState: %v", err)
	}

	// Verify each state has exactly 1 plan.
	for _, state := range states {
		if counts[state] != 1 {
			t.Errorf("counts[%s]: got %d, want 1", state, counts[state])
		}
	}

	// Total should be exactly 3 states.
	if len(counts) != 3 {
		t.Errorf("number of distinct states: got %d, want 3", len(counts))
	}
}

func TestDeletePlan(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	p := newTestPlan("delete test")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Add a phase.
	phase := NewPlanPhase(p.ID, "Phase 1", 1, 2)
	if err := store.CreatePhase(ctx, phase); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}

	// Link to a session.
	if err := store.LinkSession(ctx, p.ID, "sess-del"); err != nil {
		t.Fatalf("LinkSession: %v", err)
	}

	// Add a signoff.
	so := NewPlanSignoff(p.ID, phase.ID, "sess-del", "alice", "approved", "ok")
	if err := store.CreateSignoff(ctx, so); err != nil {
		t.Fatalf("CreateSignoff: %v", err)
	}

	// Delete the plan.
	if err := store.DeletePlan(ctx, p.ID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	// Verify GetPlan returns ErrPlanNotFound.
	_, err := store.GetPlan(ctx, p.ID)
	if err != ErrPlanNotFound {
		t.Errorf("GetPlan after delete: got err=%v, want ErrPlanNotFound", err)
	}

	// Verify phases are gone (cascade).
	phases, err := store.GetPhases(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPhases after delete: %v", err)
	}
	if len(phases) != 0 {
		t.Errorf("phases after delete: got %d, want 0", len(phases))
	}

	// Verify session links are gone (cascade).
	sessionPlans, err := store.GetPlansForSession(ctx, "sess-del")
	if err != nil {
		t.Fatalf("GetPlansForSession after delete: %v", err)
	}
	if len(sessionPlans) != 0 {
		t.Errorf("session plans after delete: got %d, want 0", len(sessionPlans))
	}

	// Verify signoffs are gone (cascade).
	signoffs, err := store.GetSignoffs(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetSignoffs after delete: %v", err)
	}
	if len(signoffs) != 0 {
		t.Errorf("signoffs after delete: got %d, want 0", len(signoffs))
	}

	// Deleting non-existent plan should return ErrPlanNotFound.
	err = store.DeletePlan(ctx, "nonexistent")
	if err != ErrPlanNotFound {
		t.Errorf("DeletePlan(nonexistent): got err=%v, want ErrPlanNotFound", err)
	}
}
