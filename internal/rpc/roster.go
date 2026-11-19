package rpc

// roster.go — the unified agents.roster RPC (plan tree
// tui-gui-consistency-fixes, leaf 01-backend/01-roster-rpc.md). It merges the
// three daemon roster sources (discovered registry agents, bots, employees)
// into one C1 row list under {"agents": [...]}.
//
// LAYERING / IMPORT CYCLE: internal/employee imports internal/rpc
// (employee/handler_auth.go), so internal/rpc may NOT import the concrete
// employee/agent/bot domain types. The sources are therefore injected as
// neutral `func() []RosterEntry` seams; daemon wiring (internal/daemon imports
// all four packages) supplies the real closures that map domain types to C1
// rows. See RegisterRosterRPC in registry.go.

import (
	"context"
	"encoding/json"
)

// Frozen C1 type discriminator values. The handler stamps these onto every
// row from its corresponding source; a source never has to set Type itself.
const (
	RosterTypeAgent    = "agent"
	RosterTypeBot      = "bot"
	RosterTypeEmployee = "employee"
)

// RosterEntry is one row of the unified agents.roster response. It carries
// EXACTLY the nine frozen C1 JSON keys — no extras leak onto the wire. Role
// and Description are plain (no omitempty) so a fully-populated row always
// marshals all nine keys, and an empty one still marshals an empty string
// rather than dropping the key (predictable for the TUI/GUI consumers).
type RosterEntry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	ActiveCount    int    `json:"active_count"`
	DailyCostCents int    `json:"daily_cost_cents"`
	Role           string `json:"role"`
	Description    string `json:"description"`
	Enabled        bool   `json:"enabled"`
}

// RosterDeps holds the three injected source funcs. Every field is optional:
// a nil source func yields no rows (nil-safe), so a partially-wired daemon
// still answers agents.roster with a valid (possibly partial) roster — never
// a panic and never "method not found".
type RosterDeps struct {
	ListAgents    func() []RosterEntry
	ListBots      func() []RosterEntry
	ListEmployees func() []RosterEntry
}

// RosterHandler serves agents.roster from the injected sources.
type RosterHandler struct {
	deps RosterDeps
}

// NewRosterHandler builds a handler from the injected sources. All deps may
// be nil; handleRoster is nil-safe on both the handler and each source func.
func NewRosterHandler(deps RosterDeps) *RosterHandler {
	return &RosterHandler{deps: deps}
}

// handleRoster merges the three sources into one roster, stamping Type with
// the frozen discriminator for each source. Only Type is overwritten; every
// other field (ID, Name, Status, ActiveCount, DailyCostCents, Role,
// Description, Enabled) is preserved from the source row. Params are ignored
// (read-only list), and a nil handler/sources produce an empty (non-nil)
// slice so the wire shape is always {"agents": []}.
func (h *RosterHandler) handleRoster(_ context.Context, _ json.RawMessage) (any, error) {
	rows := make([]RosterEntry, 0)
	if h == nil {
		return map[string]any{"agents": rows}, nil
	}

	stamp := func(src func() []RosterEntry, typ string) {
		if src == nil {
			return
		}
		for _, row := range src() {
			row.Type = typ
			rows = append(rows, row)
		}
	}
	stamp(h.deps.ListAgents, RosterTypeAgent)
	stamp(h.deps.ListBots, RosterTypeBot)
	stamp(h.deps.ListEmployees, RosterTypeEmployee)

	return map[string]any{"agents": rows}, nil
}

// RegisterRosterMethods registers agents.roster on server. Safe to call with
// a nil handler (registers the empty-roster envelope) or a nil server (no-op).
func (h *RosterHandler) RegisterRosterMethods(server *Server) {
	if server == nil {
		return
	}
	server.RegisterHandler("agents.roster", h.handleRoster)
}
