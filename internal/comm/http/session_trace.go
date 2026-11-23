package http

// Dispatch-trace HTTP bridge (20261121-interactive-usability leaf 09,
// task 6 / P2-1): GET /api/v1/sessions/{id}/trace mirrors the TUI
// sessions-view `t` affordance and the CLI `meept session trace`, proxying
// the session.dispatch_trace RPC through rpcCall (same bus-call lane as
// memory.reviewQueue). An unknown id yields an EMPTY list, never a 500 —
// the C5 dual-key contract (internal/rpc/dispatch_trace.go) already
// resolves unknown ids to nothing.

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// handleSessionTrace handles GET /api/v1/sessions/{id}/trace.
//
// Query params:
//
//	limit - optional row cap (default 50, the RPC's own default).
//
// Response JSON (pinned shape for the GUI side):
//
//	{
//	  "session_id": "<path id as requested>",
//	  "count":      <int>,
//	  "entries": [ { "session_id", "turn_id", "input_summary",
//	                 "intent_type", "agent_id", "confidence",
//	                 "classifier_method", "handler_case", "task_id" }, ... ]
//	}
func (s *Server) handleSessionTrace(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "dispatch trace service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	params := map[string]any{"session_id": id}
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			s.writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		params["limit"] = limit
	}

	raw, err := json.Marshal(params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}

	result, err := s.rpcCall(r.Context(), "session.dispatch_trace", raw)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Normalize the envelope: the RPC result carries {entries, count};
	// echo the requested id so a client can correlate the call.
	//
	// In-process CallMethod returns the handler's Go value directly (typed
	// structs, e.g. []metrics.DispatchLogEntry), NOT the JSON-decoded
	// generic form the socket path yields. A raw m["entries"].([]any)
	// type-assertion therefore silently fails in-process and the envelope
	// stays empty (dispatch-trace HTTP twin RED while the RPC twin is
	// green). Round-trip through JSON so both transports normalize to
	// map[string]any/[]any before the assertion.
	envelope := map[string]any{
		"session_id": id,
		"entries":    []any{},
		"count":      0,
	}
	if raw, mErr := json.Marshal(result); mErr == nil {
		var m map[string]any
		if uErr := json.Unmarshal(raw, &m); uErr == nil {
			if entries, ok := m["entries"].([]any); ok {
				envelope["entries"] = entries
				envelope["count"] = len(entries)
			}
		}
	}

	s.writeJSON(w, http.StatusOK, envelope)
}
