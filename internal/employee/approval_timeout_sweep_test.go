package employee

// Round-2 bughunt pins (parent-verified at HEAD 16b8a81a):
//
//   - Fix 1 (approval-timeout sweep reads the wrong plan): the sweep used
//     to key off ActivePlanID alone — but ActivePlanIDs[0] can be an
//     already-EXECUTING plan (tier-2 appends pending plans after the
//     executing one), so the real pending_approval plan's timeout never
//     fired while RejectPlan's pending_approval->cancelled CAS harmlessly
//     failed on the executing plan. The sweep now walks the full
//     ActivePlanIDs list and filters to plans actually in pending_approval
//     via the PlanStateLookup seam (same adapter pattern as
//     PlanApprovedLister from 59045fa5).
//
//   - Fix 2 (pointer clobber): AddActivePlan/RemoveActivePlan used to set
//     ActivePlanID = ActivePlanIDs[0] unconditionally, so a later
//     AddActivePlan overwrote the executing-plan pointer SetActivePlan had
//     recorded. AddActivePlan now sets the pointer only when empty;
//     RemoveActivePlan re-points only when the removed id WAS the pointer.
//
//   - Fix 3 chose the DOC fix (ordering contract on SetPlanApprovedLister:
//     write-once before StartApprovedPlanResumeSweep, read-only after;
//     production wires sequentially) over locking — smallest sound option.
//
//   - Fix 4 corrected the sweep's false "will retry next pass" Warn: the
//     once-map (written before execution) makes every retry an
//     already-resumed skip within this process; only a daemon restart
//     retries (the plan stays approved, the fresh once-map is empty).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// stubSweepDisposer records RejectPlan calls (and approves nothing).
type stubSweepDisposer struct {
	mu       sync.Mutex
	rejected []string
}

func (d *stubSweepDisposer) ApprovePlan(_ context.Context, _, _, _ string) error {
	return nil
}

func (d *stubSweepDisposer) RejectPlan(_ context.Context, planID, _, _, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rejected = append(d.rejected, planID)
	return nil
}

func (d *stubSweepDisposer) rejectedPlans() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.rejected...)
}

// stubPlanCreatedAt returns a fixed creation timestamp for every plan.
type stubPlanCreatedAt struct{ t time.Time }

func (s stubPlanCreatedAt) PlanCreatedAt(_ context.Context, _ string) (time.Time, error) {
	return s.t, nil
}

// stubPlanStates resolves planID -> signoff state (PlanStateLookup seam).
type stubPlanStates struct {
	mu     sync.Mutex
	states map[string]string
	errs   map[string]error
	calls  int
}

func (s *stubPlanStates) PlanState(_ context.Context, planID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if err, ok := s.errs[planID]; ok {
		return "", err
	}
	if st, ok := s.states[planID]; ok {
		return st, nil
	}
	return "", errors.New("plan unknown to stub")
}

// seedTwoPlanGoal creates a goal for the resume fixture employee carrying
// [executingA, pendingB] in ActivePlanIDs with A as the executing pointer —
// the exact tier-2 shape (pending plan appended after the executing one).
func seedTwoPlanGoal(t *testing.T, m *Manager, gs *GoalStore, executingA, pendingB string) *Goal {
	t.Helper()
	ctx := context.Background()
	goal, err := m.CreateGoal(ctx, GoalSpec{
		EmployeeID: resumeEmployeeID,
		Title:      "keep the approval-timeout fixture green",
		Mandate:    "pursue the approval-timeout fixture mandate to completion",
		Source:     "user",
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	goal.SetActivePlan(executingA)
	goal.AddActivePlan(pendingB)
	if err := gs.Update(ctx, goal); err != nil {
		t.Fatalf("seed two-plan goal: %v", err)
	}
	return goal
}

// TestApprovalTimeoutSweep_TimesOutPendingPlanLeavesExecutingAlone is the
// fix-1 pin: a goal with [executingA, pendingB] times out B and leaves A
// alone. The pre-fix sweep keyed off ActivePlanID (= A, the executing
// pointer): it "rejected" A (harmless CAS failure in the plan system) and
// B's approval timeout NEVER fired — B sat pending forever.
func TestApprovalTimeoutSweep_TimesOutPendingPlanLeavesExecutingAlone(t *testing.T) {
	m, gs, _ := newResumeTestManager(t)
	const executingA = "plan-exec-a"
	const pendingB = "plan-pend-b"
	goal := seedTwoPlanGoal(t, m, gs, executingA, pendingB)

	disposer := &stubSweepDisposer{}
	m.SetPlanDisposer(disposer)
	// Both plans are older than the timeout; only the STATE distinguishes
	// them — exactly the shape the pre-fix sweep got wrong.
	old := time.Now().UTC().Add(-time.Hour)
	m.SetPlanLookup(stubPlanCreatedAt{t: old})
	states := &stubPlanStates{states: map[string]string{
		executingA: "executing",
		pendingB:   "pending_approval",
	}}
	m.SetPlanStateLookup(states)

	m.runApprovalTimeoutSweep(context.Background(), time.Nanosecond)

	rejected := disposer.rejectedPlans()
	if len(rejected) != 1 || rejected[0] != pendingB {
		t.Errorf("rejected = %v, want exactly [%s] (executing plan %s must be left alone; pending plan's timeout must fire)",
			rejected, pendingB, executingA)
	}
	if states.calls == 0 {
		t.Error("PlanStateLookup never consulted (sweep did not filter by state)")
	}

	// B was rejected, so it left ActivePlanIDs; A stays with the pointer.
	// Re-read from the store: RejectPlan mutates its own copy of the goal.
	fresh, err := gs.Get(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("re-read goal: %v", err)
	}
	if got := fresh.ActivePlan(); got != executingA {
		t.Errorf("ActivePlanID = %q, want %q (executing pointer untouched)", got, executingA)
	}
	ids := fresh.ActivePlans()
	if len(ids) != 1 || ids[0] != executingA {
		t.Errorf("ActivePlans = %v, want [%s] only", ids, executingA)
	}
}

// TestApprovalTimeoutSweep_StateLookupErrorSkips: a plan whose state cannot
// be resolved (unknown/purged) is treated as not-pending and skipped — the
// sweep must never reject on a guess.
func TestApprovalTimeoutSweep_StateLookupErrorSkips(t *testing.T) {
	m, gs, _ := newResumeTestManager(t)
	const pendingB = "plan-pend-err"
	seedTwoPlanGoal(t, m, gs, "plan-exec-x", pendingB)

	disposer := &stubSweepDisposer{}
	m.SetPlanDisposer(disposer)
	m.SetPlanLookup(stubPlanCreatedAt{t: time.Now().UTC().Add(-time.Hour)})
	states := &stubPlanStates{errs: map[string]error{
		pendingB: errors.New("plan purged"),
	}}
	m.SetPlanStateLookup(states)

	m.runApprovalTimeoutSweep(context.Background(), time.Nanosecond)

	if got := disposer.rejectedPlans(); len(got) != 0 {
		t.Errorf("rejected = %v, want none (state lookup error must skip, not reject)", got)
	}
}

// TestApprovalTimeoutSweep_NilStateLookupRejectsLegacyPointer: without a
// wired PlanStateLookup the sweep keeps the legacy ActivePlanID-only
// behavior (documented pre-seam contract) so callers that never wired the
// accessor see no behavior change.
func TestApprovalTimeoutSweep_NilStateLookupRejectsLegacyPointer(t *testing.T) {
	m, gs, _ := newResumeTestManager(t)
	const solo = "plan-solo"
	ctx := context.Background()
	goal, err := m.CreateGoal(ctx, GoalSpec{
		EmployeeID: resumeEmployeeID,
		Title:      "legacy pointer sweep",
		Mandate:    "pursue the legacy pointer mandate to completion",
		Source:     "user",
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	goal.SetActivePlan(solo)
	if err := gs.Update(ctx, goal); err != nil {
		t.Fatalf("seed goal: %v", err)
	}

	disposer := &stubSweepDisposer{}
	m.SetPlanDisposer(disposer)
	m.SetPlanLookup(stubPlanCreatedAt{t: time.Now().UTC().Add(-time.Hour)})

	m.runApprovalTimeoutSweep(ctx, time.Nanosecond)

	if got := disposer.rejectedPlans(); len(got) != 1 || got[0] != solo {
		t.Errorf("rejected = %v, want [%s] (legacy no-seam behavior preserved)", got, solo)
	}
}

// TestGoal_AddActivePlan_KeepsExecutingPointer is the fix-2 pin:
// SetActivePlan(A) then AddActivePlan(B) keeps A as the executing pointer —
// the pre-fix code re-pointed to ActivePlanIDs[0] on every add, clobbering
// the pointer the approval-timeout sweep and resume path key off.
func TestGoal_AddActivePlan_KeepsExecutingPointer(t *testing.T) {
	g := &Goal{ID: "goal-clobber"}
	g.SetActivePlan("plan-exec-a")
	g.AddActivePlan("plan-pend-b")

	if got := g.ActivePlanID; got != "plan-exec-a" {
		t.Errorf("ActivePlanID = %q, want plan-exec-a (AddActivePlan clobbered the executing pointer)", got)
	}
	ids := g.ActivePlans()
	if len(ids) != 2 || ids[0] != "plan-exec-a" || ids[1] != "plan-pend-b" {
		t.Errorf("ActivePlans = %v, want [plan-exec-a plan-pend-b]", ids)
	}

	// First-add path still sets the pointer when it was empty.
	g2 := &Goal{ID: "goal-first"}
	if n := g2.AddActivePlan("plan-first"); n != 1 {
		t.Fatalf("first add count = %d, want 1", n)
	}
	if got := g2.ActivePlanID; got != "plan-first" {
		t.Errorf("first add: ActivePlanID = %q, want plan-first", got)
	}
}

// TestGoal_RemoveActivePlan_RepPointsOnlyWhenPointerRemoved: removing the
// PENDING plan never moves the executing pointer; removing the pointer
// itself falls back to the first remaining entry (or empty).
func TestGoal_RemoveActivePlan_RepPointsOnlyWhenPointerRemoved(t *testing.T) {
	g := &Goal{ID: "goal-repoint"}
	g.SetActivePlan("plan-exec-a")
	g.AddActivePlan("plan-pend-b")

	// Removing the pending plan leaves the pointer alone.
	if n := g.RemoveActivePlan("plan-pend-b"); n != 1 {
		t.Fatalf("remove pending count = %d, want 1", n)
	}
	if got := g.ActivePlanID; got != "plan-exec-a" {
		t.Errorf("after removing pending: ActivePlanID = %q, want plan-exec-a (pointer must not move)", got)
	}

	// Removing the pointer re-points to the first remaining entry.
	g.AddActivePlan("plan-pend-c")
	if n := g.RemoveActivePlan("plan-exec-a"); n != 1 {
		t.Fatalf("remove pointer count = %d, want 1", n)
	}
	if got := g.ActivePlanID; got != "plan-pend-c" {
		t.Errorf("after removing pointer: ActivePlanID = %q, want plan-pend-c (fallback to remaining entry)", got)
	}

	// Removing the last entry empties the pointer.
	g.RemoveActivePlan("plan-pend-c")
	if got := g.ActivePlanID; got != "" {
		t.Errorf("after removing all: ActivePlanID = %q, want empty", got)
	}
}
