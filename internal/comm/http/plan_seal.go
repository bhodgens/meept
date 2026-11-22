package http

// Plan draft/seal HTTP twins (20261119-usability-remediation 06-planning
// leaf 02, T2): GET /api/v1/tasks/{id}/draft and POST /api/v1/tasks/{id}/seal
// mirror the plan.draft (get shape) / plan.seal RPC methods from
// internal/rpc/plan_seal.go via the same rpcCall seam the C2 task-approval
// twins use, so the GUI seal surface drives identical semantics to the CLI:
// a compile WITH problems is a RESULT ({status:"problems", problems:[...]})
// rendered inline by the client, and sealing is only ever an explicit user
// action - no route here compiles or executes without a POST /seal.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleTaskDraft handles GET /api/v1/tasks/{id}/draft -- the plan.draft
// RPC's get shape (no markdown in params).
func (s *Server) handleTaskDraft(w http.ResponseWriter, r *http.Request) {
	s.dispatchPlanSeal(w, r, "plan.draft", false)
}

// handleTaskSeal handles POST /api/v1/tasks/{id}/seal -- plan.seal:
// compile, persist, stamp, execute. Never invoked implicitly.
func (s *Server) handleTaskSeal(w http.ResponseWriter, r *http.Request) {
	s.dispatchPlanSeal(w, r, "plan.seal", true)
}

// dispatchPlanSeal is the shared twin body: validate the path id and
// forward to the RPC registry through rpcCall. Error mapping mirrors the
// seal handler's refusals: "no draft" is a client-fixable 404, the rest
// map like dispatchTaskApproval.
func (s *Server) dispatchPlanSeal(
	w http.ResponseWriter,
	r *http.Request,
	method string,
	withBody bool,
) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "plan seal service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	// The seal/draft RPCs key on task_id; the path id is authoritative.
	// Optional JSON body is accepted on POST for forward-compat but the
	// markdown-save shape of plan.draft is NOT exposed over this GET twin.
	if withBody && r.ContentLength > 0 {
		var probe map[string]any
		if !s.readJSON(w, r, &probe) {
			return
		}
	}

	params, err := json.Marshal(map[string]any{"task_id": id})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to encode params: %v", err))
		return
	}

	result, err := s.rpcCall(r.Context(), method, params)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "no draft"):
			// plan.seal/plan.draft refuse tasks without a compiler draft:
			// "no draft for task <id>" -- a 404, same posture as the
			// task-approval twins' unknown-task mapping.
			s.writeError(w, http.StatusNotFound, msg)
		case strings.Contains(msg, "not available"):
			s.writeError(w, http.StatusServiceUnavailable, msg)
		default:
			// Compile failures ("plan compile failed: ...") are honest
			// 400s -- the draft stays a draft, the client renders the
			// message. Compile PROBLEMS arrive as 200 results, not here.
			s.writeError(w, http.StatusBadRequest, msg)
		}
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
