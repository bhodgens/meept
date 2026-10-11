package employee

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestRPCHandler_GoalsCreate covers leaf 01 Task 4: agents.goals.create
// round-trips a GoalSpec through the RPC layer into the manager.
func TestRPCHandler_GoalsCreate(t *testing.T) {
	t.Parallel()

	t.Run("registered in method map", func(t *testing.T) {
		h := NewRPCHandler(nil)
		if _, ok := h.Handlers()["agents.goals.create"]; !ok {
			t.Fatal("agents.goals.create not registered")
		}
	})

	t.Run("nil manager returns errNotConfigured", func(t *testing.T) {
		h := NewRPCHandler(nil)
		_, err := h.handleGoalsCreate(context.Background(), json.RawMessage(`{}`))
		if !errors.Is(err, errNotConfigured) {
			t.Fatalf("err = %v, want errNotConfigured", err)
		}
	})

	t.Run("round trip creates goal", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		h := NewRPCHandler(m)

		params, err := json.Marshal(GoalSpec{
			EmployeeID: empID,
			Title:      "rpc goal",
			Mandate:    "created via agents.goals.create",
			Source:     "user",
			Gate:       &GoalGateSpec{Command: "make check"},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		result, err := h.handleGoalsCreate(context.Background(), params)
		if err != nil {
			t.Fatalf("handleGoalsCreate: %v", err)
		}
		resp, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T, want map[string]any", result)
		}
		goal, ok := resp["goal"].(*Goal)
		if !ok {
			t.Fatalf("response[\"goal\"] type = %T, want *Goal", resp["goal"])
		}
		if goal.ID == "" || goal.Title != "rpc goal" {
			t.Errorf("goal = %+v", goal)
		}
		// Round-trip through the store.
		stored, err := m.GetGoal(context.Background(), goal.ID)
		if err != nil {
			t.Fatalf("GetGoal: %v", err)
		}
		if stored.Mandate != "created via agents.goals.create" {
			t.Errorf("stored mandate = %q", stored.Mandate)
		}
	})

	t.Run("invalid spec rejected", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		h := NewRPCHandler(m)
		params, _ := json.Marshal(GoalSpec{EmployeeID: empID}) // missing title+mandate
		if _, err := h.handleGoalsCreate(context.Background(), params); err == nil {
			t.Fatal("expected validation error, got nil")
		}
	})
}
