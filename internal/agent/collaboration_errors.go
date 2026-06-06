package agent

import "fmt"

// CollaborationError represents errors specific to collaboration sessions.
type CollaborationError struct {
	Code    string `json:"code"`
	Session string `json:"session,omitempty"`
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message"`
}

func (e *CollaborationError) Error() string {
	if e.Session != "" {
		return fmt.Sprintf("collaboration error [%s] session=%s phase=%s: %s", e.Code, e.Session, e.Phase, e.Message)
	}
	return fmt.Sprintf("collaboration error [%s]: %s", e.Code, e.Message)
}

// NewCollaborationError creates a new collaboration error.
func NewCollaborationError(code, session, phase, message string) *CollaborationError {
	return &CollaborationError{Code: code, Session: session, Phase: phase, Message: message}
}

// Common collaboration error codes.
const (
	ErrCodeBudgetExceeded  = "budget_exceeded"
	ErrCodeDepthExceeded   = "depth_exceeded"
	ErrCodeAgentFailed     = "agent_failed"
	ErrCodeWorkspace       = "workspace_error"
	ErrCodeCollabTimeout   = "timeout"
	ErrCodeInvalidMode     = "invalid_mode"
	ErrCodeSessionNotFound = "session_not_found"
)

// ErrBudgetExceeded is returned when a sub-session exceeds available token budget.
var ErrBudgetExceeded = &CollaborationError{Code: ErrCodeBudgetExceeded, Message: "insufficient token budget for collaboration session"}

// ErrDepthExceeded is returned when collaboration nesting exceeds max depth.
var ErrDepthExceeded = &CollaborationError{Code: ErrCodeDepthExceeded, Message: "maximum collaboration nesting depth exceeded"}
