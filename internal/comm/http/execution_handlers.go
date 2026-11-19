package http

// execution_handlers.go - HTTP bridges for the durable execution-run
// history (issue #160 follow-up) and the unified roster. Each handler is a
// thin RPC forward (pattern: activity_handlers.go); the RPC layer owns all
// validation and nil-degradation.

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleExecutionRuns handles GET /api/v1/executions.
// Query params: agent_id (optional), limit (1-1000, default 50),
// since (optional RFC3339).
func (s *Server) handleExecutionRuns(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "execution service not available")
		return
	}

	limit, err := parseIntParam(r, "limit", 50, 1, 1000)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := struct {
		AgentID string `json:"agent_id"`
		Limit   int    `json:"limit"`
		Since   string `json:"since,omitempty"`
	}{
		AgentID: r.URL.Query().Get("agent_id"),
		Limit:   limit,
		Since:   r.URL.Query().Get("since"),
	}

	paramBytes, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "agents.executionRuns", paramBytes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleExecutionHistogram handles GET /api/v1/executions/histogram.
// Query params: agent_id (required). Returns 8 token cells for the last hour.
func (s *Server) handleExecutionHistogram(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "execution service not available")
		return
	}

	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		s.writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	params := struct {
		AgentID string `json:"agent_id"`
	}{AgentID: agentID}

	paramBytes, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "agents.executionHistogram", paramBytes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleAgentsRoster handles GET /api/v1/agents/roster - the unified
// agent/bot/employee roster (C1 rows with active_count). The GUI reads
// active counts from here for the per-card concurrent-run counter.
func (s *Server) handleAgentsRoster(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "roster service not available")
		return
	}

	result, err := s.rpcCall(r.Context(), "agents.roster", json.RawMessage("{}"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
