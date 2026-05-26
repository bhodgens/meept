package web

import (
	"net/http"
)

// handleToolsList handles GET /api/v1/tools.
func (s *Server) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if s.toolLister == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"tools":    []any{},
			KeyCount:   0,
			KeyMessage: "tool listing not configured",
		})
		return
	}

	tools, err := s.toolLister.ListTools(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list tools: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"tools":  tools,
		KeyCount: len(tools),
	})
}
