package daemon

// Tests for the session→employee binding resolution (goal-completion
// follow-up #5): the adapter prefers a session's explicitly bound employee
// over the daemon's sole employee, keeping single-employee installs on the
// existing SoleEmployeeID fallback.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// newAdapterTestSessionStore builds the session SQLite store the adapter
// reads bindings from.
func newAdapterTestSessionStore(t *testing.T) *session.SQLiteStore {
	t.Helper()
	store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), nil)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestGoalAnchorAdapter_SessionEmployeeIDPrefersBinding pins the new
// precedence: a session-bound employee wins over the roster regardless of
// roster cardinality; unbound sessions fall through to sole-employee.
func TestGoalAnchorAdapter_SessionEmployeeIDPrefersBinding(t *testing.T) {
	_, goals := newAdapterTestGoalStore(t)
	store := newAdapterTestSessionStore(t)

	sess, err := store.Create("bound-session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Roster with TWO employees: sole-employee would return "", but the
	// binding must win.
	a := newGoalAnchorAdapter(store, goals, fakeRoster{ids: []string{"emp1", "emp2"}}, nil)
	if err := store.SetEmployeeID(sess.ID, "emp2"); err != nil {
		t.Fatalf("SetEmployeeID: %v", err)
	}
	if got := a.SoleEmployeeID(); got != "" {
		t.Fatalf("precondition: sole employee with two on the roster = %q, want empty", got)
	}
	if got := a.SessionEmployeeID(sess.ID); got != "emp2" {
		t.Errorf("SessionEmployeeID = %q, want emp2 (binding beats roster)", got)
	}

	// Unbound session, one employee: falls through to the sole employee.
	a = newGoalAnchorAdapter(store, goals, fakeRoster{ids: []string{"emp1"}}, nil)
	if got := a.SessionEmployeeID("sess-unbound"); got != "emp1" {
		t.Errorf("unbound session SessionEmployeeID = %q, want sole employee emp1", got)
	}

	// Unbound session, two employees: no reliable answer => "".
	a = newGoalAnchorAdapter(store, goals, fakeRoster{ids: []string{"emp1", "emp2"}}, nil)
	if got := a.SessionEmployeeID("sess-unbound"); got != "" {
		t.Errorf("unbound session with two employees = %q, want empty", got)
	}

	// Unbound session, zero employees: "".
	a = newGoalAnchorAdapter(store, goals, fakeRoster{}, nil)
	if got := a.SessionEmployeeID("sess-unbound"); got != "" {
		t.Errorf("unbound session with zero employees = %q, want empty", got)
	}
}

// TestGoalAnchorAdapter_SessionEmployeeIDDegradations pins the inert paths:
// nil sessions store, empty session id, and a dangling binding.
func TestGoalAnchorAdapter_SessionEmployeeIDDegradations(t *testing.T) {
	_, goals := newAdapterTestGoalStore(t)

	// Nil sessions reader: sole-employee fallback still works.
	a := newGoalAnchorAdapter(nil, goals, fakeRoster{ids: []string{"emp1"}}, nil)
	if got := a.SessionEmployeeID("sess-x"); got != "emp1" {
		t.Errorf("nil sessions reader => %q, want sole employee emp1", got)
	}

	store := newAdapterTestSessionStore(t)
	a = newGoalAnchorAdapter(store, goals, fakeRoster{ids: []string{"emp1"}}, nil)
	if got := a.SessionEmployeeID(""); got != "emp1" {
		t.Errorf("empty session id => %q, want sole employee emp1", got)
	}

	// A dangling binding (employee no longer on the roster) must not
	// attribute work to a retired employee.
	sess, err := store.Create("dangling-session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SetEmployeeID(sess.ID, "emp-retired"); err != nil {
		t.Fatalf("SetEmployeeID: %v", err)
	}
	a = newGoalAnchorAdapter(store, goals, fakeRoster{ids: []string{"emp1"}}, nil)
	if got := a.SessionEmployeeID(sess.ID); got != "" {
		t.Errorf("dangling binding => %q, want empty (never attribute to a retired employee)", got)
	}
}

// TestGoalAnchorAdapter_BoundEmployeeAnchorsImplicitGoal is the end-to-end
// adapter behavior: EnsureImplicitGoal through a session binding with TWO
// employees on the roster (previously impossible — the plan stayed goal-less).
func TestGoalAnchorAdapter_BoundEmployeeAnchorsImplicitGoal(t *testing.T) {
	ctx := context.Background()
	botStore, goals := newAdapterTestGoalStore(t)
	seedEmployee(t, botStore, "emp1")
	seedEmployee(t, botStore, "emp2")
	store := newAdapterTestSessionStore(t)

	sess, err := store.Create("anchored-session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SetEmployeeID(sess.ID, "emp2"); err != nil {
		t.Fatalf("SetEmployeeID: %v", err)
	}

	a := newGoalAnchorAdapter(store, goals, fakeRoster{ids: []string{"emp1", "emp2"}}, nil)
	empID := a.SessionEmployeeID(sess.ID)
	if empID != "emp2" {
		t.Fatalf("SessionEmployeeID = %q, want emp2", empID)
	}
	if got := a.EnsureImplicitGoal(ctx, empID, "plan title"); got == "" {
		t.Fatal("EnsureImplicitGoal returned empty for the bound employee")
	}
}
