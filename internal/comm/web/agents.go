package web

import (
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
			KeyCount: len(agents),
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
		KeyCount: len(agents),
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
	if err := readJSON(w, r, &req); err != nil {
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

// defaultAgentList returns the built-in agent definitions used only as a
// fallback when the server has no agent lister wired. The canonical roster
// lives in config/agents/*/AGENT.md and is reached via the lister path.
func defaultAgentList() []AgentEntry {
	return []AgentEntry{
		{ID: config.AgentIDDispatcher, Name: config.AgentIDDispatcher, Role: "Dispatcher", Description: "Intake, classify, route to specialists", Enabled: true},
		{ID: config.AgentIDChat, Name: config.AgentIDChat, Role: RoleExecutor, Description: "General conversation", Enabled: true},
		{ID: config.AgentIDCoder, Name: config.AgentIDCoder, Role: RoleExecutor, Description: "File ops, shell, coding tasks", Enabled: true},
		{ID: config.AgentIDDebugger, Name: config.AgentIDDebugger, Role: RoleExecutor, Description: "Troubleshooting, bug fixing", Enabled: true},
		{ID: config.AgentIDPlanner, Name: config.AgentIDPlanner, Role: RoleExecutor, Description: "Task decomposition, planning", Enabled: true},
		{ID: config.AgentIDAnalyst, Name: config.AgentIDAnalyst, Role: RoleExecutor, Description: "Synthesizes information, draws insights, summarizes", Enabled: true},
		{ID: config.AgentIDResearcher, Name: config.AgentIDResearcher, Role: RoleExecutor, Description: "Gathers information from web, documentation, and codebase", Enabled: true},
		{ID: config.AgentIDCommitter, Name: config.AgentIDCommitter, Role: RoleExecutor, Description: "Git operations", Enabled: true},
		{ID: config.AgentIDScheduler, Name: config.AgentIDScheduler, Role: RoleExecutor, Description: "Job scheduling", Enabled: true},
		{ID: "code-reviewer", Name: "code-reviewer", Role: RoleReviewer, Description: "Reviews code changes for correctness and style", Enabled: true},
		{ID: "test-reviewer", Name: "test-reviewer", Role: RoleReviewer, Description: "Reviews test coverage and correctness", Enabled: true},
		{ID: "debug-reviewer", Name: "debug-reviewer", Role: RoleReviewer, Description: "Reviews debugging work for root-cause and side effects", Enabled: true},
		{ID: "analyst-reviewer", Name: "analyst-reviewer", Role: RoleReviewer, Description: "Reviews analyses for accuracy and actionability", Enabled: true},
		{ID: "planner-reviewer", Name: "planner-reviewer", Role: RoleReviewer, Description: "Reviews plans for feasibility and ordering", Enabled: true},
	}
}
