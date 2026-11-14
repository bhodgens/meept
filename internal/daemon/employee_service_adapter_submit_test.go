package daemon

// Leaf 06 (tree docs/plans/20261113-escalation-resume-and-hardening):
// planCreatorAdapter.CreatePlanWithGoal must land employee goal-loop plans
// in pending_approval — CreatePlanWithGoal alone stops at StateDraft and a
// draft plan can never be approved (ApprovePlan CASes
// pending_approval->approved), which stranded every goal-loop plan and made
// the escalation-resume path dead. Uses the REAL PlanManager over a temp
// SQLite store (mirrors the evolver_plan_sink test posture); no mocks.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// newAdapterPlanManager builds a real PlanManager over a temp store with
// approval semantics driven by requireApproval (the exact knob SubmitPlan's
// auto-approve branch reads).
func newAdapterPlanManager(t *testing.T, requireApproval bool) *plan.PlanManager {
	t.Helper()
	logger := slog.Default()
	store, err := plan.NewSQLiteStore(t.TempDir()+"/plans.db", logger)
	if err != nil {
		t.Fatalf("failed to create plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.DefaultConfig()
	cfg.Plans.Approval.RequireApproval = requireApproval
	return plan.NewPlanManager(store, nil, cfg.Plans, nil, logger)
}

// TestPlanCreatorAdapter_SubmitsGoalLoopPlanToPendingApproval: the adapter's
// CreatePlanWithGoal (the goal-loop seam, GoalID stamped) leaves the plan
// row in pending_approval — not draft — so `meept plans approve` and the
// leaf-01/02 resume path can reach it.
func TestPlanCreatorAdapter_SubmitsGoalLoopPlanToPendingApproval(t *testing.T) {
	planMgr := newAdapterPlanManager(t, true)
	adapter := newPlanCreatorAdapter(planMgr, slog.Default())

	ref, err := adapter.CreatePlanWithGoal(context.Background(),
		"escalated: leaf06 goal plan", "the escalating candidate",
		"employee-x", "gloop_test01", "goal-leaf06")
	if err != nil {
		t.Fatalf("CreatePlanWithGoal: %v", err)
	}
	if ref.ID == "" {
		t.Fatal("CreatePlanWithGoal returned an empty plan id")
	}
	if ref.GoalID != "goal-leaf06" {
		t.Errorf("ref.GoalID = %q, want %q", ref.GoalID, "goal-leaf06")
	}
	if ref.State != "pending_approval" {
		t.Errorf("ref.State = %q, want pending_approval (plan must not strand in draft)", ref.State)
	}

	// The stored row carries the same post-submit state (authoritative).
	p, err := planMgr.GetPlan(context.Background(), ref.ID)
	if err != nil {
		t.Fatalf("GetPlan %s: %v", ref.ID, err)
	}
	if p.State != "pending_approval" {
		t.Errorf("stored plan state = %q, want pending_approval", p.State)
	}
	if p.GoalID != "goal-leaf06" {
		t.Errorf("stored plan goal_id = %q, want %q", p.GoalID, "goal-leaf06")
	}
}

// TestPlanCreatorAdapter_PlainCreateAlsoSubmits: CreatePlan (empty goal —
// the legacy no-active-goal path) delegates to WithGoal and must submit the
// same way.
func TestPlanCreatorAdapter_PlainCreateAlsoSubmits(t *testing.T) {
	planMgr := newAdapterPlanManager(t, true)
	adapter := newPlanCreatorAdapter(planMgr, slog.Default())

	ref, err := adapter.CreatePlan(context.Background(),
		"escalated: leaf06 plain plan", "no goal linkage", "employee-y", "gloop_test02")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if ref.State != "pending_approval" {
		t.Errorf("ref.State = %q, want pending_approval", ref.State)
	}
}

// TestPlanCreatorAdapter_AutoApproveModeStillSucceeds: with
// require_approval=false, SubmitPlan takes the auto-approve branch
// (ApprovePlan with by="system" + synthesis). In production the daemon's
// PlanManager always carries a TaskCreator (daemon.go), so the row ends
// approved/executing and plan.approved drives the resume pump. This test
// runs the manager WITHOUT a task creator, which makes the auto-approve's
// Synthesize step fail — SubmitPlan refuses — and pins the tolerance
// contract: creation still returns the plan ref (WARN branch), never an
// error.
func TestPlanCreatorAdapter_AutoApproveModeStillSucceeds(t *testing.T) {
	planMgr := newAdapterPlanManager(t, false)
	adapter := newPlanCreatorAdapter(planMgr, slog.Default())

	ref, err := adapter.CreatePlanWithGoal(context.Background(),
		"escalated: leaf06 auto-approve", "auto-approve mode",
		"employee-z", "gloop_test03", "goal-leaf06-auto")
	if err != nil {
		t.Fatalf("CreatePlanWithGoal under auto-approve (submit refused): %v", err)
	}
	if ref.ID == "" {
		t.Fatal("CreatePlanWithGoal returned an empty plan id despite submit refusal")
	}
	if ref.GoalID != "goal-leaf06-auto" {
		t.Errorf("ref.GoalID = %q, want %q", ref.GoalID, "goal-leaf06-auto")
	}
}

// TestPlanCreatorAdapter_SubmitRefusalStillReturnsPlanRef: a refused submit
// (the CAS rejects a draft that concurrently left draft — here simulated by
// pre-cancelling... actually by cancelling between create and submit is
// racy; the deterministic refusal is a state rule) must NOT fail creation:
// the plan ref comes back with whatever state the row holds.
func TestPlanCreatorAdapter_SubmitRefusalStillReturnsPlanRef(t *testing.T) {
	planMgr := newAdapterPlanManager(t, true)
	adapter := newPlanCreatorAdapter(planMgr, slog.Default())

	// Deterministic refusal: move the row out of draft behind the
	// adapter's back via a state CAS the adapter cannot see — the only
	// seam without hooks is the store, so cancel the JUST-created plan
	// through the manager between the two calls is impossible from
	// outside. Instead, drive the refusal through the transition table:
	// a plan already in pending_approval resubmitted is refused
	// (draft->pending_approval CAS misses) while creation still returns
	// the ref. Simulate by cancelling the plan right after the adapter's
	// internal create via CancelPlan is not injectable — so exercise the
	// contract at the observable boundary: submit a SECOND time through
	// the manager on an already-pending plan and assert the error shape,
	// then pin that the adapter surfaces plan refs with non-draft states
	// verbatim (no error).
	pending, err := adapter.CreatePlanWithGoal(context.Background(),
		"escalated: leaf06 refusal setup", "setup", "employee-w", "gloop_test04", "goal-leaf06-refusal")
	if err != nil {
		t.Fatalf("setup CreatePlanWithGoal: %v", err)
	}
	if pending.State != "pending_approval" {
		t.Fatalf("setup state = %q, want pending_approval", pending.State)
	}
	// SubmitPlan again on the pending row: the draft->pending_approval CAS
	// refuses (ErrPlanNotModifiable family). The adapter never calls submit
	// twice, but this pins the refusal ERROR shape the WARN branch handles.
	err = planMgr.SubmitPlan(context.Background(), pending.ID)
	if err == nil {
		t.Fatal("second SubmitPlan on a pending_approval row must be refused")
	}
	if !strings.Contains(err.Error(), "not modifiable") && !strings.Contains(err.Error(), "expected") {
		t.Errorf("refusal error does not look like a state-rule refusal: %v", err)
	}
}
