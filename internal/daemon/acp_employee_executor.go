package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/pkg/id"
)

// acpTurnRunner is the minimal loop seam the acp-backed employee executor
// needs: one user message in, final text out. *agent.AgentLoop satisfies it
// (RunOnce delegates to RunOnceWithParts, which carries the shared
// reply-guard / output-filter / security-scan lane — the reason D2-B routes
// through the loop instead of calling ACPExecutor.Run directly; see
// docs/workflows/acp-employees-phase3.md).
type acpTurnRunner interface {
	RunOnce(ctx context.Context, userMessage, conversationID string) (string, error)
}

// acpEmployeeExecutorAdapter implements bot.BotExecutor for an employee
// whose BotDefinition.ExecutorAgent names an acp-backed registry agent
// definition (acp-employees phase 3, D1-A/D2-B). Each EXECUTE turn routes
// the userMessage through the REFERENCED agent's loop RunOnce — never
// RunOnceWithParts — so harness output earns the same reply guard, output
// filters, and security scan as a local employee turn.
//
// Token counts are not available on the acp wire (session/prompt returns
// stopReason only — docs/workflows/acp-employees-phase3.md D4), so
// tokensUsed is always 0, mirroring the agentLoopBotExecutorAdapter
// precedent (employee_service_adapter.go: ExecuteBot returns 0 with the
// rationale comment).
type acpEmployeeExecutorAdapter struct {
	employeeID string
	agentID    string
	// resolveLoop returns the referenced agent's loop. Built from the
	// agent registry at wiring time; a function (not the registry
	// concrete type) keeps the adapter unit-testable without a registry.
	resolveLoop func(agentID string) (acpTurnRunner, error)
	logger      *slog.Logger
}

// ExecuteBot runs a single employee turn through the referenced agent's
// loop. The systemPrompt is unused-by-design: the loop constructs its own
// system prompts internally (mirrors the agentLoopBotExecutorAdapter
// comment in employee_service_adapter.go); the constitution prompt stays
// advisory for acp employees, matching the phase-3 doc (the persona
// profile already carries harness voice). Returns (output, 0, err).
func (a *acpEmployeeExecutorAdapter) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	if a.resolveLoop == nil {
		return "", 0, fmt.Errorf("acp employee executor %q: loop resolver not configured", a.employeeID)
	}
	loop, err := a.resolveLoop(a.agentID)
	if err != nil {
		return "", 0, fmt.Errorf("acp employee executor %q: resolve agent %q: %w", a.employeeID, a.agentID, err)
	}
	if loop == nil {
		return "", 0, fmt.Errorf("acp employee executor %q: agent %q resolved to a nil loop", a.employeeID, a.agentID)
	}
	// Conversation id scoped like the local adapter's so employee sessions
	// never collide with user-driven conversations.
	conversationID := id.Generate("bot-exec")
	response, err := loop.RunOnce(ctx, userMessage, conversationID)
	if err != nil {
		a.logger.Warn("acp employee executor turn failed",
			"employee_id", a.employeeID,
			"executor_agent", a.agentID,
			"conversation_id", conversationID,
			"error", err)
		return "", 0, err
	}
	return response, 0, nil
}

// Compile-time guard: acpEmployeeExecutorAdapter must satisfy bot.BotExecutor.
var _ bot.BotExecutor = (*acpEmployeeExecutorAdapter)(nil)

// acpSpecSource is the registry read surface the wiring guard needs.
// *agent.AgentRegistry satisfies it via GetSpec; tests pass a stub.
type acpSpecSource interface {
	GetSpec(id string) (*agent.AgentSpec, bool)
}

// acpEmployeeLoopResolver builds the loop resolver the acp employee adapter
// uses: the same registry-backed loop source the chat lane uses
// (AgentRegistry.Get via the synthetic "_default" task bucket). Nil
// registry yields a resolver that fails loudly per turn rather than
// panicking at wiring time.
func acpEmployeeLoopResolver(registry *agent.AgentRegistry) func(agentID string) (acpTurnRunner, error) {
	return func(agentID string) (acpTurnRunner, error) {
		if registry == nil {
			return nil, fmt.Errorf("agent registry not configured")
		}
		loop, err := registry.Get(agentID)
		if err != nil {
			// Explicit nil so a failed lookup never boxes a typed-nil
			// *AgentLoop into the acpTurnRunner interface (daemon
			// AGENTS.md typed-nil rule).
			return nil, err
		}
		return loop, nil
	}
}

// effectiveACPPermissionMode returns the permission mode an acp-backed
// agent actually runs under: the agent definition's override when set,
// else the global [acp] permission_mode (precedence per internal/agent
// ACPSpec.PermissionMode doc + internal/acp/manager.go persona override).
func effectiveACPPermissionMode(spec *agent.AgentSpec, globalMode string) string {
	if spec != nil && spec.ACP != nil && spec.ACP.PermissionMode != "" {
		return spec.ACP.PermissionMode
	}
	return globalMode
}

// employeeBotExecutor decides which bot.BotExecutor an employee's GoalLoop
// gets, enforcing the acp-employees phase-3 guards (D1 target validation,
// D3 tier×permission-mode guard, D4 cost warnings) at wiring time. The
// agent registry is only in scope here — internal/employee's Validate()
// cannot see it — so executor_agent target existence/acp-backing is
// validated at this site, not at definition load.
//
// Returns (executor, refuse):
//   - ExecutorAgent empty: (defaultExecutor, false) — the non-acp path is
//     byte-identical to today (no registry reads, no logs).
//   - Reference broken (no registry / target missing / target not
//     acp-backed) OR D3 guard trips (tier-3 + permissive): ERROR logged
//     naming employee/agent/tier/mode, (nil, true) — the caller skips
//     loop registration for that employee (it stays out of the goal-loop
//     roster; the daemon keeps booting, matching the surrounding
//     skip-on-load-failure style of this wiring block).
//   - Healthy acp reference: (acp adapter, false), with WARN for tier-1/2
//     × permissive (D3) and cost-cap no-ops (D4).
func employeeBotExecutor(emp employee.Employee, defaultExecutor bot.BotExecutor,
	registry acpSpecSource, globalPermissionMode string,
	resolveLoop func(agentID string) (acpTurnRunner, error),
	logger *slog.Logger) (bot.BotExecutor, bool) {

	if emp.ExecutorAgent == "" {
		return defaultExecutor, false
	}
	if registry == nil {
		logger.Error("employee executor_agent refused: agent registry unavailable",
			"employee_id", emp.ID, "executor_agent", emp.ExecutorAgent)
		return nil, true
	}
	spec, ok := registry.GetSpec(emp.ExecutorAgent)
	if !ok || spec == nil {
		// D1: the referenced agent definition must exist.
		logger.Error("employee executor_agent references unknown agent: refusing to wire goal loop",
			"employee_id", emp.ID, "executor_agent", emp.ExecutorAgent)
		return nil, true
	}
	if spec.Executor != "acp" || spec.ACP == nil {
		// D1: the referenced definition must be acp-backed (executor: acp
		// with a non-nil acp block).
		logger.Error(fmt.Sprintf("executor_agent references agent %s which is not acp-backed: refusing to wire goal loop", emp.ExecutorAgent),
			"employee_id", emp.ID,
			"agent_executor", spec.Executor,
			"has_acp_block", spec.ACP != nil)
		return nil, true
	}

	mode := effectiveACPPermissionMode(spec, globalPermissionMode)
	// D3 guard (decision B; phase-3 doc "Security"): a tier-3 acp employee
	// running permissive means meept stops binding once the prompt is
	// sent — the plan-level escalation gate is upstream of the turn and
	// the constitution cannot reach harness internals, so only the
	// permission lane binds, and permissive disables it. That combination
	// is refused; tier-1/2 keep the human in the loop elsewhere, so a
	// warning is enough.
	if mode == "permissive" {
		if emp.Constitution.AutonomyTier == employee.Tier3Autonomous {
			logger.Error("employee acp executor refused: tier-3 autonomy with permissive permission mode",
				"employee_id", emp.ID,
				"executor_agent", emp.ExecutorAgent,
				"tier", emp.Constitution.AutonomyTier.String(),
				"permission_mode", mode)
			return nil, true
		}
		logger.Warn("employee acp executor: permissive permission mode below tier-3",
			"employee_id", emp.ID,
			"executor_agent", emp.ExecutorAgent,
			"tier", emp.Constitution.AutonomyTier.String(),
			"permission_mode", mode)
	}

	// D4 (decision A+C): the acp wire carries no token usage today
	// (session/prompt returns stopReason only, docs/workflows/
	// acp-employees-phase3.md D4), so cost-bound caps are no-ops for
	// acp employees. Warn, don't refuse — refusing would be hostile to
	// existing definitions that grew these fields under the local
	// executor.
	if emp.Constraints.DailyBudgetCents > 0 || emp.Constitution.Constraints.DailyBudgetCents > 0 {
		logger.Warn("employee acp executor: daily_budget_cents cannot bind remote turns (no cost signal on the acp wire)",
			"employee_id", emp.ID, "executor_agent", emp.ExecutorAgent)
	}
	for _, trig := range emp.Constitution.Constraints.EscalationTriggers {
		if trig.On == employee.EscalateOnCost {
			logger.Warn("employee acp executor: cost escalation trigger cannot fire (no cost signal on the acp wire)",
				"employee_id", emp.ID, "executor_agent", emp.ExecutorAgent, "trigger_match", trig.Match)
		}
	}

	return &acpEmployeeExecutorAdapter{
		employeeID:  emp.ID,
		agentID:     emp.ExecutorAgent,
		resolveLoop: resolveLoop,
		logger:      logger.With("component", "acp-employee-executor", "employee_id", emp.ID),
	}, false
}
