package http

import (
	"encoding/json"
	"net/http"
	"strings"
)

// acpStatusList is the GET /api/v1/acp/agents envelope.
type acpStatusList struct {
	Enabled bool           `json:"enabled" yaml:"enabled"`
	Agents  []acpStatusRow `json:"agents" yaml:"agents"`
}

type acpStatusRow struct {
	ID      string `json:"id" yaml:"id"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Running bool   `json:"running" yaml:"running"`
	State   string `json:"state" yaml:"state"`
	UptimeS int    `json:"uptime_s" yaml:"uptime_s"`
}

func acpDisabledStatus() acpStatusList {
	return acpStatusList{Enabled: false, Agents: []acpStatusRow{}}
}

// handleACPAgentsList handles GET /api/v1/acp/agents.
// Nil rpcCall (or a disabled manager behind acp.list) returns 200 with the
// disabled envelope — never 500.
func (s *Server) handleACPAgentsList(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeJSON(w, http.StatusOK, acpDisabledStatus())
		return
	}
	result, err := s.rpcCall(r.Context(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

// handleACPPermissionsList handles GET /api/v1/acp/permissions — the human
// relay pending snapshot (acp-human-permission-relay leaf 03, Contract C).
// Nil rpcCall returns 200 with the disabled envelope (acp_status.go
// precedent), never 500.
func (s *Server) handleACPPermissionsList(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeJSON(w, http.StatusOK, acpDisabledPermissions())
		return
	}
	result, err := s.rpcCall(r.Context(), "acp.permissions.list", json.RawMessage("{}"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

// acpPermissionsList is the GET /api/v1/acp/permissions disabled envelope
// (pending always an empty array, never null — TUI/GUI decode shapes).
type acpPermissionsList struct {
	Enabled bool            `json:"enabled" yaml:"enabled"`
	Pending []acpPendingRow `json:"pending" yaml:"pending"`
}

// acpPendingRow is one pending human relay request in the HTTP envelope
// (frozen keys: id, agent_id, session_id, summary, created_at).
type acpPendingRow struct {
	ID        string `json:"id" yaml:"id"`
	AgentID   string `json:"agent_id" yaml:"agent_id"`
	SessionID string `json:"session_id" yaml:"session_id"`
	Summary   string `json:"summary" yaml:"summary"`
	CreatedAt string `json:"created_at" yaml:"created_at"`
}

func acpDisabledPermissions() acpPermissionsList {
	return acpPermissionsList{Enabled: false, Pending: []acpPendingRow{}}
}

// handleACPPermissionsResolve handles POST /api/v1/acp/permissions/resolve.
// Body: {"id": "...", "decision": "allow"|"deny"|"always"}. The verb is
// validated here too (400 on a bad body) so the GUI/CLI never pays an RPC
// round trip for a typo, and the relay never sees a normalizable-but-wrong
// string: explicit allow|deny|always only ("always" is the human-only
// acp-always-allow verb, leaf 01).
func (s *Server) handleACPPermissionsResolve(w http.ResponseWriter, r *http.Request) {
	if s.rpcCall == nil {
		s.writeError(w, http.StatusServiceUnavailable, "acp service not available")
		return
	}
	var body struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	body.Decision = strings.ToLower(strings.TrimSpace(body.Decision))
	if body.ID == "" || (body.Decision != "allow" && body.Decision != "deny" && body.Decision != "always") {
		s.writeError(w, http.StatusBadRequest, "id and decision (allow|deny|always) are required")
		return
	}
	params, err := json.Marshal(body)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to encode request")
		return
	}
	result, err := s.rpcCall(r.Context(), "acp.permissions.resolve", params)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, strings.ToLower(err.Error()))
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}
