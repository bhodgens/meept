package http

// Task approval HTTP twins (20261119-usability-remediation 01-safety leaf
// 01, C2): POST /api/v1/tasks/{id}/approve and /reject mirror the plans
// approve/reject REST shape and dispatch to the SAME TaskApprovalHandler
// seams the RPC surface uses (task.approve / task.reject via rpcCall), so
// a rejection can never approve: the RPC handler's RejectFunc is the only
// reject path on every transport.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleTaskApprove handles POST /api/v1/tasks/{id}/approve.
func (s *Server) handleTaskApprove(w http.ResponseWriter, r *http.Request) {
	s.dispatchTaskApproval(w, r, "task.approve")
}

// handleTaskReject handles POST /api/v1/tasks/{id}/reject.
func (s *Server) handleTaskReject(w http.ResponseWriter, r *http.Request) {
	s.dispatchTaskApproval(w, r, "task.reject")
}

// dispatchTaskApproval is the shared twin body: validate the path id,
// read the optional JSON body ({"reason": "..."} on reject), and forward
// to the RPC approval handler through rpcCall. Error mapping mirrors the
// planner seams' honest refusals: unknown task -> 404, wrong state -> 409.
func (s *Server) dispatchTaskApproval(w http.ResponseWriter, r *http.Request, method string) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task approval service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if !s.readJSON(w, r, &req) {
		return
	}

	params, err := json.Marshal(map[string]any{
		"task_id": id,
		"reason":  req.Reason,
	})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to encode params: %v", err))
		return
	}

	result, err := s.rpcCall(r.Context(), method, params)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not found"):
			// StrategicPlanner.ApprovePlan/RejectPlan refuse unknown ids
			// with "task not found: <id>" - a client-fixable 404, not 500.
			s.writeError(w, http.StatusNotFound, msg)
		case strings.Contains(msg, "is in state"):
			// The task is not awaiting approval (already executing,
			// rejected, completed): a state conflict, mirroring the
			// queue's ErrJobNotClaimable 409 posture.
			s.writeError(w, http.StatusConflict, msg)
		default:
			s.handleServiceError(w, err)
		}
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
