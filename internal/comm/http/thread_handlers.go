package http

import (
	"net/http"

	"github.com/caimlas/meept/internal/services"
)

// Thread HTTP endpoints for thread-based context partitioning.
//
// All endpoints live under /api/v1/sessions/{id}/threads and delegate to
// services.ThreadService, which wraps the session store's thread CRUD.
// The handlers mirror the RPC methods in internal/daemon/thread_rpc.go.

// handleThreadList handles GET /api/v1/sessions/{id}/threads.
func (s *Server) handleThreadList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Thread == nil {
		s.writeError(w, http.StatusServiceUnavailable, "thread service not available")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		s.writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	threads, err := s.services.Thread.ListThreads(r.Context(), services.ListThreadsRequest{SessionID: sessionID})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

// handleThreadCreate handles POST /api/v1/sessions/{id}/threads.
func (s *Server) handleThreadCreate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Thread == nil {
		s.writeError(w, http.StatusServiceUnavailable, "thread service not available")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		s.writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	var req services.CreateThreadRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	req.SessionID = sessionID
	thread, err := s.services.Thread.CreateThread(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, thread)
}

// handleThreadGet handles GET /api/v1/sessions/{id}/threads/{threadID}.
func (s *Server) handleThreadGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Thread == nil {
		s.writeError(w, http.StatusServiceUnavailable, "thread service not available")
		return
	}
	threadID := r.PathValue("threadID")
	if threadID == "" {
		s.writeError(w, http.StatusBadRequest, "missing thread id")
		return
	}
	thread, err := s.services.Thread.GetThread(r.Context(), services.GetThreadRequest{ThreadID: threadID})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, thread)
}

// handleThreadDelete handles DELETE /api/v1/sessions/{id}/threads/{threadID}.
func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Thread == nil {
		s.writeError(w, http.StatusServiceUnavailable, "thread service not available")
		return
	}
	threadID := r.PathValue("threadID")
	if threadID == "" {
		s.writeError(w, http.StatusBadRequest, "missing thread id")
		return
	}
	if err := s.services.Thread.DeleteThread(r.Context(), services.DeleteThreadRequest{ThreadID: threadID}); err != nil {
		s.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleThreadGetActive handles GET /api/v1/sessions/{id}/threads/active.
func (s *Server) handleThreadGetActive(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Thread == nil {
		s.writeError(w, http.StatusServiceUnavailable, "thread service not available")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		s.writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	thread, err := s.services.Thread.GetActiveThread(r.Context(), services.GetActiveThreadRequest{SessionID: sessionID})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, thread)
}

// handleThreadSetActive handles PUT /api/v1/sessions/{id}/threads/active.
func (s *Server) handleThreadSetActive(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Thread == nil {
		s.writeError(w, http.StatusServiceUnavailable, "thread service not available")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		s.writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	var req services.SetActiveThreadRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	req.SessionID = sessionID
	thread, err := s.services.Thread.SetActiveThread(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, thread)
}
