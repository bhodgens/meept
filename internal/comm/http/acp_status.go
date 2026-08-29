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
