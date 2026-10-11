package employee

// Session→employee binding seam (goal-completion follow-up #5). Lives in
// its own file beside handler.go so the binding RPC does not entangle with
// the goals handlers: agents.employees.bind_session persists a session's
// employee_id, which the daemon's goal-anchor adapter reads to attribute
// chat-path goals on multi-employee installs. Same seam shape as
// SessionGoalBinder (leaf 03 Task 3): the daemon injects the session store;
// package employee never imports internal/session directly.

import (
	"context"
	"encoding/json"
	"fmt"
)

// SessionEmployeeBinder is the narrow session-store surface the binding RPC
// needs: persisting (or clearing, with an empty employeeID) a session's
// employee binding. Satisfied structurally by session.Store.
type SessionEmployeeBinder interface {
	SetEmployeeID(sessionID, employeeID string) error
}

// SetSessionEmployeeBinder wires the session store for
// agents.employees.bind_session. Nil is ignored (typed-nil guard per
// docs/agents/coding-practices.md).
func (h *RPCHandler) SetSessionEmployeeBinder(b SessionEmployeeBinder) {
	if b == nil {
		return
	}
	h.sessionEmployees = b
}

// bindEmployeeSessionRequest is the agents.employees.bind_session params
// shape. An empty employee_id clears the session's binding (unbind).
type bindEmployeeSessionRequest struct {
	SessionID  string `json:"session_id"`
	EmployeeID string `json:"employee_id"`
}

// handleEmployeesBindSession handles agents.employees.bind_session
// (goal-completion follow-up #5): validates the employee exists and writes
// the session employee binding via the injected SessionEmployeeBinder. An
// empty employee_id clears the binding (unbind) and skips the lookup —
// there is nothing to check. TUI/GUI exposure is deferred; this RPC is the
// seam (scope guard: no employee-selection UI in this change).
func (h *RPCHandler) handleEmployeesBindSession(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	if h.sessionEmployees == nil {
		return nil, fmt.Errorf("session employee binding unavailable: session store not wired")
	}
	var req bindEmployeeSessionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if req.EmployeeID != "" {
		emp, err := h.manager.GetEmployee(ctx, req.EmployeeID)
		if err != nil {
			return nil, fmt.Errorf("get employee: %w", err)
		}
		if emp == nil {
			return nil, fmt.Errorf("employee %s not found", req.EmployeeID)
		}
	}
	if err := h.sessionEmployees.SetEmployeeID(req.SessionID, req.EmployeeID); err != nil {
		return nil, fmt.Errorf("bind session employee: %w", err)
	}
	return map[string]any{
		"session_id":  req.SessionID,
		"employee_id": req.EmployeeID,
		"bound":       req.EmployeeID != "",
	}, nil
}
