package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// activityHandlerParams holds the parsed query params for activity.query.
type activityHandlerParams struct {
	Since   string   `json:"since"`
	Until   string   `json:"until"`
	Sort    string   `json:"sort"`
	TaskID  string   `json:"task_id"`
	PlanID  string   `json:"plan_id"`
	AgentID string   `json:"agent_id"`
	Kinds   []string `json:"kinds"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

// handleActivityQuery handles GET /api/v1/activity.
func (s *Server) handleActivityQuery(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "activity service not available")
		return
	}

	// Parse query params
	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")
	sort := r.URL.Query().Get("sort")
	taskID := r.URL.Query().Get("task_id")
	planID := r.URL.Query().Get("plan_id")
	agentID := r.URL.Query().Get("agent_id")
	kindsParam := r.URL.Query().Get("kinds")
	limit, err := parseIntParam(r, "limit", 50, 0, 1000)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := parseIntParam(r, "offset", 0, 0, 10000)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate sort value
	switch sort {
	case "", "chrono", "task", "plan":
		// valid
	default:
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid sort value %q (want chrono, task, or plan)", sort))
		return
	}

	// Parse kinds (comma-separated -> JSON array)
	var kinds []string
	if kindsParam != "" {
		parts := strings.Split(kindsParam, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				kinds = append(kinds, p)
			}
		}
	}

	params := activityHandlerParams{
		Since:   since,
		Until:   until,
		Sort:    sort,
		TaskID:  taskID,
		PlanID:  planID,
		AgentID: agentID,
		Kinds:   kinds,
		Limit:   limit,
		Offset:  offset,
	}

	paramBytes, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "activity.query", paramBytes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleActivityRecent handles GET /api/v1/activity/recent.
func (s *Server) handleActivityRecent(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "activity service not available")
		return
	}

	limit, err := parseIntParam(r, "limit", 50, 1, 1000)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := struct {
		Limit int `json:"limit"`
	}{Limit: limit}

	paramBytes, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "activity.recent", paramBytes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleActivityOverview handles GET /api/v1/activity/overview.
func (s *Server) handleActivityOverview(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "activity service not available")
		return
	}

	// No params needed
	result, err := s.rpcCall(r.Context(), "activity.overview", json.RawMessage("{}"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleActivityFindings handles GET /api/v1/activity/findings.
func (s *Server) handleActivityFindings(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "activity service not available")
		return
	}

	since := r.URL.Query().Get("since")
	severity := r.URL.Query().Get("severity")
	employeeID := r.URL.Query().Get("employee_id")
	resolvedParam := r.URL.Query().Get("resolved")
	limit, err := parseIntParam(r, "limit", 100, 0, 1000)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := parseIntParam(r, "offset", 0, 0, 10000)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Parse resolved boolean (optional)
	var resolved *bool
	if resolvedParam != "" {
		b := resolvedParam == "true"
		resolved = &b
	}

	params := struct {
		EmployeeID string `json:"employee_id"`
		Since      string `json:"since"`
		Severity   string `json:"severity"`
		Resolved   *bool  `json:"resolved,omitempty"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
	}{
		EmployeeID: employeeID,
		Since:      since,
		Severity:   severity,
		Resolved:   resolved,
		Limit:      limit,
		Offset:     offset,
	}

	paramBytes, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "activity.findings", paramBytes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleAgentsReview handles GET /api/v1/agents/{id}/review.
func (s *Server) handleAgentsReview(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "activity service not available")
		return
	}

	employeeID := r.PathValue("id")
	if employeeID == "" {
		s.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	params := struct {
		EmployeeID string `json:"employee_id"`
	}{EmployeeID: employeeID}

	paramBytes, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "agents.review", paramBytes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
