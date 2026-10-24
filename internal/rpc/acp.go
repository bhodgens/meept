package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/acp"
)

// ErrACPSelfApproval is returned when the caller of acp.permissions.resolve
// (or .list) is the daemon itself or a process the daemon spawned — i.e. an
// agent answering its own permission request (audit M5). The relay stays
// untouched: the pending request remains answerable by a real human surface
// until its timeout.
var ErrACPSelfApproval = errors.New("acp: caller is daemon-spawned; self-approval is not permitted")

// ACPListResult is the envelope returned by acp.list and GET /api/v1/acp/agents.
type ACPListResult struct {
	Enabled bool             `json:"enabled" yaml:"enabled"`
	Agents  []ACPAgentStatus `json:"agents" yaml:"agents"`
}

// ACPAgentStatus is one catalog agent plus live-session fields.
//
// Personas is the additive per-persona detail (acp-employees persona lane):
// the live persona names for this agent, sorted. It is OMITTED (never [])
// on the no-persona-only wire shape, exactly like the permission surface's
// persona key, so pre-persona consumers see byte-identical rows.
type ACPAgentStatus struct {
	ID       string   `json:"id" yaml:"id"`
	Enabled  bool     `json:"enabled" yaml:"enabled"`
	Running  bool     `json:"running" yaml:"running"`
	State    string   `json:"state" yaml:"state"`
	UptimeS  int      `json:"uptime_s" yaml:"uptime_s"`
	Personas []string `json:"personas,omitempty" yaml:"personas,omitempty"`
}

// ACPHandler provides the acp.list RPC method.
// A nil manager is valid and reports the disabled envelope.
type ACPHandler struct {
	manager *acp.Manager
	// enforceApprover gates acp.permissions.resolve/.list on caller
	// identity (audit M5). Default true: daemon-spawned peers (agent-
	// controlled processes) are rejected with ErrACPSelfApproval and
	// peers whose identity cannot be verified fail closed. Set false
	// only by [acp] approver_identity="off" (documented escape hatch).
	enforceApprover bool
	logger          *slog.Logger
}

// NewACPHandler creates a handler. manager may be nil. The approver
// identity gate is ON by default (fail-closed); see enforceApprover.
func NewACPHandler(mgr *acp.Manager) *ACPHandler {
	return &ACPHandler{manager: mgr, enforceApprover: true, logger: slog.Default()}
}

// SetApproverEnforcement disables the self-approval gate. Wiring:
// [acp] approver_identity="off" (daemon startup only).
func (h *ACPHandler) SetApproverEnforcement(enforce bool) {
	if h == nil {
		return
	}
	h.enforceApprover = enforce
}

// SetLogger overrides the handler logger (nil restores slog.Default).
func (h *ACPHandler) SetLogger(l *slog.Logger) {
	if h == nil {
		return
	}
	if l == nil {
		l = slog.Default()
	}
	h.logger = l
}

// checkApprover enforces the self-approval gate (audit M5) for a
// permission-surface call. It returns (approver, nil) when the caller may
// proceed, or an error to reject:
//
//   - Socket callers (PeerIdentity in ctx): reject when the peer is the
//     daemon or a daemon descendant (agent-controlled: shell-tool children,
//     PTY sessions, MCP servers are ALL daemon-spawned). Human surfaces
//     (TUI, menubar, GUI-over-RPC) are not daemon descendants and pass.
//     Fail closed when the peer PID could not be verified.
//   - In-process callers (no PeerIdentity — the HTTP boundary dispatches
//     via CallMethod): allowed ONLY when they carry an explicit
//     acp.Approver (the HTTP handler attaches the key fingerprint);
//     an in-process call with no approver is an internal daemon path and
//     is rejected — internal code must never answer a human gate.
func (h *ACPHandler) checkApprover(ctx context.Context) (acp.Approver, error) {
	if !h.enforceApprover {
		// Gate off: still attribute when identity is available.
		if peer, ok := PeerIdentityFromContext(ctx); ok && peer.HasPID {
			return acp.Approver{Kind: "peercred", PID: peer.PID, UID: peer.UID}, nil
		}
		if a, ok := acp.ApproverFromContext(ctx); ok {
			return a, nil
		}
		return acp.Approver{Kind: "in-process"}, nil
	}

	if peer, ok := PeerIdentityFromContext(ctx); ok {
		if !peer.HasPID {
			// Platform cannot verify the peer (e.g. the peercred stub):
			// fail closed — an unverifiable caller must not answer a
			// human gate.
			return acp.Approver{}, fmt.Errorf("%w (peer identity unverifiable on this platform)", ErrACPSelfApproval)
		}
		if peer.SpawnedByDaemon {
			h.logger.Warn("acp: rejected self-approval attempt",
				"component", "acp",
				"peer_pid", peer.PID,
				"peer_uid", peer.UID,
			)
			return acp.Approver{}, ErrACPSelfApproval
		}
		return acp.Approver{Kind: "peercred", PID: peer.PID, UID: peer.UID}, nil
	}

	// No socket peer: in-process dispatch (HTTP boundary). Require an
	// explicit approver attached by that boundary.
	if a, ok := acp.ApproverFromContext(ctx); ok && a.Kind != "" {
		return a, nil
	}
	return acp.Approver{}, fmt.Errorf("%w (in-process caller without approver identity)", ErrACPSelfApproval)
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
		// Running-state spans EVERY lane of the catalog entry.
		// Manager.LiveSessions externalizes the composite NUL session
		// keys as bare "agentID" (no-persona lane) or "agentID/persona"
		// (persona lanes — internal/acp/manager.go sessionLabel), and the
		// employee executor chain always spawns via GetOrCreatePersona, so
		// keying the lookup by the bare id alone never surfaced persona
		// sessions as running (2026-10-23 e2e leaf report). Catalog agent
		// ids contain no '/', so the "id/" prefix is unambiguous.
		if ss, ok := live[entry.ID]; ok {
			st.State = acpStateString(ss)
			st.Running = ss != acp.StateClosed
		}
		prefix := entry.ID + "/"
		var personas []string
		for label, ss := range live {
			name, found := strings.CutPrefix(label, prefix)
			if !found || name == "" {
				continue
			}
			personas = append(personas, name)
			if ss != acp.StateClosed {
				st.Running = true
			}
		}
		sort.Strings(personas)
		st.Personas = personas
		agents = append(agents, st)
	}
	return ACPListResult{Enabled: true, Agents: agents}
}

// ACPPendingPermissionStatus is one still-answerable human relay request
// (frozen keys: id, agent_id, session_id, summary, created_at; plus the
// additive options key — the agent-offered {optionId,name,kind} labels —
// from acp-always-allow leaf 01, so list consumers (GUI catch-up after a
// reconnect missed the requested frame) can render the allow_always
// affordance too; plus the additive persona key from the permission
// surfaces phase-2 work — the multi-session persona that asked, empty on
// the no-persona lane so old consumers see the exact prior wire shape).
type ACPPendingPermissionStatus struct {
	ID        string                 `json:"id" yaml:"id"`
	AgentID   string                 `json:"agent_id" yaml:"agent_id"`
	Persona   string                 `json:"persona,omitempty" yaml:"persona,omitempty"`
	SessionID string                 `json:"session_id" yaml:"session_id"`
	Summary   string                 `json:"summary" yaml:"summary"`
	CreatedAt time.Time              `json:"created_at" yaml:"created_at"`
	Options   []acp.PermissionOption `json:"options,omitempty" yaml:"options,omitempty"`
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

func (h *ACPHandler) handlePermissionsList(ctx context.Context, _ json.RawMessage) (any, error) {
	// Gate the pending-id surface too (audit M5): a daemon-spawned caller
	// must not even enumerate the ids it could try to resolve. Rejected
	// callers get the disabled envelope, not an error — list is a
	// status/refresh surface and an agent polling it should see "nothing
	// for you", never a hint that a gate exists to probe.
	if h.enforceApprover {
		if _, err := h.checkApprover(ctx); err != nil {
			return ACPPermissionsResult{Enabled: false, Pending: []ACPPendingPermissionStatus{}}, nil
		}
	}
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
			Persona:   p.Persona,
			SessionID: p.SessionID,
			Summary:   p.Summary,
			CreatedAt: p.CreatedAt,
			Options:   p.Options,
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
//
// Approver identity (audit M5): the caller must NOT be the daemon or a
// daemon-spawned process (an agent answering its own request). Socket
// callers are gated on the kernel-verified peer PID (accept-time ancestry
// walk); in-process callers (HTTP dispatch) must carry an explicit
// acp.Approver. The resolved approver rides on the relay decision into the
// permission_resolved notification and the bus event.
func (h *ACPHandler) handlePermissionsResolve(ctx context.Context, params json.RawMessage) (any, error) {
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
	approver, err := h.checkApprover(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.manager.ResolvePermissionWithApprover(ctx, p.ID, p.Decision, approver); err != nil {
		switch {
		case errors.Is(err, acp.ErrRelayNotFound):
			return nil, fmt.Errorf("permission not found: %s", p.ID)
		case errors.Is(err, acp.ErrRelayAnswered):
			return nil, fmt.Errorf("permission already answered: %s", p.ID)
		case errors.Is(err, acp.ErrResolveRanked):
			// Tighten-only decision-model rank (audit M5 Phase 2b-rank):
			// a confident "block" second opinion rejected this allow. The
			// pending request is untouched and stays answerable.
			return nil, fmt.Errorf("allow rejected by decision-model rank; the request is still pending and can be denied or re-approved: %w", err)
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
