package rpc

import (
	"context"
	"encoding/json"

	"github.com/caimlas/meept/internal/acp"
)

// ACPListResult is the envelope returned by acp.list and GET /api/v1/acp/agents.
type ACPListResult struct {
	Enabled bool             `json:"enabled" yaml:"enabled"`
	Agents  []ACPAgentStatus `json:"agents" yaml:"agents"`
}

// ACPAgentStatus is one catalog agent plus live-session fields.
type ACPAgentStatus struct {
	ID      string `json:"id" yaml:"id"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Running bool   `json:"running" yaml:"running"`
	State   string `json:"state" yaml:"state"`
	UptimeS int    `json:"uptime_s" yaml:"uptime_s"`
}

// ACPHandler provides the acp.list RPC method.
// A nil manager is valid and reports the disabled envelope.
type ACPHandler struct {
	manager *acp.Manager
}

// NewACPHandler creates a handler. manager may be nil.
func NewACPHandler(mgr *acp.Manager) *ACPHandler {
	return &ACPHandler{manager: mgr}
}

// RegisterACPMethods registers acp.list on the server.
func (h *ACPHandler) RegisterACPMethods(server *Server) {
	server.RegisterHandler("acp.list", h.handleList)
}

func (h *ACPHandler) handleList(_ context.Context, _ json.RawMessage) (any, error) {
	return h.list(), nil
}

func (h *ACPHandler) list() ACPListResult {
	empty := ACPListResult{Enabled: false, Agents: []ACPAgentStatus{}}
	if h == nil || h.manager == nil || !h.manager.Enabled() {
		return empty
	}

	live := h.manager.LiveSessions()
	catalog := h.manager.Agents()
	agents := make([]ACPAgentStatus, 0, len(catalog))
	for _, entry := range catalog {
		st := ACPAgentStatus{
			ID:      entry.ID,
			Enabled: entry.Enabled,
		}
		if ss, ok := live[entry.ID]; ok {
			st.State = acpStateString(ss)
			st.Running = ss != acp.StateClosed
		}
		agents = append(agents, st)
	}
	return ACPListResult{Enabled: true, Agents: agents}
}

func acpStateString(st acp.SessionState) string {
	switch st {
	case acp.StateStarting:
		return "starting"
	case acp.StateReady:
		return "ready"
	case acp.StateBusy:
		return "busy"
	case acp.StateClosed:
		return "closed"
	default:
		return ""
	}
}
