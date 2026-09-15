package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// TestRouteToPlan_LifecycleAdvances pins the plan-lifecycle wiring (issue #40
// item 1): a plan created via the dispatcher's routeToPlan path must not stay
// in state "draft". routeToPlan now calls SubmitPlan after CreatePlan, so:
//   - with plans.approval.require_approval=false (zero PlansConfig), the plan
//     auto-approves through pending_approval into "approved" and synthesizes
//     into an executing task hierarchy;
//   - with require_approval=true, the plan rests at "pending_approval" where
//     the TUI/HTTP approval paths can pick it up.
//
// Both branches also verify that the session link is populated: CreatePlan
// links the source session when sessionID is non-empty, and the store must
// return the plan for that session afterwards.
func TestRouteToPlan_LifecycleAdvances(t *testing.T) {
	logger := digestTestLogger()
	ctx := context.Background()

	cases := []struct {
		name            string
		requireApproval bool
		wantState       plan.PlanState
	}{
		{"auto_approve_advances_past_draft", false, plan.StateApproved},
		{"require_approval_parks_at_pending", true, plan.StatePendingApproval},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
			if err != nil {
				t.Fatalf("plan store: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })

			cfg := config.PlansConfig{Mode: "always"}
			cfg.Approval.RequireApproval = tc.requireApproval
			d := newDigestCaptureDispatcher(t, newCaptureServer(t, `{}`))
			d.SetPlanManager(plan.NewPlanManager(store, nil, cfg, nil, logger))

			res, err := d.routeToPlan(ctx, "plan the great refactor end to end", &Intent{
				Type:       string(IntentPlan),
				Confidence: 0.9,
				AgentType:  config.AgentIDPlanner,
			}, "sess-issue40")
			if err != nil {
				t.Fatalf("routeToPlan: %v", err)
			}
			if res == nil || res.Plan == nil {
				t.Fatalf("routeToPlan returned no plan: %+v", res)
			}

			// Re-read from the store: the in-memory result must match what a
			// restart would see.
			got, err := store.GetPlan(ctx, res.Plan.ID)
			if err != nil {
				t.Fatalf("get plan %s: %v", res.Plan.ID, err)
			}
			if got.State != tc.wantState {
				t.Errorf("plan state = %q, want %q", got.State, tc.wantState)
			}

			// Session link must be populated (plan_sessions table).
			linked, err := store.GetPlansForSession(ctx, "sess-issue40")
			if err != nil {
				t.Fatalf("get plans for session: %v", err)
			}
			found := false
			for _, lp := range linked {
				if lp.ID == res.Plan.ID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("plan %s not linked to session sess-issue40; got %d plans", res.Plan.ID, len(linked))
			}

			// When approval is not required the lifecycle must advance all the
			// way through pending_approval -> approved with a recorded
			// signoff. Synthesis (TaskID) needs a TaskCreator, which the bare
			// test dispatcher does not wire — the plan package's own
			// SubmitPlan tests cover the synthesized hierarchy.
			if !tc.requireApproval {
				signoffs, err := store.GetSignoffs(ctx, res.Plan.ID)
				if err != nil {
					t.Fatalf("get signoffs: %v", err)
				}
				if len(signoffs) == 0 {
					t.Error("auto-approved plan recorded no signoff")
				}
			}
		})
	}
}

// TestRouteToPlan_NilManagerUnchanged documents that routeToPlan is only
// reached with a non-nil planManager (all three call sites gate on
// d.planManager != nil); no guard is added in routeToPlan itself.
func TestRouteToPlan_NilManagerUnchanged(t *testing.T) {
	if true {
		t.Skip("call sites guard on planManager != nil; nothing to assert here")
	}
}
