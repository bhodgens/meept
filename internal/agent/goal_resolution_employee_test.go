package agent

// Tests for the session→employee binding precedence (goal-completion
// follow-up #5): resolution prefers the session's bound employee over the
// daemon's sole employee, keeping single-employee installs on the
// existing fallback.

import (
	"context"
	"testing"
)

// stubSessionEmployeeGoalAnchor records the sessionID each lookup receives.
type stubSessionEmployeeGoalAnchor struct {
	stubGoalAnchor
	sessionEmployee string
	lastSessionID   string
}

func (s *stubSessionEmployeeGoalAnchor) SessionEmployeeID(sessionID string) string {
	s.lastSessionID = sessionID
	return s.sessionEmployee
}

// TestResolveGoalID_SessionEmployeeBinding pins the new first choice for the
// employee identity: the session's bound employee, else the sole employee.
func TestResolveGoalID_SessionEmployeeBinding(t *testing.T) {
	ctx := context.Background()

	t.Run("session binding wins over sole employee", func(t *testing.T) {
		anchor := &stubSessionEmployeeGoalAnchor{
			stubGoalAnchor:  stubGoalAnchor{soleEmployee: "emp_sole", implicitID: "goal_implicit"},
			sessionEmployee: "emp_bound",
		}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "goal_implicit" {
			t.Errorf("resolveGoalID = %q, want goal_implicit", got)
		}
		if anchor.implicitEmp != "emp_bound" {
			t.Errorf("implicit employee = %q, want emp_bound (session binding, not sole)", anchor.implicitEmp)
		}
		if anchor.lastSessionID != "sess-1" {
			t.Errorf("SessionEmployeeID received session %q, want sess-1", anchor.lastSessionID)
		}
	})

	t.Run("unbound session falls back to sole employee", func(t *testing.T) {
		anchor := &stubSessionEmployeeGoalAnchor{
			stubGoalAnchor: stubGoalAnchor{soleEmployee: "emp_sole", implicitID: "goal_implicit"},
		}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "goal_implicit" {
			t.Errorf("resolveGoalID = %q, want goal_implicit", got)
		}
		if anchor.implicitEmp != "emp_sole" {
			t.Errorf("implicit employee = %q, want emp_sole (fallback)", anchor.implicitEmp)
		}
	})

	t.Run("binding and sole both empty keeps the plan goal-less", func(t *testing.T) {
		anchor := &stubSessionEmployeeGoalAnchor{
			stubGoalAnchor: stubGoalAnchor{implicitID: "goal_should_not_exist"},
		}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "" {
			t.Errorf("resolveGoalID = %q, want empty", got)
		}
		if anchor.implicitTitle != "" {
			t.Errorf("EnsureImplicitGoal called without an employee (title %q)", anchor.implicitTitle)
		}
	})

	t.Run("bound session anchors when sole employee is unavailable", func(t *testing.T) {
		// The multi-employee install case: sole is "" but the session is
		// bound — branches 2/3 must run against the bound employee.
		anchor := &stubSessionEmployeeGoalAnchor{
			stubGoalAnchor:  stubGoalAnchor{soleActiveID: "goal_active"},
			sessionEmployee: "emp_bound",
		}
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		d.SetGoalAnchor(anchor)

		if got := d.resolveGoalID(ctx, "sess-1", "plan title"); got != "goal_active" {
			t.Errorf("resolveGoalID = %q, want goal_active (bound employee branch 2)", got)
		}
	})
}
