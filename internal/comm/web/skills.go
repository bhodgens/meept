package web

import (
	"net/http"
)

// handleSkillsExecute handles POST /api/v1/skills/{name}/execute.
func (s *Server) handleSkillsExecute(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "skill name is required")
		return
	}

	var req SkillExecuteRequest
	if err := readJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Input == "" {
		s.writeError(w, http.StatusBadRequest, "input is required")
		return
	}

	if s.skillExecutor == nil {
		s.writeError(w, http.StatusServiceUnavailable, "skill execution not configured")
		return
	}

	result, err := s.skillExecutor.ExecuteSkill(r.Context(), name, req.Input)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "skill execution failed: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
