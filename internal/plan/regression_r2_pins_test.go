package plan

// Regression-review R2 wave pin test (2026-11-11).
//
//   R2-5: CompletePlanningRevision had ZERO production callers — revised
//         plans (RevisePlan -> planning) stranded in planning forever:
//         SubmitPlan required draft, ApprovePlan required pending_approval,
//         and retention pruning does not promptly touch planning rows.
//         SubmitPlan now accepts planning-state plans by transitioning
//         planning->draft first via the guarded table (the CompletePlanningRevision
//         writer), so a revised plan can reach pending_approval again.

import (
	"testing"
)

// TestSubmitPlanRescuesPlanningStatePlan pins R2-5: submit directly on a
// planning-state (revised) plan succeeds — the plan reaches pending_approval
// without any caller-side CompletePlanningRevision invocation and WITHOUT
// being retention-pruned.
func TestSubmitPlanRescuesPlanningStatePlan(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p, err := mgr.CreatePlan(ctx, "r2-5-rescue", "draft body", "", "", "sess-r25")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan (initial): %v", err)
	}
	// Revision sends it back to planning (the strand state).
	if err := mgr.RevisePlan(ctx, p.ID, "sess-r25", "rework requested"); err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	got, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after revise: %v", err)
	}
	if got.State != StatePlanning {
		t.Fatalf("state after revise = %s, want %s (fixture)", got.State, StatePlanning)
	}

	// THE PIN: SubmitPlan alone rescues the plan — planning -> draft ->
	// pending_approval through the guarded table, no retention pruning
	// involved.
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan on planning-state plan: %v (revised plans must not strand)", err)
	}
	got, err = mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after rescue submit: %v", err)
	}
	if got.State != StatePendingApproval {
		t.Errorf("state after rescue submit = %s, want %s", got.State, StatePendingApproval)
	}
}

// TestSubmitPlanNonPlanningBehaviorUnchanged pins the flip side: a
// pending_approval plan is still REFUSED by SubmitPlan (the rescue must not
// widen the accepted set beyond planning->draft->pending_approval), and a
// draft plan submits byte-identically.
func TestSubmitPlanNonPlanningBehaviorUnchanged(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p, err := mgr.CreatePlan(ctx, "r2-5-guard", "draft body", "", "", "sess-r25g")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	// Draft submits normally.
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan (draft): %v", err)
	}
	got, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StatePendingApproval {
		t.Fatalf("state after draft submit = %s, want %s", got.State, StatePendingApproval)
	}
	// pending_approval is still refused (table has no pending_approval ->
	// pending_approval edge; the rescue must not change that).
	if err := mgr.SubmitPlan(ctx, p.ID); err == nil {
		t.Error("SubmitPlan on pending_approval plan succeeded — the accepted set was widened beyond planning")
	}
}
