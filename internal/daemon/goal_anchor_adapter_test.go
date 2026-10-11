package daemon

// Tests for goalAnchorAdapter (goal-completion leaf 03 Task 2): the daemon
// adapter that resolves agent.GoalAnchorStore against the employee goal
// store + live roster.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
)

type fakeRoster struct{ ids []string }

func (f fakeRoster) ListEmployeeIDs(context.Context) ([]string, error) { return f.ids, nil }

// newAdapterTestGoalStore builds the shared bot+goal SQLite store. The
// employee_goals FK references bot_definitions, so the bot store owns the
// schema and seeds the employee rows (production shared-DB parity; same
// shape as newGoalTestManager in internal/employee).
func newAdapterTestGoalStore(t *testing.T) (*bot.Store, *employee.GoalStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	goals, err := employee.NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { _ = goals.Close() })
	return botStore, goals
}

// seedEmployee inserts the FK parent bot_definitions row for id.
func seedEmployee(t *testing.T, botStore *bot.Store, id string) {
	t.Helper()
	err := botStore.Create(context.Background(), bot.BotDefinition{
		ID:      id,
		Name:    id,
		Prompt:  "test employee " + id,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed employee %s: %v", id, err)
	}
}

func TestGoalAnchorAdapter_SessionGoalIDNilReaderIsEmpty(t *testing.T) {
	_, goals := newAdapterTestGoalStore(t)
	a := newGoalAnchorAdapter(nil, goals, fakeRoster{ids: []string{"emp1"}}, nil)
	if got := a.SessionGoalID("sess-x"); got != "" {
		t.Errorf("nil sessions reader => %q, want empty", got)
	}
}

func TestGoalAnchorAdapter_SoleEmployee(t *testing.T) {
	a := newGoalAnchorAdapter(nil, nil, fakeRoster{ids: []string{"emp1"}}, nil)
	if got := a.SoleEmployeeID(); got != "emp1" {
		t.Errorf("SoleEmployeeID = %q, want emp1", got)
	}
	// Zero employees: empty.
	a = newGoalAnchorAdapter(nil, nil, fakeRoster{}, nil)
	if got := a.SoleEmployeeID(); got != "" {
		t.Errorf("zero employees => %q, want empty", got)
	}
	// Multiple employees: empty (no per-session binding to disambiguate).
	a = newGoalAnchorAdapter(nil, nil, fakeRoster{ids: []string{"emp1", "emp2"}}, nil)
	if got := a.SoleEmployeeID(); got != "" {
		t.Errorf("two employees => %q, want empty", got)
	}
}

func TestGoalAnchorAdapter_SoleActiveSkipsImplicitAndMultiple(t *testing.T) {
	ctx := context.Background()
	botStore, goals := newAdapterTestGoalStore(t)
	seedEmployee(t, botStore, "emp1")
	a := newGoalAnchorAdapter(nil, goals, fakeRoster{ids: []string{"emp1"}}, nil)

	// No goals: empty.
	if got := a.SoleActiveGoalID(ctx, "emp1"); got != "" {
		t.Errorf("no goals => %q, want empty", got)
	}

	// An implicit goal alone never anchors (branch 3 handles creation).
	if err := goals.Create(ctx, &employee.Goal{EmployeeID: "emp1", Title: "impl", Mandate: "impl", State: employee.GoalActive, Source: employee.SourceImplicit}); err != nil {
		t.Fatal(err)
	}
	if got := a.SoleActiveGoalID(ctx, "emp1"); got != "" {
		t.Errorf("implicit-only => %q, want empty", got)
	}

	// One non-implicit goal: it anchors.
	userGoal := &employee.Goal{EmployeeID: "emp1", Title: "user", Mandate: "user", State: employee.GoalActive, Source: employee.SourceUser}
	if err := goals.Create(ctx, userGoal); err != nil {
		t.Fatal(err)
	}
	if got := a.SoleActiveGoalID(ctx, "emp1"); got != userGoal.ID {
		t.Errorf("sole non-implicit => %q, want %s", got, userGoal.ID)
	}

	// A second non-implicit goal: ambiguous => empty.
	second := &employee.Goal{EmployeeID: "emp1", Title: "user2", Mandate: "user2", State: employee.GoalActive, Source: employee.SourceUser}
	if err := goals.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got := a.SoleActiveGoalID(ctx, "emp1"); got != "" {
		t.Errorf("two non-implicit => %q, want empty", got)
	}
}

func TestGoalAnchorAdapter_EnsureImplicit(t *testing.T) {
	ctx := context.Background()
	botStore, goals := newAdapterTestGoalStore(t)
	seedEmployee(t, botStore, "emp1")
	a := newGoalAnchorAdapter(nil, goals, fakeRoster{ids: []string{"emp1"}}, nil)

	id1 := a.EnsureImplicitGoal(ctx, "emp1", "plan title")
	if id1 == "" {
		t.Fatal("EnsureImplicitGoal returned empty id")
	}
	// Idempotent: same goal reused on the next plan with a different title.
	id2 := a.EnsureImplicitGoal(ctx, "emp1", "another plan")
	if id2 != id1 {
		t.Errorf("second ensure = %q, want reuse of %s", id2, id1)
	}
}
