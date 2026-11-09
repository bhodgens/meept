package rpc

// delegate_children.go — the delegate.status RPC surface (DSH-9d, audit
// item 5 operator half): a read-only report of a session's (or agent's)
// delegated child records. It rides the seam-based handler pattern of
// voice_local.go — func-field seams, no internal/rpc -> internal/agent
// import (agent already imports rpc transitively via activity); the daemon
// wiring adapts *agent.DelegateManager onto the seam.
//
// Shapes:
//
//	delegate.status req {query} -> {query, children: [...], note}
//
// Report-only: there is NO steer/interrupt/cancel surface here — the model
// already owns child control through the subagent_* tools; this is the
// operator's read mirror (the intake_status.go posture). Rows carry the
// sanitized child projection (child_id, agent_id, parent_session_id, mode,
// state, model, created_at, has_result, error) — never the seed snapshot,
// the live ACP session, or the full result payload.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DelegateChildRow is one delegated child in the delegate.status report.
// Error is presence-carrying (the child's settlement error text, truncated)
// but carries no prompt/seed material.
type DelegateChildRow struct {
	ChildID         string `json:"child_id"`
	AgentID         string `json:"agent_id"`
	ParentSessionID string `json:"parent_session_id"`
	Mode            string `json:"mode"`
	State           string `json:"state"`
	Model           string `json:"model"`
	Provider        string `json:"provider"`
	CreatedAt       string `json:"created_at_rfc3339,omitempty"`
	HasResult       bool   `json:"has_result"`
	Error           string `json:"error,omitempty"`
}

// DelegateStatusResult is the delegate.status response. Children is never
// nil (empty list, not absent) so clients render "no delegated children"
// from the same shape.
type DelegateStatusResult struct {
	Query    string             `json:"query,omitempty"`
	Children []DelegateChildRow `json:"children"`
	Note     string             `json:"note,omitempty"`
}

// DelegateChildrenSeam carries the daemon-side snapshot operation. Nil =
// the disabled posture: status answers an empty list with the unwired note
// (never "method not found", never a panic).
type DelegateChildrenSeam struct {
	// Children returns every registered child record snapshot; the RPC
	// layer filters by query and projects onto rows.
	Children func() []DelegateChildRow
}

// DelegateChildrenHandler serves delegate.status.
type DelegateChildrenHandler struct {
	seam DelegateChildrenSeam
}

// NewDelegateChildrenHandler constructs the handler. The zero seam is the
// disabled posture: status answers an empty list with a note.
func NewDelegateChildrenHandler(seam DelegateChildrenSeam) *DelegateChildrenHandler {
	return &DelegateChildrenHandler{seam: seam}
}

// RegisterDelegateStatusMethods registers delegate.status on the server.
// Always registers (even with a nil seam) so operators get the named
// disabled envelope instead of "method not found".
func (h *DelegateChildrenHandler) RegisterDelegateStatusMethods(server *Server) {
	if server == nil || h == nil {
		return
	}
	server.RegisterHandler("delegate.status", h.handleStatus)
}

// delegateChildRowFromRecord projects one record onto the wire row. Kept
// here (not on the record type) so the wire shape stays an rpc concern.
// Kept as a package function for the daemon adapter to reuse.
// maxDelegateChildError bounds a row's error text (report surface, not a
// log dump).
const maxDelegateChildError = 300

// NewDelegateChildRow builds a row from the record fields (the daemon
// adapter maps agent.ChildRecord through this; declared in the rpc package
// so the frozen wire shape and its construction stay together).
func NewDelegateChildRow(childID, agentID, parentSessionID, mode, state, model, provider, createdAt string, hasResult bool, errText string) DelegateChildRow {
	if len(errText) > maxDelegateChildError {
		errText = errText[:maxDelegateChildError] + " ..."
	}
	return DelegateChildRow{
		ChildID:         childID,
		AgentID:         agentID,
		ParentSessionID: parentSessionID,
		Mode:            mode,
		State:           state,
		Model:           model,
		Provider:        provider,
		CreatedAt:       createdAt,
		HasResult:       hasResult,
		Error:           errText,
	}
}

// handleStatus answers delegate.status. An empty query is a named error
// (the operator must say what they are looking at); an unknown query is an
// EMPTY list with a note, not an error (report-only posture: absence is
// information, same as intake status).
func (h *DelegateChildrenHandler) handleStatus(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Query string `json:"query"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("delegate.status: invalid parameters: %w", err)
		}
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("delegate.status: query is required (a session id or agent id)")
	}

	out := DelegateStatusResult{Query: query, Children: []DelegateChildRow{}}
	if h.seam.Children == nil {
		out.Note = "delegate registry not wired on this daemon"
		return out, nil
	}
	for _, row := range h.seam.Children() {
		if row.ParentSessionID == query || row.AgentID == query || row.ChildID == query {
			out.Children = append(out.Children, row)
		}
	}
	if len(out.Children) == 0 {
		out.Note = "no delegated children match this query (children are process-local; a daemon restart clears the registry)"
	}
	return out, nil
}
