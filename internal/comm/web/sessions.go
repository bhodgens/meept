package web

import (
	"encoding/json"
	"net/http"
)

// handleSessionsList handles GET /api/v1/sessions.
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"sessions": []any{},
			KeyCount:   0,
			KeyMessage: "session management not configured",
		})
		return
	}

	sessions, err := s.sessionManager.ListSessions(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list sessions: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		KeyCount:   len(sessions),
	})
}

// handleSessionsCreate handles POST /api/v1/sessions.
func (s *Server) handleSessionsCreate(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session management not configured")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		req.Name = "unnamed session"
	}

	session, err := s.sessionManager.CreateSession(r.Context(), req.Name)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create session: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, session)
}

// handleSessionsGet handles GET /api/v1/sessions/{id}.
func (s *Server) handleSessionsGet(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session management not configured")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	session, err := s.sessionManager.GetSession(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "session not found: "+id)
		return
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleSessionsDelete handles DELETE /api/v1/sessions/{id}.
func (s *Server) handleSessionsDelete(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session management not configured")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	if err := s.sessionManager.DeleteSession(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, "session not found: "+id)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
