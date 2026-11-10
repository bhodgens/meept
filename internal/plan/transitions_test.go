package plan

import (
	"context"
	"errors"
	"testing"
)

// T3 (leaf 03-plans-goals/04): independent machine==table proof. The gate
// file transitions_gate_test.go owns TestPlanTransitionTable's full pinning;
// this file adds the closure checks the migration depends on: every from-set
// any manager method can pass to transitionFrom is a subset of the table
// edges into its target (a caller may narrow, never widen), and the typed
// guard error is what every refusal carries.

func TestTransitionTableCallSiteSubsets(t *testing.T) {
	// The pinned from-sets at each manager call site, as written in
	// manager.go. If a migration ever widens a call-site set past the
	// table, this fails before the gate tests even run.
	callSites := []struct {
		name string
		from []PlanState
		to   PlanState
	}{
		{name: "SubmitPlan", from: []PlanState{StateDraft}, to: StatePendingApproval},
		{name: "ApprovePlan", from: []PlanState{StatePendingApproval}, to: StateApproved},
		{name: "RejectPlan", from: []PlanState{StatePendingApproval}, to: StateCancelled},
		{name: "RevisePlan", from: []PlanState{StatePendingApproval, StateCancelled, StateFailed}, to: StatePlanning},
		{name: "ConfirmPlan (derived)", from: planFromStatesFor(StateConfirmed), to: StateConfirmed},
		{name: "CancelPlan (derived)", from: planFromStatesFor(StateCancelled), to: StateCancelled},
		{name: "completePlan (derived)", from: planFromStatesFor(StateCompleted), to: StateCompleted},
	}
	for _, cs := range callSites {
		for _, s := range cs.from {
			if !planAllows(s, cs.to) {
				t.Errorf("%s: pinned/derived from-state %s has no table edge to %s — call sites may narrow, never widen",
					cs.name, s, cs.to)
			}
		}
	}
}

func TestTransitionRefusalsAreTyped(t *testing.T) {
	ctx := context.Background()
	mgr := setupTestManager(t)

	p := NewPlan("typed-refusal", "d", "", "", "")
	p.State = StateDraft
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Non-member edge -> ErrPlanNotModifiable, row untouched.
	err := mgr.transition(ctx, p.ID, StateCompleted)
	if !errors.Is(err, ErrPlanNotModifiable) {
		t.Fatalf("illegal edge error = %v, want ErrPlanNotModifiable", err)
	}
	got, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateDraft {
		t.Errorf("refused transition mutated the row: %s", got.State)
	}

	// Missing row: table-refusal via the read (GetPlan fails) — but the
	// error is still not a silent success.
	if err := mgr.transition(ctx, "no-such-plan", StateCancelled); err == nil {
		t.Error("transition on missing plan must error")
	}

	// ApprovePlan must not fire the synthesizing->approved reclaim row.
	sp := NewPlan("lease-stomp-guard", "d", "", "", "")
	sp.State = StateSynthesizing
	if err := mgr.store.CreatePlan(ctx, sp); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.ApprovePlan(ctx, sp.ID, "", "operator"); !errors.Is(err, ErrPlanNotModifiable) {
		t.Errorf("ApprovePlan on a synthesizing plan = %v, want typed refusal (reclaim edge belongs to the lease primitives)", err)
	}
	if got, err := mgr.store.GetPlan(ctx, sp.ID); err != nil || got.State != StateSynthesizing {
		t.Errorf("lease row disturbed: %v (err %v)", got.State, err)
	}
}
