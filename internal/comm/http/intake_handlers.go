package http

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleIntakeStatus handles GET /api/v1/intake/status — the HTTP bridge
// for the intake.status RPC (issue #109 GUI parity; pattern:
// handleActivityOverview in activity_handlers.go). Read-only: the daemon
// handler renders the [intake] config snapshot plus the poller-job state
// and NEVER surfaces token material (presence-only posture, same as
// `meept intake status` / github status). No query params: the view is a
// flat snapshot. A daemon whose RPC seam is absent gets 503, matching the
// activity handlers; the RPC method itself always answers (nil-handler
// disabled envelope), so no partial-failure paths exist here.
func (s *Server) handleIntakeStatus(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "intake service not available")
		return
	}

	result, err := s.rpcCall(r.Context(), "intake.status", json.RawMessage("{}"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
