package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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

// RegisterACPMethods registers acp.list plus the human relay permission
// surface (acp-human-permission-relay leaf 03, Contract C) on the server.
// All three methods register even when the manager is nil so HTTP clients
// get the disabled envelope / a not-found error instead of method-not-found.
func (h *ACPHandler) RegisterACPMethods(server *Server) {
	server.RegisterHandler("acp.list", h.handleList)
	server.RegisterHandler("acp.permissions.list", h.handlePermissionsList)
	server.RegisterHandler("acp.permissions.resolve", h.handlePermissionsResolve)
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

// ACPPendingPermissionStatus is one still-answerable human relay request
// (frozen keys: id, agent_id, session_id, summary, created_at).
type ACPPendingPermissionStatus struct {
	ID        string    `json:"id" yaml:"id"`
	AgentID   string    `json:"agent_id" yaml:"agent_id"`
	SessionID string    `json:"session_id" yaml:"session_id"`
	Summary   string    `json:"summary" yaml:"summary"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
}

// ACPPermissionsResult is the envelope returned by acp.permissions.list and
// GET /api/v1/acp/permissions. Enabled reports whether the manager carries
// an armed human relay seam (registry + notifier); Pending is always a
// non-nil slice.
type ACPPermissionsResult struct {
	Enabled bool                         `json:"enabled" yaml:"enabled"`
	Pending []ACPPendingPermissionStatus `json:"pending" yaml:"pending"`
}

// ACPPermissionResolveResult echoes the accepted verdict
// (acp.permissions.resolve reply: decision + reason per the frozen leaf
// contract).
type ACPPermissionResolveResult struct {
	ID       string `json:"id" yaml:"id"`
	Decision string `json:"decision" yaml:"decision"`
	Reason   string `json:"reason" yaml:"reason"`
}

func (h *ACPHandler) handlePermissionsList(_ context.Context, _ json.RawMessage) (any, error) {
	return h.permissionsList(), nil
}

func (h *ACPHandler) permissionsList() ACPPermissionsResult {
	empty := ACPPermissionsResult{Enabled: false, Pending: []ACPPendingPermissionStatus{}}
	if h == nil || h.manager == nil || !h.manager.Enabled() {
		return empty
	}
	pending := h.manager.PendingPermissions()
	out := make([]ACPPendingPermissionStatus, 0, len(pending))
	for _, p := range pending {
		out = append(out, ACPPendingPermissionStatus{
			ID:        p.ID,
			AgentID:   p.AgentID,
			SessionID: p.SessionID,
			Summary:   p.Summary,
			CreatedAt: p.CreatedAt,
		})
	}
	// Enabled mirrors HasPermissionRelay: the pending list only ever fills
	// when BOTH relay halves are wired (leaf 02's setter contract), so a
	// manager without the seam reports the disabled envelope.
	return ACPPermissionsResult{Enabled: h.manager.HasPermissionRelay(), Pending: out}
}

// handlePermissionsResolve answers {id, decision}. The decision verb is
// validated HERE, at the RPC boundary — the relay's internal normalize
// ("anything not allow/always -> deny") must never turn a typo into a
// silent deny on a user-facing surface. "always" is a human-only verb
// (acp-always-allow leaf 01): the reply echo shape is unchanged and the
// reason stays "answered".
func (h *ACPHandler) handlePermissionsResolve(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	p.ID = strings.TrimSpace(p.ID)
	p.Decision = strings.ToLower(strings.TrimSpace(p.Decision))
	if p.ID == "" {
		return nil, fmt.Errorf("invalid params: id is required")
	}
	if p.Decision != "allow" && p.Decision != "deny" && p.Decision != "always" {
		return nil, fmt.Errorf("invalid params: decision must be \"allow\", \"deny\", or \"always\"")
	}
	if h == nil || h.manager == nil {
		return nil, fmt.Errorf("permission not found: %s", p.ID)
	}
	if err := h.manager.ResolvePermission(p.ID, p.Decision); err != nil {
		switch {
		case errors.Is(err, acp.ErrRelayNotFound):
			return nil, fmt.Errorf("permission not found: %s", p.ID)
		case errors.Is(err, acp.ErrRelayAnswered):
			return nil, fmt.Errorf("permission already answered: %s", p.ID)
		default:
			return nil, err
		}
	}
	return ACPPermissionResolveResult{ID: p.ID, Decision: p.Decision, Reason: "answered"}, nil
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
