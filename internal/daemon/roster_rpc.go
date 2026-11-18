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
	"log/slog"

	"github.com/caimlas/meept/internal/rpc"
)

// registerRosterRPC registers agents.roster on server with real source
// closures built over the live component handles. Nil-safe: a nil server is
// a no-op, and each closure returns an empty slice when its source is
// unwired. Registration is unconditional (like acp.list / intake.status) so
// the method always exists and answers a valid {"agents": []} envelope.
func registerRosterRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	// The source funcs on RosterDeps carry no context, so each list call uses
	// a fresh background context (the domain list methods are short SQLite
	// reads; they do not outlive the call).
	rosterCtx := context.Background()

	deps := rpc.RosterDeps{
		ListAgents: func() []rpc.RosterEntry {
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
		},

		ListBots: func() []rpc.RosterEntry {
			if c == nil || c.BotManager == nil {
				return []rpc.RosterEntry{}
			}
			bots, err := c.BotManager.ListBots(rosterCtx)
			if err != nil {
				logger.Debug("agents.roster: list bots failed", "error", err)
				return []rpc.RosterEntry{}
			}

			// Exclude hired employees from the bot source: they are surfaced
			// by ListEmployees below. ListEmployees returns EVERY bot row
			// (it has no constitution filter with an empty status filter),
			// so the split is by the constitution marker: only rows with a
			// real constitution are employees; plain bots stay bots.
			employeeIDs := rosterEmployeeIDs(c, rosterCtx, logger)

			rows := make([]rpc.RosterEntry, 0, len(bots))
			for _, def := range bots {
				if _, isEmployee := employeeIDs[def.ID]; isEmployee {
					continue
				}
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
				if state, stateErr := c.BotManager.GetBotStatus(rosterCtx, def.ID); stateErr == nil && state != nil {
					row.Status = string(state.Status)
					row.DailyCostCents = state.TodayCostCents
				}
				rows = append(rows, row)
			}
			return rows
		},

		ListEmployees: func() []rpc.RosterEntry {
			if c == nil || c.EmployeeManager == nil {
				return []rpc.RosterEntry{}
			}
			emps, err := c.EmployeeManager.ListEmployees(rosterCtx, "")
			if err != nil {
				logger.Debug("agents.roster: list employees failed", "error", err)
				return []rpc.RosterEntry{}
			}
			rows := make([]rpc.RosterEntry, 0, len(emps))
			for _, emp := range emps {
				// ListEmployees returns every bot row; only rows with a real
				// constitution are hired employees (the plain-bot split).
				if !emp.HasConstitution() {
					continue
				}
				row := rpc.RosterEntry{
					ID:          emp.ID,
					Name:        emp.Name,
					Status:      string(botStatusStopped),
					Description: emp.Description,
					Enabled:     emp.Enabled,
				}
				if c.AgentRegistry != nil {
					row.ActiveCount = c.AgentRegistry.ActiveLoopCount(emp.ID)
				}
				if c.BotManager != nil {
					if state, stateErr := c.BotManager.GetBotStatus(rosterCtx, emp.ID); stateErr == nil && state != nil {
						row.Status = string(state.Status)
						row.DailyCostCents = state.TodayCostCents
					}
				}
				rows = append(rows, row)
			}
			return rows
		},
	}

	rpc.RegisterRosterRPC(server, deps)
	logger.Info("agents.roster RPC handler registered")
}

// rosterEmployeeIDs returns the set of hired-employee ids (bot rows carrying
// a real constitution). Used by the bot source to avoid emitting the same
// row twice — hired employees belong to the employee source, plain bots to
// the bot source. A missing EmployeeManager yields an empty set.
func rosterEmployeeIDs(c *Components, ctx context.Context, logger *slog.Logger) map[string]struct{} {
	ids := make(map[string]struct{})
	if c == nil || c.EmployeeManager == nil {
		return ids
	}
	emps, err := c.EmployeeManager.ListEmployees(ctx, "")
	if err != nil {
		logger.Debug("agents.roster: list employees for bot split failed", "error", err)
		return ids
	}
	for _, emp := range emps {
		if emp.HasConstitution() {
			ids[emp.ID] = struct{}{}
		}
	}
	return ids
}

// botStatusStopped is the C1 fallback status for a bot/employee whose state
// is not yet readable. It matches bot.BotStatusStopped / employee status; a
// local constant keeps this wiring file free of domain-package imports.
const botStatusStopped = "stopped"
