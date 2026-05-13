package web

import (
	"encoding/json"
	"net/http"

	"github.com/caimlas/meept/internal/config"
)

// handleAgentsList handles GET /api/v1/agents.
func (s *Server) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	if s.agentLister == nil {
		// Return static list from known agent definitions.
		agents := defaultAgentList()
		s.writeJSON(w, http.StatusOK, map[string]any{
			"agents": agents,
			"count":  len(agents),
		})
		return
	}

	agents, err := s.agentLister.ListAgents(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list agents: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"agents": agents,
		"count":  len(agents),
	})
}

// handleAgentsDelegate handles POST /api/v1/agents/{id}/delegate.
func (s *Server) handleAgentsDelegate(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	if agentID == "" {
		s.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	var req DelegateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		s.writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	if s.agentLister == nil {
		s.writeError(w, http.StatusServiceUnavailable, "agent delegation not configured")
		return
	}

	result, err := s.agentLister.DelegateTask(r.Context(), agentID, req.Message)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "delegation failed: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// defaultAgentList returns the built-in agent definitions.
func defaultAgentList() []AgentEntry {
	return []AgentEntry{
		{ID: config.AgentIDDispatcher, Name: config.AgentIDDispatcher, Role: "Dispatcher", Description: "Intake, classify, route to specialists", Enabled: true},
		{ID: config.AgentIDChat, Name: config.AgentIDChat, Role: "Executor", Description: "General conversation", Enabled: true},
		{ID: config.AgentIDCoder, Name: config.AgentIDCoder, Role: "Executor", Description: "File ops, shell, coding tasks", Enabled: true},
		{ID: config.AgentIDDebugger, Name: config.AgentIDDebugger, Role: "Executor", Description: "Troubleshooting, bug fixing", Enabled: true},
		{ID: config.AgentIDPlanner, Name: config.AgentIDPlanner, Role: "Executor", Description: "Task decomposition, planning", Enabled: true},
		{ID: config.AgentIDAnalyst, Name: config.AgentIDAnalyst, Role: "Executor", Description: "Research, data analysis", Enabled: true},
		{ID: config.AgentIDCommitter, Name: config.AgentIDCommitter, Role: "Executor", Description: "Git operations", Enabled: true},
		{ID: config.AgentIDScheduler, Name: config.AgentIDScheduler, Role: "Executor", Description: "Job scheduling", Enabled: true},
	}
}
