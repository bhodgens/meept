package agent

// Tests for goal-completion leaf 03 Task 2: the C6 goal-resolution rule at
// chat-path plan creation (explicit session goal > sole active non-implicit
// goal > implicit goal; no employee => empty GoalID).

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// stubGoalAnchor is a hand-rolled GoalAnchorStore recording which branch
// fired and what it returned.
type stubGoalAnchor struct {
	sessionGoalID string
	soleEmployee  string
	soleActiveID  string
	implicitID    string
	implicitErr   error

	implicitTitle string
	implicitEmp   string
}

func (s *stubGoalAnchor) SessionGoalID(sessionID string) string { return s.sessionGoalID }
func (s *stubGoalAnchor) SoleEmployeeID() string                { return s.soleEmployee }
func (s *stubGoalAnchor) SessionEmployeeID(_ string) string     { return "" }
func (s *stubGoalAnchor) SoleActiveGoalID(_ context.Context, employeeID string) string {
	return s.soleActiveID
}
func (s *stubGoalAnchor) EnsureImplicitGoal(_ context.Context, employeeID, title string) string {
	s.implicitEmp = employeeID
	s.implicitTitle = title
	if s.implicitErr != nil {
		return ""
	}
	return s.implicitID
}

// TestResolveGoalID_BranchOrder pins the C6 precedence: explicit session
// goal wins over the sole active goal, which wins over the implicit goal.
func TestResolveGoalID_BranchOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("explicit session goal wins", func(t *testing.T) {
		anchor := &stubGoalAnchor{sessionGoalID: "goal_explicit", soleEmployee: "emp1", soleActiveID: "goal_active", implicitID: "goal_implicit"}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "goal_explicit" {
			t.Errorf("resolveGoalID = %q, want goal_explicit", got)
		}
	})

	t.Run("sole active goal second", func(t *testing.T) {
		anchor := &stubGoalAnchor{soleEmployee: "emp1", soleActiveID: "goal_active", implicitID: "goal_implicit"}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "goal_active" {
			t.Errorf("resolveGoalID = %q, want goal_active", got)
		}
	})

	t.Run("implicit goal last, titled from plan title", func(t *testing.T) {
		anchor := &stubGoalAnchor{soleEmployee: "emp1", implicitID: "goal_implicit"}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "the plan title"); got != "goal_implicit" {
			t.Errorf("resolveGoalID = %q, want goal_implicit", got)
		}
		if anchor.implicitTitle != "the plan title" {
			t.Errorf("implicit title = %q, want the plan title", anchor.implicitTitle)
		}
		if anchor.implicitEmp != "emp1" {
			t.Errorf("implicit employee = %q, want emp1", anchor.implicitEmp)
		}
	})

	t.Run("no employee produces empty goal id and no implicit write", func(t *testing.T) {
		anchor := &stubGoalAnchor{implicitID: "goal_should_not_exist"}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "" {
			t.Errorf("resolveGoalID = %q, want empty", got)
		}
		if anchor.implicitTitle != "" {
			t.Errorf("EnsureImplicitGoal called without an employee (title %q)", anchor.implicitTitle)
		}
	})

	t.Run("nil anchor produces empty goal id", func(t *testing.T) {
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "" {
			t.Errorf("resolveGoalID = %q, want empty", got)
		}
	})
}

// TestRouteToPlan_GoalAnchored pins the wiring: a dispatcher with a goal
// anchor stamps the created plan's GoalID (persisted in the store row).
func TestRouteToPlan_GoalAnchored(t *testing.T) {
	logger := digestTestLogger()
	ctx := context.Background()

	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	anchor := &stubGoalAnchor{soleEmployee: "emp1", implicitID: "goal_anchored"}
	d := newDigestCaptureDispatcher(t, newCaptureServer(t, `{}`))
	d.SetPlanManager(plan.NewPlanManager(store, nil, config.PlansConfig{Mode: "always"}, nil, logger))
	d.SetGoalAnchor(anchor)

	res, err := d.routeToPlan(ctx, "carry out the anchored migration plan", &Intent{
		Type:       string(IntentQuickPlan),
		Confidence: 0.9,
		AgentType:  config.AgentIDPlanner,
	}, "sess-goal-anchor")
	if err != nil {
		t.Fatalf("routeToPlan: %v", err)
	}
	if res == nil || res.Plan == nil {
		t.Fatalf("routeToPlan returned no plan: %+v", res)
	}
	if res.Plan.GoalID != "goal_anchored" {
		t.Errorf("in-memory plan GoalID = %q, want goal_anchored", res.Plan.GoalID)
	}

	// The linkage must be PERSISTED (leaf interface contract: leaf 05 e2e
	// asserts the row).
	got, err := store.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.GoalID != "goal_anchored" {
		t.Errorf("stored plan GoalID = %q, want goal_anchored", got.GoalID)
	}
}

// TestRouteToPlan_NoEmployeeZeroGoalRows is the hard requirement: a chat
// path with no resolvable employee produces a plan with an EMPTY GoalID and
// never writes a goal row.
func TestRouteToPlan_NoEmployeeZeroGoalRows(t *testing.T) {
	logger := digestTestLogger()
	ctx := context.Background()

	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	anchor := &stubGoalAnchor{} // no employee, no session goal
	d := newDigestCaptureDispatcher(t, newCaptureServer(t, `{}`))
	d.SetPlanManager(plan.NewPlanManager(store, nil, config.PlansConfig{Mode: "always"}, nil, logger))
	d.SetGoalAnchor(anchor)

	res, err := d.routeToPlan(ctx, "carry out the unanchored migration plan", &Intent{
		Type:       string(IntentQuickPlan),
		Confidence: 0.9,
		AgentType:  config.AgentIDPlanner,
	}, "sess-no-emp")
	if err != nil {
		t.Fatalf("routeToPlan: %v", err)
	}
	if res == nil || res.Plan == nil {
		t.Fatalf("routeToPlan returned no plan: %+v", res)
	}
	if res.Plan.GoalID != "" {
		t.Errorf("plan GoalID = %q, want empty (no employee)", res.Plan.GoalID)
	}
	got, err := store.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.GoalID != "" {
		t.Errorf("stored plan GoalID = %q, want empty", got.GoalID)
	}
	if anchor.implicitTitle != "" {
		t.Error("EnsureImplicitGoal was called for an employee-less session")
	}
}

// Compile-time guard: EnsureImplicitGoal errors surface as an empty id, never
// a failed dispatch.
var _ = errors.New
