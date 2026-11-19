package rpc

// registry.go — registration seam for the unified agents.roster RPC (plan
// tree tui-gui-consistency-fixes, leaf 01-backend/01-roster-rpc.md).
//
// WHY A SEAM: internal/employee imports internal/rpc (employee/handler_auth.go),
// so internal/rpc may NOT import employee (nor the agent/bot domain types in
// one coherent wiring step) without an import cycle. The real sources are
// therefore injected as neutral `func() []RosterEntry` closures built by the
// DAEMON (internal/daemon imports agent, bot, employee and rpc — no cycle),
// which calls RegisterRosterRPC with those closures. The handler itself lives
// in roster.go and is fully exercised by the committed gate test.
//
// Registration is deliberately a plain function rather than a package init:
// the repo registers every RPC method from daemon wiring via explicit
// `RegisterXMethods(server)` / `RegisterXRPC(...)` calls, and there is no
// central register-all registry in internal/rpc. A caller that never wires
// the roster simply never serves agents.roster — same posture as the other
// opt-in surfaces (kanban, intake.status).

// RegisterRosterRPC builds a RosterHandler from the injected source closures
// and registers agents.roster on server. It is the cycle-free seam the daemon
// uses to dispatch agents.roster against real data sources.
//
// Nil-safe: a nil server is a no-op. A nil source func inside deps yields no
// rows (see RosterHandler.handleRoster), so a partially-wired daemon still
// answers with a valid, possibly-partial roster.
//
// Intended daemon-side call site (in internal/daemon, OUTSIDE this leaf — the
// daemon can import agent/bot/employee without a cycle). Sketch:
//
//	rpc.RegisterRosterRPC(rpcServer, rpc.RosterDeps{
//	    ListAgents: func() []rpc.RosterEntry {
//	        rows := make([]rpc.RosterEntry, 0, len(c.AgentRegistry.ListSpecs()))
//	        for _, spec := range c.AgentRegistry.ListSpecs() {
//	            role := string(spec.Role)
//	            rows = append(rows, rpc.RosterEntry{
//	                ID: spec.ID, Name: spec.Name, Role: role,
//	                Description: spec.Description, Enabled: spec.Enabled,
//	            })
//	        }
//	        return rows
//	    },
//	    ListBots: func() []rpc.RosterEntry { /* map c.BotManager.ListBots(ctx) */ },
//	    ListEmployees: func() []rpc.RosterEntry { /* map c.EmployeeManager.ListEmployees(ctx, "") */ },
//	})
//
// (active_count / daily_cost_cents population is leaf 02's scope; this seam
// only needs the source closures.)
func RegisterRosterRPC(server *Server, deps RosterDeps) {
	if server == nil {
		return
	}
	h := NewRosterHandler(deps)
	h.RegisterRosterMethods(server)
}
