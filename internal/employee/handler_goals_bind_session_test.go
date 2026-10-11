package employee

// Tests for goal-completion leaf 03 Task 3: agents.goals.bind_session —
// validates the goal is active, then writes the session goal via the
// injected session-goal setter.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// stubSessionGoalBinder records SetGoalID calls.
type stubSessionGoalBinder struct {
	calls  [][2]string
	setErr error
}

func (s *stubSessionGoalBinder) SetGoalID(sessionID, goalID string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.calls = append(s.calls, [2]string{sessionID, goalID})
	return nil
}

// TestRPCHandler_GoalsBindSession covers leaf 03 Task 3.
func TestRPCHandler_GoalsBindSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("registered in method map", func(t *testing.T) {
		h := NewRPCHandler(nil)
		if _, ok := h.Handlers()["agents.goals.bind_session"]; !ok {
			t.Fatal("agents.goals.bind_session not registered")
		}
	})

	t.Run("nil manager returns errNotConfigured", func(t *testing.T) {
		h := NewRPCHandler(nil)
		_, err := h.handleGoalsBindSession(ctx, json.RawMessage(`{"session_id":"s","goal_id":"g"}`))
		if !errors.Is(err, errNotConfigured) {
			t.Fatalf("err = %v, want errNotConfigured", err)
		}
	})

	t.Run("binds active goal", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(ctx, GoalSpec{EmployeeID: empID, Title: "bind me", Mandate: "bind me"})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}

		binder := &stubSessionGoalBinder{}
		h := NewRPCHandler(m)
		h.SetSessionGoalBinder(binder)

		params, _ := json.Marshal(bindSessionRequest{SessionID: "sess-1", GoalID: goal.ID})
		if _, err := h.handleGoalsBindSession(ctx, params); err != nil {
			t.Fatalf("handleGoalsBindSession: %v", err)
		}
		if len(binder.calls) != 1 || binder.calls[0] != [2]string{"sess-1", goal.ID} {
			t.Fatalf("binder calls = %v, want one (sess-1, %s)", binder.calls, goal.ID)
		}
	})

	t.Run("rejects non-active goal", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(ctx, GoalSpec{EmployeeID: empID, Title: "retire me", Mandate: "retire me"})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		if err := m.goalStore.Retire(ctx, goal.ID, time.Now().UTC()); err != nil {
			t.Fatalf("Retire: %v", err)
		}

		binder := &stubSessionGoalBinder{}
		h := NewRPCHandler(m)
		h.SetSessionGoalBinder(binder)

		params, _ := json.Marshal(bindSessionRequest{SessionID: "sess-1", GoalID: goal.ID})
		if _, err := h.handleGoalsBindSession(ctx, params); err == nil {
			t.Fatal("expected error binding a retired goal, got nil")
		}
		if len(binder.calls) != 0 {
			t.Fatalf("binder called for inactive goal: %v", binder.calls)
		}
	})

	t.Run("missing session id rejected", func(t *testing.T) {
		m, _ := newGoalTestManager(t)
		h := NewRPCHandler(m)
		h.SetSessionGoalBinder(&stubSessionGoalBinder{})
		params, _ := json.Marshal(bindSessionRequest{GoalID: "goal_x"})
		if _, err := h.handleGoalsBindSession(ctx, params); err == nil {
			t.Fatal("expected error for empty session_id, got nil")
		}
	})

	t.Run("empty goal id unbinds without goal lookup", func(t *testing.T) {
		m, _ := newGoalTestManager(t)
		binder := &stubSessionGoalBinder{}
		h := NewRPCHandler(m)
		h.SetSessionGoalBinder(binder)

		params, _ := json.Marshal(bindSessionRequest{SessionID: "sess-2"})
		if _, err := h.handleGoalsBindSession(ctx, params); err != nil {
			t.Fatalf("unbind: %v", err)
		}
		if len(binder.calls) != 1 || binder.calls[0] != [2]string{"sess-2", ""} {
			t.Fatalf("binder calls = %v, want one (sess-2, \"\")", binder.calls)
		}
	})

	t.Run("binder error propagates", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(ctx, GoalSpec{EmployeeID: empID, Title: "t", Mandate: "t"})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		binder := &stubSessionGoalBinder{setErr: errors.New("no session")}
		h := NewRPCHandler(m)
		h.SetSessionGoalBinder(binder)

		params, _ := json.Marshal(bindSessionRequest{SessionID: "sess-3", GoalID: goal.ID})
		if _, err := h.handleGoalsBindSession(ctx, params); err == nil {
			t.Fatal("expected binder error to propagate, got nil")
		}
	})
}
