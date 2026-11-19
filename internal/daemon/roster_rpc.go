package daemon

// roster_rpc.go wires the unified agents.roster RPC (plan tree
// tui-gui-consistency-fixes, leaves 01-backend/01 + 02). internal/employee
// imports internal/rpc, so the roster source closures must be built here in
// internal/daemon (which imports agent, bot, employee and rpc with no cycle).
//
// The three sources are split by identity, not duplicated:
//   - agents  = AgentRegistry.ListSpecs() (discovered AGENT.md specs)
//   - bots    = every BotManager row that is NOT a hired employee
//   - employees = EmployeeManager.ListEmployees() (bot rows with a constitution)
//
// ONE SNAPSHOT PER INVOCATION (bughunt 2026-11-19 L12): the bot/employee
// split used to read the employee list twice per roster call (the bot
// closure's exclusion set ran its own full ListEmployees, then the employee
// closure listed again), so a concurrent hire/fire could surface a row in
// BOTH lists or NEITHER. registerRosterRPC now takes ONE employee snapshot
// at handler entry and binds both sources to it. Seam choice: the rpc-layer
// handleRoster invokes the three injected sources independently on one
// goroutine, so the per-call cache lives in the daemon's own
// server.RegisterHandler closure — the RPC server dispatches each request
// synchronously on its connection goroutine (server.go handleConnection ->
// dispatch), so the closure body is exactly one agents.roster invocation
// and the snapshot never leaks across calls. The closure builds a
// lightweight capture rpc.Server (in-memory handler registry, no socket),
// registers the snapshot-bound sources through the REAL rpc.RegisterRosterRPC,
// and dispatches via CallMethod — the frozen C1 stamp order (agents -> bots
// -> employees, Type per source) and envelope shape stay canonical in
// internal/rpc, and that seam stays production-wired (no deadcode orphan).
//
// Populated C1 fields (leaf 02):
//   - active_count     = live AgentRegistry active-queue loops for that id
//   - daily_cost_cents = bot state TodayCostCents (bots + employees); agents
//                        have no per-agent cost source and stay at zero
//
// Never invent cost data: a missing bot state leaves cost at zero. Every
// domain handle is nil-guarded at call time so a partially-wired daemon
// degrades to a valid (possibly partial) roster, never a panic.

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/rpc"
)

// rosterEmployeeSnapshot is ONE invocation's employee view: the hired
// employees plus the id set the bot source uses for its exclusion split.
// Both derive from the SAME ListEmployees read, so a concurrent hire/fire
// can never put a row in both lists or drop it from both (bughunt L12).
// Constructed once per agents.roster call; read-only afterwards.
type rosterEmployeeSnapshot struct {
	employees []employee.Employee
	ids       map[string]struct{}
}

// takeRosterEmployeeSnapshot reads the hired-employee list ONCE. A missing
// EmployeeManager or a failed read degrades to an empty snapshot (the
// established fail-soft posture: the roster answers a valid, possibly
// partial list, never an error).
func takeRosterEmployeeSnapshot(c *Components, ctx context.Context, logger *slog.Logger) rosterEmployeeSnapshot {
	snap := rosterEmployeeSnapshot{ids: make(map[string]struct{})}
	if c == nil || c.EmployeeManager == nil {
		return snap
	}
	emps, err := c.EmployeeManager.ListEmployees(ctx, "")
	if err != nil {
		logger.Debug("agents.roster: list employees failed", "error", err)
		return snap
	}
	snap.employees = emps
	for _, emp := range emps {
		snap.ids[emp.ID] = struct{}{}
	}
	return snap
}

// registerRosterRPC registers agents.roster on server with real source
// closures built over the live component handles. Nil-safe: a nil server is
// a no-op, and each source returns an empty slice when its handle is
// unwired. Registration is unconditional (like acp.list / intake.status) so
// the method always exists and answers a valid {"agents": []} envelope.
func registerRosterRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	// The domain list methods are short SQLite reads that do not outlive
	// the call; a fresh background context per read keeps the source shape
	// independent of the RPC request ctx (the pre-L12 posture).
	rosterCtx := context.Background()

	listAgents := func() []rpc.RosterEntry {
		if c == nil || c.AgentRegistry == nil {
			return []rpc.RosterEntry{}
		}
		specs := c.AgentRegistry.ListSpecs()
		rows := make([]rpc.RosterEntry, 0, len(specs))
		for _, spec := range specs {
			if spec == nil {
				continue
			}
			status := "running"
			if !spec.Enabled {
				status = "stopped"
			}
			rows = append(rows, rpc.RosterEntry{
				ID:          spec.ID,
				Name:        spec.Name,
				Status:      status,
				ActiveCount: c.AgentRegistry.ActiveLoopCount(spec.ID),
				// No per-agent daily cost source: stay at zero.
				Role:        string(spec.Role),
				Description: spec.Description,
				Enabled:     spec.Enabled,
			})
		}
		return rows
	}

	listBots := func(snap rosterEmployeeSnapshot) []rpc.RosterEntry {
		if c == nil || c.BotManager == nil {
			return []rpc.RosterEntry{}
		}
		bots, err := c.BotManager.ListBots(rosterCtx)
		if err != nil {
			logger.Debug("agents.roster: list bots failed", "error", err)
			return []rpc.RosterEntry{}
		}

		rows := make([]rpc.RosterEntry, 0, len(bots))
		for _, def := range bots {
			// Exclude hired employees from the bot source: they are surfaced
			// by the employee source from the SAME snapshot, so the split is
			// consistent even under a concurrent hire/fire (bughunt L12).
			if _, isEmployee := snap.ids[def.ID]; isEmployee {
				continue
			}
			rows = append(rows, rosterBotEntry(c, rosterCtx, def))
		}
		return rows
	}

	listEmployees := func(snap rosterEmployeeSnapshot) []rpc.RosterEntry {
		rows := make([]rpc.RosterEntry, 0, len(snap.employees))
		for _, emp := range snap.employees {
			// The snapshot comes from ListEmployees, which already filters
			// out plain bots (rows without a constitution): every row here
			// is a hired employee.
			rows = append(rows, rosterBotEntry(c, rosterCtx, emp.BotDefinition))
		}
		return rows
	}

	// Per-invocation snapshot seam (see file header): ONE employee read at
	// handler entry, both bot/employee sources bound to it, then the merge
	// runs through the REAL rpc.RegisterRosterRPC / RosterHandler so the
	// frozen C1 stamp order + envelope shape stay canonical in internal/rpc
	// (no re-implementation here, and the rpc seam stays production-wired).
	// The capture server is a lightweight in-memory registry (no socket, no
	// Start): RegisterRosterRPC only writes its handler map, and CallMethod
	// dispatches it synchronously — exactly one merge per agents.roster call.
	server.RegisterHandler("agents.roster", func(ctx context.Context, params json.RawMessage) (any, error) {
		snap := takeRosterEmployeeSnapshot(c, rosterCtx, logger)
		capture := rpc.New(&rpc.Config{SocketPath: ""}, nil, logger)
		rpc.RegisterRosterRPC(capture, rpc.RosterDeps{
			ListAgents:    listAgents,
			ListBots:      func() []rpc.RosterEntry { return listBots(snap) },
			ListEmployees: func() []rpc.RosterEntry { return listEmployees(snap) },
		})
		return capture.CallMethod(ctx, "agents.roster", params)
	})
	logger.Info("agents.roster RPC handler registered")
}

// rosterBotEntry maps one bot/employee definition onto a C1 row with the
// live status + daily-cost overlay (shared by both sources so the row shape
// stays identical). A missing/unreadable bot state keeps the stopped
// fallback and zero cost — never invent data.
func rosterBotEntry(c *Components, ctx context.Context, def bot.BotDefinition) rpc.RosterEntry {
	row := rpc.RosterEntry{
		ID:          def.ID,
		Name:        def.Name,
		Status:      string(botStatusStopped),
		Description: def.Description,
		Enabled:     def.Enabled,
	}
	if c.AgentRegistry != nil {
		row.ActiveCount = c.AgentRegistry.ActiveLoopCount(def.ID)
	}
	if c.BotManager != nil {
		if state, stateErr := c.BotManager.GetBotStatus(ctx, def.ID); stateErr == nil && state != nil {
			row.Status = string(state.Status)
			row.DailyCostCents = state.TodayCostCents
		}
	}
	return row
}

// botStatusStopped is the C1 fallback status for a bot/employee whose state
// is not yet readable. It matches bot.BotStatusStopped / employee status; a
// local constant keeps this wiring file free of domain-package imports.
const botStatusStopped = "stopped"
