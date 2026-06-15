package web

import (
	"net/http"
)

// handleMemoryStore handles POST /api/v1/memory.
func (s *Server) handleMemoryStore(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		s.writeError(w, http.StatusServiceUnavailable, "memory storage not configured")
		return
	}

	var req MemoryStoreRequest
	if err := readJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		s.writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Type == "" {
		req.Type = "episodic"
	}

	result, err := s.memoryStore.StoreMemory(r.Context(), req)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to store memory: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, result)
}
