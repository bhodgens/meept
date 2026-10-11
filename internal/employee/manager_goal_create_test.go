package employee

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// constitutionToMap converts a Constitution to the raw map shape HireRequest
// expects, via JSON round-trip (matching decodeConstitution's reverse path).
func constitutionToMap(t interface{ Helper() }, c Constitution) map[string]any {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		panic(err) // test-only helper; a marshal failure here is a test bug
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(err)
	}
	return m
}

// newGoalTestManager builds a Manager with a real bot store, constitution
// store, and goal store in a temp dir, plus a hired tier-2 employee, so
// CreateGoal can run its full tier validation path.
func newGoalTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()

	// One shared SQLite file for bot + constitution + goal stores: the
	// employee_goals FK references bot_definitions, which only exists in
	// the bot store's DB (mirrors the daemon's shared-DB wiring mode).
	dbPath := filepath.Join(dir, "shared.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })

	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { constStore.Close() })

	goalStore, err := NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { goalStore.Close() })

	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, goalStore, nil, nil)

	ctx := context.Background()
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	def := bot.BotDefinition{
		ID:       "emp-goals",
		Name:     "goal test bot",
		Prompt:   "test prompt",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}
	if _, err := m.Hire(ctx, HireRequest{
		ID:           def.ID,
		Name:         def.Name,
		Prompt:       def.Prompt,
		Enabled:      true,
		Triggers:     def.Triggers,
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("hire: %v", err)
	}
	return m, def.ID
}

// TestManagerCreateGoal covers leaf 01 Task 2: happy path, invalid source,
// nil goal store.
func TestManagerCreateGoal(t *testing.T) {
	t.Parallel()

	t.Run("happy path persists and returns the goal", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID,
			Title:      "keep main shippable",
			Mandate:    "every commit on main passes CI",
			Source:     "user",
			Gate:       &GoalGateSpec{Command: "go test ./...", TimeoutSeconds: 120},
		})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		if goal.ID == "" {
			t.Error("created goal has empty ID")
		}
		if goal.Source != SourceUser || goal.State != GoalActive {
			t.Errorf("goal = %+v, want source=user state=active", goal)
		}
		if goal.Gate == nil || goal.Gate.Command != "go test ./..." {
			t.Errorf("gate not mapped: %+v", goal.Gate)
		}
		// Round-trip through the store.
		stored, err := m.GetGoal(context.Background(), goal.ID)
		if err != nil {
			t.Fatalf("GetGoal: %v", err)
		}
		if stored.Title != goal.Title || stored.Mandate != goal.Mandate {
			t.Errorf("stored goal mismatch: %+v vs %+v", stored, goal)
		}
	})

	t.Run("empty source defaults to user", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID, Title: "t", Mandate: "m",
		})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		if goal.Source != SourceUser {
			t.Errorf("Source = %q, want %q", goal.Source, SourceUser)
		}
	})

	t.Run("invalid source rejected", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		_, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID, Title: "t", Mandate: "m", Source: "self_proposed",
		})
		if err == nil {
			t.Fatal("expected error for syntactically-invalid source, got nil")
		}
	})

	t.Run("missing required fields rejected", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		_, err := m.CreateGoal(context.Background(), GoalSpec{EmployeeID: empID})
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
	})

	t.Run("nil goal store returns ErrNotImplemented", func(t *testing.T) {
		m := NewManager(nil)
		_, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: "emp", Title: "t", Mandate: "m",
		})
		if !errors.Is(err, ErrNotImplemented) {
			t.Fatalf("err = %v, want ErrNotImplemented", err)
		}
	})
}

// TestHireCreatesGoals covers leaf 01 Task 3: goals in the hire request are
// created and retrievable via ListGoals.
func TestHireCreatesGoals(t *testing.T) {
	t.Parallel()

	m, _ := newGoalTestManager(t) // hires emp-goals already; hire a second with goals

	ctx := context.Background()
	empID := "emp-with-goals"
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	emp, err := m.Hire(ctx, HireRequest{
		ID:      empID,
		Name:    "goalful bot",
		Prompt:  "test prompt",
		Enabled: true,
		Triggers: []bot.BotTrigger{
			{Type: bot.TriggerTypeWebhook, Enabled: true},
		},
		Constitution: constitutionToMap(t, c),
		Goals: []GoalSpec{
			{Title: "goal one", Mandate: "do one", Source: "user"},
			{Title: "goal two", Mandate: "do two", Source: "trigger", TriggerRef: "cron:1h"},
		},
	})
	if err != nil {
		t.Fatalf("Hire with goals: %v", err)
	}
	if emp == nil {
		t.Fatal("Hire returned nil employee")
	}

	goals, err := m.ListGoals(ctx, empID)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("len(goals) = %d, want 2", len(goals))
	}
	found := map[string]bool{}
	for _, g := range goals {
		if g.EmployeeID != empID {
			t.Errorf("goal %s employee_id = %q, want %q", g.ID, g.EmployeeID, empID)
		}
		found[g.Title] = true
	}
	if !found["goal one"] || !found["goal two"] {
		t.Errorf("expected both goal titles, got %v", found)
	}
}

// TestHireInvalidGoalFails covers the errors.Join failure collection path.
func TestHireInvalidGoalFails(t *testing.T) {
	t.Parallel()

	m, _ := newGoalTestManager(t)
	ctx := context.Background()
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	_, err := m.Hire(ctx, HireRequest{
		ID:      "emp-bad-goal",
		Name:    "bad goal bot",
		Prompt:  "test prompt",
		Enabled: true,
		Triggers: []bot.BotTrigger{
			{Type: bot.TriggerTypeWebhook, Enabled: true},
		},
		Constitution: constitutionToMap(t, c),
		Goals: []GoalSpec{
			{Title: "", Mandate: "no title"}, // invalid: title required
		},
	})
	if err == nil {
		t.Fatal("expected Hire to fail when a goal spec is invalid")
	}
}
