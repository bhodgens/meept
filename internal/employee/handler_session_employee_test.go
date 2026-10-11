package employee

// Tests for the session→employee binding RPC (goal-completion follow-up
// #5): agents.employees.bind_session validates the employee exists, then
// writes the session binding via the injected SessionEmployeeBinder.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// stubSessionEmployeeBinder records SetEmployeeID calls.
type stubSessionEmployeeBinder struct {
	calls  [][2]string
	setErr error
}

func (s *stubSessionEmployeeBinder) SetEmployeeID(sessionID, employeeID string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.calls = append(s.calls, [2]string{sessionID, employeeID})
	return nil
}

// TestRPCHandler_EmployeesBindSession covers the binding seam.
func TestRPCHandler_EmployeesBindSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("registered in method map", func(t *testing.T) {
		h := NewRPCHandler(nil)
		if _, ok := h.Handlers()["agents.employees.bind_session"]; !ok {
			t.Fatal("agents.employees.bind_session not registered")
		}
	})

	t.Run("nil manager returns errNotConfigured", func(t *testing.T) {
		h := NewRPCHandler(nil)
		_, err := h.handleEmployeesBindSession(ctx, json.RawMessage(`{"session_id":"s","employee_id":"e"}`))
		if !errors.Is(err, errNotConfigured) {
			t.Fatalf("err = %v, want errNotConfigured", err)
		}
	})

	t.Run("unwired session store returns clear error", func(t *testing.T) {
		m, _ := newGoalTestManager(t)
		h := NewRPCHandler(m)
		params, _ := json.Marshal(bindEmployeeSessionRequest{SessionID: "sess-1", EmployeeID: "emp-1"})
		if _, err := h.handleEmployeesBindSession(ctx, params); err == nil {
			t.Fatal("expected error with no binder wired, got nil")
		}
	})

	t.Run("binds existing employee", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		binder := &stubSessionEmployeeBinder{}
		h := NewRPCHandler(m)
		h.SetSessionEmployeeBinder(binder)

		params, _ := json.Marshal(bindEmployeeSessionRequest{SessionID: "sess-1", EmployeeID: empID})
		resp, err := h.handleEmployeesBindSession(ctx, params)
		if err != nil {
			t.Fatalf("handleEmployeesBindSession: %v", err)
		}
		if bound, ok := resp.(map[string]any)["bound"].(bool); !ok || !bound {
			t.Errorf("response bound = %v, want true", resp)
		}
		if len(binder.calls) != 1 || binder.calls[0] != [2]string{"sess-1", empID} {
			t.Fatalf("binder calls = %v, want one (sess-1, %s)", binder.calls, empID)
		}
	})

	t.Run("rejects unknown employee", func(t *testing.T) {
		m, _ := newGoalTestManager(t)
		binder := &stubSessionEmployeeBinder{}
		h := NewRPCHandler(m)
		h.SetSessionEmployeeBinder(binder)

		params, _ := json.Marshal(bindEmployeeSessionRequest{SessionID: "sess-1", EmployeeID: "emp-does-not-exist"})
		if _, err := h.handleEmployeesBindSession(ctx, params); err == nil {
			t.Fatal("expected error binding an unknown employee, got nil")
		}
		if len(binder.calls) != 0 {
			t.Fatalf("binder called for unknown employee: %v", binder.calls)
		}
	})

	t.Run("missing session id rejected", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		h := NewRPCHandler(m)
		h.SetSessionEmployeeBinder(&stubSessionEmployeeBinder{})
		params, _ := json.Marshal(bindEmployeeSessionRequest{EmployeeID: empID})
		if _, err := h.handleEmployeesBindSession(ctx, params); err == nil {
			t.Fatal("expected error for empty session_id, got nil")
		}
	})

	t.Run("empty employee id unbinds without employee lookup", func(t *testing.T) {
		m, _ := newGoalTestManager(t)
		binder := &stubSessionEmployeeBinder{}
		h := NewRPCHandler(m)
		h.SetSessionEmployeeBinder(binder)

		params, _ := json.Marshal(bindEmployeeSessionRequest{SessionID: "sess-2"})
		if _, err := h.handleEmployeesBindSession(ctx, params); err != nil {
			t.Fatalf("unbind: %v", err)
		}
		if len(binder.calls) != 1 || binder.calls[0] != [2]string{"sess-2", ""} {
			t.Fatalf("binder calls = %v, want one (sess-2, \"\")", binder.calls)
		}
	})

	t.Run("binder error propagates", func(t *testing.T) {
		m, empID := newGoalTestManager(t)
		binder := &stubSessionEmployeeBinder{setErr: errors.New("no session")}
		h := NewRPCHandler(m)
		h.SetSessionEmployeeBinder(binder)

		params, _ := json.Marshal(bindEmployeeSessionRequest{SessionID: "sess-3", EmployeeID: empID})
		if _, err := h.handleEmployeesBindSession(ctx, params); err == nil {
			t.Fatal("expected binder error to propagate, got nil")
		}
	})
}
