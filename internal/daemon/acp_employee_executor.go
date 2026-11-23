package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/session"
)

// acpTurnRunner is the minimal loop seam the acp-backed employee executor
// needs: one user message in, final text out, plus the C2 one-shot turn
// prefix (constitution prompt) armed before that turn. *agent.AgentLoop
// satisfies it (RunOnce delegates to RunOnceWithParts, which carries the
// shared reply-guard / output-filter / security-scan lane — the reason
// D2-B routes through the loop instead of calling ACPExecutor.Run
// directly; see docs/workflows/acp-employees-phase3.md).
type acpTurnRunner interface {
	RunOnce(ctx context.Context, userMessage, conversationID string) (string, error)
	// SetTurnSystemPrefix arms the one-shot prefix (routing-funnel C2)
	// consumed by the next RunOnce on this receiver. Empty is ignored.
	SetTurnSystemPrefix(prefix string)
}

// acpTurnAuditRecorder is the narrow employee-audit surface the adapter
// rides for phase-3 D6 attribution: one AuditFinding per employee-driven
// acp turn naming the serving registry agent, the acp harness, and the
// persona. *employee.AuditStore satisfies it via Create — the same
// precedent the goal loop's escalation writes follow (goal_loop.go
// decideTier3 auditStore.Create, Severity/Checkpoint fields).
// A nil recorder means attribution dark: the turn path is unaffected.
type acpTurnAuditRecorder interface {
	Create(ctx context.Context, f employee.AuditFinding) error
}

// compile-time proof that the employee audit store satisfies the seam.
var _ acpTurnAuditRecorder = (*employee.AuditStore)(nil)

// acpEmployeeExecutorAdapter implements bot.BotExecutor for an employee
// whose BotDefinition.ExecutorAgent names an acp-backed registry agent
// definition (acp-employees phase 3, D1-A/D2-B). Each EXECUTE turn routes
// the userMessage through the REFERENCED agent's loop RunOnce — never
// RunOnceWithParts — so harness output earns the same reply guard, output
// filters, and security scan as a local employee turn.
//
// D6 observability attribution (phase 3): ExecuteBot stamps the caller
// ctx with agent.WithACPTurnSource(employee, employeeID) before RunOnce.
// The context value survives RunOnce -> RunOnceWithParts -> loopCtx
// (context.WithCancel derives values) -> runTurnProducer ->
// runACPTurn -> ACPExecutor.Run, so the executor's single
// acp.persona.turns emission site tags employee-driven turns
// source=employee (+ employee_id) while chat-slave turns stay
// source=chat. The adapter emits NO metric datapoint itself — one
// emission point, no double counting — and records one post-turn
// AuditFinding naming agent/harness/persona when an audit recorder is
// wired.
//
// Token counts are not available on the acp wire (session/prompt returns
// stopReason only — docs/workflows/acp-employees-phase3.md D4), so
// tokensUsed is always 0. The local adapter reports LastTurnTokens; this
// lane has no usage to report.
type acpEmployeeExecutorAdapter struct {
	employeeID string
	agentID    string
	// harnessAgent + persona come from the referenced spec's acp block
	// (spec.ACP.Agent / spec.ACP.Persona) at wiring time; they are only
	// read for the audit-attribution finding, never for routing.
	harnessAgent string
	persona      string
	// resolveLoop returns the referenced agent's loop. Built from the
	// agent registry at wiring time; a function (not the registry
	// concrete type) keeps the adapter unit-testable without a registry.
	resolveLoop func(agentID string) (acpTurnRunner, error)
	// audit persists the per-turn attribution finding; nil = dark.
	audit  acpTurnAuditRecorder
	logger *slog.Logger
	// sessions is the existing session store. Nil is a no-op.
	sessions session.Store
	// lastCostUSD is the just-finished turn's provider dollar cost,
	// captured in ExecuteBot for ExecutionCostCents (goal-loop budget
	// lane). Rounds on one executor are sequential, so a plain field
	// needs no extra synchronization.
	lastCostUSD float64
	// noPermissive is set for tier-3 employees: their turn ctx is stamped
	// with acp.WithNoPermissive so the manager refuses a permissive session
	// regardless of which source (call option / catalog / global) selected
	// it. The D3 wiring guard cannot see the catalog entry, so this is the
	// runtime fail-closed backstop (bughunt H5).
	noPermissive bool
}

// ExecuteBot runs a single employee turn through the referenced agent's
// loop. C2: the constitution/employee systemPrompt is armed as the loop's
// one-shot turn prefix (SetTurnSystemPrefix) before RunOnce — mirrored
// from employee_service_adapter.go. Empty prompts are ignored by the
// seam; the loop's permanent prompt config is never written. The loop
// constructs its own system prompts internally; the prefix prepends to
// that assembly for this turn only.
// Returns (output, 0, err).
func (a *acpEmployeeExecutorAdapter) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	// Per-turn cost reset: ExecutionCostCents reports the just-finished
	// attempt. The goal loop reads it once per attempt (executeBotWithRetry
	// accumulates across attempts), so a stale value from a prior attempt
	// must never be re-counted (bughunt M2/L9).
	a.lastCostUSD = 0
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
	// Standing employee chat session (routing-funnel C4). Not a throwaway
	// bot-exec id. A missing store must not panic.
	conversationID := employee.EmployeeChatSessionID(a.employeeID)
	if err := ensureEmployeeChatSession(a.sessions, a.employeeID); err != nil {
		warnEmployeeSession(a.logger, "standing employee session ensure failed",
			"employee_id", a.employeeID,
			"session_id", conversationID,
			"error", err)
	}
	// D6: stamp the turn source on the caller ctx BEFORE RunOnce. The
	// value survives every ctx derive inside the loop down to
	// ACPExecutor.Run's single emission point, which tags the
	// acp.persona.turns datapoint source=employee + employee_id —
	// distinguishing it from a chat-slave turn on the same persona.
	// C2: arm the constitution prefix for THIS turn only, mirroring
	// employee_service_adapter.go:395 (one-shot; empty ignored by the
	// seam).
	loop.SetTurnSystemPrefix(systemPrompt)
	turnCtx := agent.WithACPTurnSource(ctx, agent.ACPTurnSourceEmployee, a.employeeID)
	// H5 fail-closed: a tier-3 employee's turn ctx carries the marker that
	// makes the acp.Manager refuse a permissive session from ANY of the
	// three mode sources. The value survives every ctx derive down to
	// GetOrCreatePersona (context.WithCancel derives values).
	if a.noPermissive {
		turnCtx = acp.WithNoPermissive(turnCtx)
	}
	// M2: plant a per-turn cost sink on the ctx. The ACP wire bypasses
	// reasoningCycle (the sole production caller of recordTurnCostUSD), so
	// without this seam ExecutionCostCents() was structurally always 0 and
	// the daily-budget lane never bound. When the harness reports a cost
	// on session/prompt, acp.Session forwards it here and we accumulate it
	// into lastCostUSD (additive: more than one response per turn is safe).
	turnCtx = acp.WithTurnCostSink(turnCtx, func(costUSD float64) {
		a.lastCostUSD += costUSD
	})
	response, err := loop.RunOnce(turnCtx, userMessage, conversationID)
	// Goal-loop budget lane: the harness-reported cost was accumulated by
	// the sink above. Some loops (and test doubles) instead expose the
	// turn's cost via LastTurnCostUSD; fold that in only when the sink saw
	// nothing, so a cost reported on the wire is never clobbered by a
	// second, possibly stale, source.
	if costReader, ok := loop.(interface{ LastTurnCostUSD() float64 }); ok {
		if sinkCost := a.lastCostUSD; sinkCost == 0 {
			if loopCost := costReader.LastTurnCostUSD(); loopCost > 0 {
				a.lastCostUSD = loopCost
			}
		}
	}
	persistEmployeeExchange(a.sessions, conversationID, a.employeeID, userMessage, response, a.logger)
	if err != nil {
		a.recordTurnAudit(ctx, conversationID, "error")
		a.logger.Warn("acp employee executor turn failed",
			"employee_id", a.employeeID,
			"executor_agent", a.agentID,
			"conversation_id", conversationID,
			"error", err)
		return "", 0, err
	}
	a.recordTurnAudit(ctx, conversationID, "success")
	return response, 0, nil
}

// ExecutionCostCents reports the serving loop's provider dollar cost
// (captured in ExecuteBot) rounded to whole cents, mirroring the local
// adapters, so the goal-loop budget lane covers acp-backed employees too.
// Unseen or unconfigured cost reports 0.
func (a *acpEmployeeExecutorAdapter) ExecutionCostCents() int {
	return dollarsToExecutionCents(a.lastCostUSD)
}

// recordTurnAudit writes the D6 attribution finding: one employee audit
// row per employee-driven acp turn naming the serving registry agent, the
// acp harness, and the persona, so `meept agents audit` attributes remote
// turns. Follows the goal-loop escalation precedent (SeverityInfo +
// CheckpointPostTurn + ViolatedRule as the event key; Evidence carries
// the linkage). Nil audit recorder = attribution dark. A write failure is
// logged and never fails the turn — the turn already completed; the
// finding is observability, not correctness (same posture as the
// escalation audit write in goal_loop.go).
func (a *acpEmployeeExecutorAdapter) recordTurnAudit(ctx context.Context, conversationID, outcome string) {
	if a.audit == nil {
		return
	}
	finding := employee.AuditFinding{
		EmployeeID:   a.employeeID,
		TurnID:       conversationID,
		Severity:     employee.SeverityInfo,
		Checkpoint:   employee.CheckpointPostTurn,
		ViolatedRule: "acp_turn_attribution",
		Evidence: fmt.Sprintf("acp turn served by executor_agent %q (harness %q, persona %q), outcome=%s",
			a.agentID, a.harnessAgent, a.persona, outcome),
	}
	if err := a.audit.Create(ctx, finding); err != nil {
		a.logger.Warn("acp employee executor audit attribution write failed (non-fatal)",
			"employee_id", a.employeeID,
			"executor_agent", a.agentID,
			"error", err)
	}
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
// agent runs under from the two sources visible here: the agent
// definition's override when set, else the global [acp] permission_mode.
//
// NOTE: this is a conservative LOWER BOUND for the wiring-time guard only.
// The acp.Manager ALSO applies an operator-owned catalog entry
// PermissionMode (acp_agents.json5) with precedence call option > catalog
// > global — a source this function cannot see (bughunt H5). The
// D3 guard therefore also stamps WithNoPermissive on tier-3 turns so the
// manager enforces fail-closed at session creation: a tier-3 employee can
// never run permissive even when the catalog entry is permissive while
// this function reports "human". A resolver seam may be supplied to make
// the wiring-time check see the catalog mode too (see permissionModeResolver).
func effectiveACPPermissionMode(spec *agent.AgentSpec, globalMode string) string {
	if spec != nil && spec.ACP != nil && spec.ACP.PermissionMode != "" {
		return spec.ACP.PermissionMode
	}
	return globalMode
}

// permissionModeResolver is the optional seam the wiring guard consults
// for the FULL effective mode (call option > catalog entry > global). When
// supplied, the tier-3 D3 guard uses its answer so the refusal decision is
// made on the same value the session will actually run with. It is
// satisfied by (*acp.Manager).EffectivePermissionMode from the daemon
// wiring (via a small closure, keeping this package free of an acp import).
type permissionModeResolver func(agentID, callMode string) string

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
//
// modeResolver is an OPTIONAL seam (variadic so existing callers are
// byte-identical): when supplied it returns the FULL effective permission
// mode (call option > catalog entry > global) that the session will
// actually run with, letting the tier-3 D3 guard see the operator-owned
// catalog entry too. When absent, the guard falls back to spec+global and
// the runtime no-permissive marker is the fail-closed backstop at session
// creation (see ExecuteBot / acp.WithNoPermissive).
func employeeBotExecutor(emp employee.Employee, defaultExecutor bot.BotExecutor,
	registry acpSpecSource, globalPermissionMode string,
	resolveLoop func(agentID string) (acpTurnRunner, error),
	audit acpTurnAuditRecorder,
	logger *slog.Logger,
	modeResolver ...permissionModeResolver) (bot.BotExecutor, bool) {

	if emp.ExecutorAgent == "" {
		return employeeBoundLocalExecutor(emp, defaultExecutor), false
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
	// H5: when a resolver seam is supplied, use the FULL effective mode
	// (call option = spec.ACP.PermissionMode > catalog entry > global) that
	// the session will actually run with. This makes the wiring-time D3
	// guard see the operator-owned catalog entry it previously missed.
	// CallMode carries the agent-definition override so the resolver's
	// precedence mirrors GetOrCreatePersona exactly.
	if len(modeResolver) > 0 && modeResolver[0] != nil {
		mode = modeResolver[0](spec.ACP.Agent, spec.ACP.PermissionMode)
		if mode == "" {
			mode = globalPermissionMode
		}
	}
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

	// H5 fail-closed backstop: a tier-3 employee stamps WithNoPermissive
	// on its turn ctx so the acp.Manager refuses a permissive session from
	// any source, even when this wiring guard could only see spec+global.
	noPermissive := emp.Constitution.AutonomyTier == employee.Tier3Autonomous

	// D4 (decision A+C): the ACP wire does not carry provider usage on
	// every harness today (session/prompt returns stopReason only,
	// docs/workflows/acp-employees-phase3.md D4). Cost-bound caps bind only
	// when the harness reports a cost on the prompt response; a harness
	// without that extension leaves ExecutionCostCents() at 0, so these
	// caps remain inert for it. Warn, don't refuse — refusing would be
	// hostile to existing definitions that grew these fields under the
	// local executor.
	if emp.Constraints.DailyBudgetCents > 0 || emp.Constitution.Constraints.DailyBudgetCents > 0 {
		logger.Warn("employee acp executor: daily_budget_cents binds only when the harness reports cost (no cost signal on some acp wires)",
			"employee_id", emp.ID, "executor_agent", emp.ExecutorAgent)
	}
	for _, trig := range emp.Constitution.Constraints.EscalationTriggers {
		if trig.On == employee.EscalateOnCost {
			logger.Warn("employee acp executor: cost escalation trigger fires only when the harness reports cost (no cost signal on some acp wires)",
				"employee_id", emp.ID, "executor_agent", emp.ExecutorAgent, "trigger_match", trig.Match)
		}
	}

	return &acpEmployeeExecutorAdapter{
		employeeID:   emp.ID,
		agentID:      emp.ExecutorAgent,
		harnessAgent: spec.ACP.Agent,
		persona:      spec.ACP.Persona,
		resolveLoop:  resolveLoop,
		audit:        audit,
		noPermissive: noPermissive,
		logger:       logger.With("component", "acp-employee-executor", "employee_id", emp.ID),
		sessions:     sessionsFromExecutor(defaultExecutor),
	}, false
}

// sessionsFromExecutor reads the existing session store off the shared
// local adapter so the acp lane persists into the same store. A foreign
// or nil executor has no store; a missing store must not panic.
func sessionsFromExecutor(exec bot.BotExecutor) session.Store {
	adapter, ok := exec.(*agentLoopBotExecutorAdapter)
	if !ok || adapter == nil {
		return nil
	}
	return adapter.sessions
}

// employeeBoundLocalExecutor applies the bug-1 fix to the NON-voice
// local lane: an employee with a constitution whose default executor is
// the shared agent-loop adapter gets the per-turn-clone wrapper, so the
// constitution's tool policy binds the model-visible list instead of
// the full template list. Everything else passes through unchanged:
// no constitution (the goal-loop roster skips those anyway), a foreign
// executor type (test doubles / future executors), or a nil executor.
// The acp branch above never reaches here: its loop is a registry
// agent's loop already spec-scoped by AgentRegistry.filterTools, and
// the harness branch sends no meept tool definitions to a model.
func employeeBoundLocalExecutor(emp employee.Employee, defaultExecutor bot.BotExecutor) bot.BotExecutor {
	if defaultExecutor == nil || !emp.HasConstitution() {
		return defaultExecutor
	}
	inner, ok := defaultExecutor.(*agentLoopBotExecutorAdapter)
	if !ok {
		return defaultExecutor
	}
	return &employeeBoundAgentLoopExecutor{
		inner:        inner,
		employeeID:   emp.ID,
		constitution: emp.Constitution,
		sessions:     inner.sessions,
	}
}
