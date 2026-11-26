package agent

// Dispatcher quick_plan plan-contract pins (tree
// 20261125-plan-contract-quickplan leaf 01 + 03 call site; master C-2/C-3;
// decisions D1/D2/D4; closes bughunt 2026-11-24 M11/H6).
//
// X2 shape pinned end-to-end at the dispatcher seam: ONE task per quick_plan
// dispatch (the strategic planner task), adopted by the plan row; the zombie
// pending_approval row and the unlinked orphan task are BOTH gone on the
// false branch; approval_requested=true parks the row and creates NO task.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// newQuickPlanContractDispatcher wires a real plan store (config
// independent) onto the capture dispatcher; returns the dispatcher, the
// plan store, and a task-count helper over the dispatcher's task store.
func newQuickPlanContractDispatcher(t *testing.T, requireApproval bool) (*Dispatcher, *plan.SQLiteStore, func() int) {
	t.Helper()
	logger := digestTestLogger()
	cs := newCaptureServer(t, `{}`)
	d := newDigestCaptureDispatcher(t, cs)

	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.PlansConfig{Mode: "always"}
	cfg.Approval.RequireApproval = requireApproval
	pm := plan.NewPlanManager(store, nil, cfg, nil, logger)
	// The dispatcher adopts the PLANNER task itself; the plan machine's
	// synthesis creator is never invoked on the X2 path. A counting adapter
	// over the dispatcher's task store doubles as the CreateTask oracle:
	// any plan-side synthesis would show up as EXTRA task rows.
	d.SetPlanManager(pm)

	countTasks := func() int {
		all, err := d.taskStore.List(nil, 1000)
		if err != nil {
			t.Fatalf("task store list: %v", err)
		}
		return len(all)
	}
	return d, store, countTasks
}

// TestRouteToPlan_QuickPlanSingleTaskNoZombie pins the M11 closure on the
// false (default) branch: exactly ONE task exists, it is DispatchResult.Task,
// the plan row reaches executing bound to it (plan.TaskID == task.ID), no
// pending_approval zombie survives even with require_approval=true config
// (the intent, not the config, gates), Response stays empty for the async
// ack, and SuggestedMode is quick_plan.
func TestRouteToPlan_QuickPlanSingleTaskNoZombie(t *testing.T) {
	for _, requireApproval := range []bool{false, true} {
		d, pstore, countTasks := newQuickPlanContractDispatcher(t, requireApproval)
		ctx := context.Background()

		intent := &Intent{
			Type:             string(IntentQuickPlan),
			Confidence:       0.8,
			AgentType:        "orchestrator",
			Summary:          "ship the migration",
			RequiresPlanning: true,
		}
		res, err := d.routeToPlan(ctx, "ship the migration end to end without check-ins", intent, "sess-contract-qp")
		if err != nil {
			t.Fatalf("routeToPlan(quickplan): %v", err)
		}
		if res.Task == nil {
			t.Fatal("quick_plan produced no task; async gate dead")
		}
		// ONE task only: the pre-route createTask is the sole task on this
		// turn (no plan-synthesis parent/child hierarchy, no orphan twin).
		if n := countTasks(); n != 1 {
			t.Errorf("task rows = %d, want exactly 1 (require_approval=%v)", n, requireApproval)
		}
		got, err := pstore.GetPlan(ctx, res.Plan.ID)
		if err != nil {
			t.Fatalf("GetPlan: %v", err)
		}
		if got.State != plan.StateExecuting {
			t.Errorf("plan state = %q, want %q (require_approval=%v)", got.State, plan.StateExecuting, requireApproval)
		}
		if got.TaskID != res.Task.ID {
			t.Errorf("plan.TaskID = %q, want adopted task %q", got.TaskID, res.Task.ID)
		}
		// No zombie pending_approval row anywhere.
		parked, err := pstore.ListPlansByState(ctx, plan.StatePendingApproval, 100)
		if err != nil {
			t.Fatalf("ListPlansByState: %v", err)
		}
		for _, z := range parked {
			t.Errorf("zombie pending_approval row: plan %s", z.ID)
		}
		if res.Response != "" {
			t.Errorf("Response = %q, want empty (async ack carries the plan)", res.Response)
		}
		if res.SuggestedMode != IntentQuickPlan.SuggestedMode() {
			t.Errorf("SuggestedMode = %q, want %q", res.SuggestedMode, IntentQuickPlan.SuggestedMode())
		}
	}
}

// TestRouteToPlan_QuickPlanAdoptedTaskRollsup pins D2 at the dispatcher
// seam: completing the dispatched task rolls the plan row to completed.
func TestRouteToPlan_QuickPlanAdoptedTaskRollsup(t *testing.T) {
	d, pstore, _ := newQuickPlanContractDispatcher(t, false)
	ctx := context.Background()

	intent := &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       0.8,
		AgentType:        "orchestrator",
		Summary:          "ship the migration",
		RequiresPlanning: true,
	}
	res, err := d.routeToPlan(ctx, "ship the migration end to end without check-ins", intent, "sess-contract-rollup")
	if err != nil {
		t.Fatalf("routeToPlan: %v", err)
	}
	if err := d.planManager.OnTaskCompleted(ctx, res.Task.ID, "completed"); err != nil {
		t.Fatalf("OnTaskCompleted: %v", err)
	}
	got, err := pstore.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StateCompleted {
		t.Errorf("plan state after task rollup = %q, want %q (D2)", got.State, plan.StateCompleted)
	}
}

// TestRouteToPlan_QuickPlanApprovalRequestedParks pins the C-3/D3 true
// posture: row parks at pending_approval, NO task is created, and the
// reply carries the legacy "plan created" text.
func TestRouteToPlan_QuickPlanApprovalRequestedParks(t *testing.T) {
	// require_approval=false config proves the park is INTENT-driven: the
	// auto-approve tail must not fire on the approval_requested branch.
	d, pstore, countTasks := newQuickPlanContractDispatcher(t, false)
	ctx := context.Background()

	intent := &Intent{
		Type:              string(IntentQuickPlan),
		Confidence:        0.8,
		AgentType:         "orchestrator",
		Summary:           "refactor then wait for my go-ahead",
		RequiresPlanning:  true,
		ApprovalRequested: true,
	}
	res, err := d.routeToPlan(ctx, "plan the refactoring and implement it only after I approve", intent, "sess-contract-approval")
	if err != nil {
		t.Fatalf("routeToPlan: %v", err)
	}
	if res.Task != nil {
		t.Error("approval_requested branch created a task; execution must wait for sign-off")
	}
	if n := countTasks(); n != 0 {
		t.Errorf("task rows = %d, want 0 (parked contract, nothing dispatched yet)", n)
	}
	got, err := pstore.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StatePendingApproval {
		t.Errorf("plan state = %q, want %q", got.State, plan.StatePendingApproval)
	}
	if res.Response == "" {
		t.Error("approval park lost the 'plan created' text reply")
	}
}

// TestRouteToPlan_QuickPlanOverridePreserved keeps the researcher-extract
// contract green through the rewritten branch: a client override naming an
// executor survives as the dispatch agent + AgentOverrideApplied flag.
func TestRouteToPlan_QuickPlanOverridePreserved(t *testing.T) {
	d, _, _ := newQuickPlanContractDispatcher(t, false)
	ctx := context.Background()

	intent := &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       0.8,
		AgentType:        config.AgentIDResearcher,
		Summary:          "extract the paper",
		RequiresPlanning: true,
	}
	res, err := d.routeToPlanWithOverride(ctx, "quickplan extract this paper and save notes", intent, "sess-contract-override", true)
	if err != nil {
		t.Fatalf("routeToPlanWithOverride: %v", err)
	}
	if res.AgentID != config.AgentIDResearcher {
		t.Errorf("AgentID = %q, want researcher (override discarded)", res.AgentID)
	}
	if !res.AgentOverrideApplied {
		t.Error("AgentOverrideApplied flag lost: RouteToAgent shortcut-skip will misroute")
	}
	if res.Task == nil {
		t.Fatal("override quickplan produced no task")
	}
	// The created task carries the overridden agent assignment.
	if res.Task.AssignedAgent != config.AgentIDResearcher {
		t.Errorf("task agent = %q, want researcher", res.Task.AssignedAgent)
	}
	_ = task.StatePending
}

// TestClassifyAndRoute_QuickPlanApprovalThreaded pins the C-3 seam end to
// end through the analyzer: a canned analysis with approval_requested true
// lands on Intent.ApprovalRequested AND routes to the park posture, while
// the default false lands on the executing posture.
func TestClassifyAndRoute_QuickPlanApprovalThreaded(t *testing.T) {
	logger := digestTestLogger()
	cs := newCaptureServer(t, `{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":true}`)
	d := newDigestCaptureDispatcher(t, cs)

	pstore, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = pstore.Close() })
	d.SetPlanManager(plan.NewPlanManager(pstore, nil, config.PlansConfig{Mode: "always"}, nil, logger))

	ctx := context.Background()
	res, err := d.ClassifyAndRoute(ctx, "plan the auth refactor and implement it only after I approve", "sess-thread-approval", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil || !res.Intent.ApprovalRequested {
		t.Fatalf("ApprovalRequested not threaded to the routed Intent: %+v", res.Intent)
	}
	if res.Task != nil {
		t.Error("approval-threaded quickplan created a task; must park")
	}
	if res.Plan == nil {
		t.Fatal("no plan row on the approval branch")
	}
	got, err := pstore.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StatePendingApproval {
		t.Errorf("plan state = %q, want %q", got.State, plan.StatePendingApproval)
	}
}
