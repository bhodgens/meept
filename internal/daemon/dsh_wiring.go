package daemon

// dsh_wiring.go — daemon integration wiring for the dsh-convergence leaf
// seams (goal / delegate / plan-mode). This is the root-AGENTS.md wiring
// completion: every feature leaf in docs/plans/20261102-dsh-convergence ships
// its implementation ONLINE only after this file binds the live daemon
// dependencies onto the tool-provider + plan-mode seams.
//
// Invariants (internal/daemon/AGENTS.md): every Set* seam is typed-nil
// guarded — a nil concrete dependency leaves the corresponding surface
// FAIL-CLOSED (zero tools / controller sit inert) rather than arming a tool
// against a nil receiver. Nothing here invents stub behavior; an unwireable
// seam is reported as fail-closed, never faked.

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/agent/modelselect"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/pkg/models"
)

// wireDSHConvergence wires the dsh-convergence leaf seams into the live
// daemon. It runs from NewComponents AFTER the agent registry, dispatcher, ACP
// manager, session store, and employee manager all exist (the toolprov seam
// mirrors the scheduler registration at components.go:4756; goal + delegate
// arrive gated by [tool_providers]). It is nil-safe for test harnesses: a nil
// Component/registry returns immediately, and every sub-seam degrades
// independently.
func wireDSHConvergence(c *Components, cfg *config.Config, msgBus *bus.MessageBus, logger *slog.Logger) {
	if c == nil || c.ToolRegistry == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	wireGoalProvider(c, cfg, logger)
	wireDelegateProvider(c, cfg, msgBus, logger)
	wirePlanMode(c, logger)
	wireGoalRoundDriver(c, cfg, logger)
}

// goalGateParkedForEmployee reports whether any ACTIVE goal of this
// employee has a parked approval gate on the daemon's GateParker (C25).
// Nil parker / nil manager / listing failure report false (fail-open: the
// pre-C25 behavior must not regress when parking is disabled).
func goalGateParkedForEmployee(mgr *employee.Manager, parker *agent.GateParker, employeeID string) bool {
	if mgr == nil || parker == nil {
		return false
	}
	goals, err := mgr.ListGoals(context.Background(), employeeID)
	if err != nil {
		return false
	}
	for _, g := range goals {
		if g == nil || g.State != employee.GoalActive {
			continue
		}
		if parker.HasParkedGateForID(g.ID) {
			return true
		}
	}
	return false
}

// wireGoalRoundDriver constructs the autonomous continuation driver over the
// live employee manager and binds the leaf-02 authority seam (the daemon
// bridge onto builtin.SetGoalAutonomousRound), the deferred-turn scheduler
// seam (leaf 03 T2: re-enter Manager.Trigger with a source:"goal" payload),
// and the quiescence check (no parked episode + no queued steer + invoke
// mutex free). Typed-nil guarded: a nil manager leaves c.GoalRoundDriver nil
// and the round-advance RPC fails closed ("round driver not configured").
// The driver is owned here so the agents.goals.round.advance RPC handler
// reads it from Components. Start happens in Components.Start (after the
// GoalLoops are registered and the scheduler runs), gated by
// [employees] goal_auto_advance (default true, the leaf's zero-human-turns
// contract).
func wireGoalRoundDriver(c *Components, cfg *config.Config, logger *slog.Logger) {
	if c == nil || c.EmployeeManager == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	driver := employee.NewGoalRoundDriver(c.EmployeeManager, logger).
		WithAutonomousRoundSetter(builtin.SetGoalAutonomousRound).
		WithAutoAdvance(cfg.Employees.GoalAutoAdvanceEnabled())
	if c.Scheduler != nil {
		// Leaf T2: the deferred turn rides the EXISTING scheduler via the
		// same adapter shape as the employee background jobs — no new
		// queue/scheduler primitive. ScheduleOneShot maps onto an
		// immediate-fire "@every" registration that the loop's own
		// settlement pass replaces each tick.
		driver = driver.WithDeferredScheduler(
			goalRoundDeferredScheduler{sched: c.Scheduler, logger: logger})
	}
	if c.EmployeeManager != nil {
		// Quiescence (leaf 03): an employee with a parked episode
		// (provider-wait park) or queued steering is NOT settled —
		// advancing a round under it would double-start the goal.
		// Manager accessors delegate to the per-employee GoalLoop; a
		// loop without a wired parker (nil-safe seam, leaf G4) reports
		// quiet.
		//
		// C25: a parked GOAL-approval gate is also not-quiet. While a
		// human approval pends on the daemon's GateParker, every poll
		// tick would otherwise run a FULL autonomous round (LLM spend
		// + side effects) up to the 256-round cap, and tier-2 verdicts
		// could drop on a BLOCKED goal. The predicate probes the gate
		// parker per active goal id (nil parker / nil manager fail
		// open — disabled parking stays byte-identical).
		mgr := c.EmployeeManager
		gateParker := c.GateParker
		driver = driver.WithQuiescenceCheck(func(employeeID string) bool {
			if !mgr.HasParkedEpisode(employeeID) &&
				mgr.PendingSteerCount(employeeID) == 0 {
				return !goalGateParkedForEmployee(mgr, gateParker, employeeID)
			}
			return false
		})
	}
	c.GoalRoundDriver = driver
	logger.Debug("wired goal round driver", "employee_framework", true,
		"auto_advance", cfg.Employees.GoalAutoAdvanceEnabled(),
		"deferred_scheduler", c.Scheduler != nil)
}

// StartGoalRoundDriverLoop launches the round driver's autonomous
// continuation loop (dsh-convergence leaf 03 T2) on the daemon lifecycle
// context. Called from Daemon.Run AFTER Components.Start so the GoalLoops
// are registered (the quiescence check reads them) and the scheduler runs
// (the deferred turn rides it). Gated by the driver's auto-advance flag:
// [employees] goal_auto_advance=false leaves the RPC/CLI manual advance as
// the only driver. Stop happens in Daemon.shutdown via
// StopGoalRoundDriverLoop.
func (c *Components) StartGoalRoundDriverLoop(ctx context.Context) {
	if c == nil || c.GoalRoundDriver == nil {
		return
	}
	c.GoalRoundDriver.Start(ctx)
}

// StopGoalRoundDriverLoop ends the autonomous continuation loop and joins
// its goroutine. Safe when the driver or the loop never started.
func (c *Components) StopGoalRoundDriverLoop() {
	if c == nil || c.GoalRoundDriver == nil {
		return
	}
	c.GoalRoundDriver.Stop()
}

// StartDelegateResumeSweep cold-resumes in-flight delegated children left in
// the durable child-descriptor store by a previous run (bughunt C13). Called
// from Daemon.Run right next to StartGoalRoundDriverLoop — AFTER
// Components.Start so the agent registry and dispatcher exist (resumed
// children execute against them) and the session store is live. Bounded
// (maxResumeSweep), logged, best-effort: sweep failures warn and never fail
// the boot. nil manager / nil store = no-op.
func (c *Components) StartDelegateResumeSweep(ctx context.Context) {
	if c == nil || c.DelegateManager == nil {
		return
	}
	resumed, failed := c.DelegateManager.ResumeInFlightDescriptors(ctx, "")
	if len(resumed) > 0 || len(failed) > 0 {
		slog.Info("delegate resume sweep: startup cold-resume pass complete",
			"resumed", len(resumed), "failed", len(failed))
	}
}

// goalRoundDeferredScheduler adapts *scheduler.Scheduler onto the round
// driver's DeferredScheduler seam (ScheduleOneShot): each registration is a
// transient immediate-fire interval job keyed per employee. The driver's
// poll loop replaces the registration per settled round, so the job never
// accumulates; Stop drops it via Unschedule on shutdown. Best-effort:
// scheduling failures log and skip (the manual RPC/CLI advance remains).
type goalRoundDeferredScheduler struct {
	sched  *scheduler.Scheduler
	logger *slog.Logger
}

// ScheduleOneShot implements DeferredScheduler. The registration is an
// "@every 1s" interval job (the scheduler's only immediate-fire shape) whose
// callback FIRST unschedules itself, giving exact one-shot semantics: the
// first tick runs fn once and drops the registration, so a long round never
// re-queues behind its own trigger. A re-registration for the same name
// (next settled round) replaces the prior entry.
func (a goalRoundDeferredScheduler) ScheduleOneShot(name string, fn func()) {
	_ = a.sched.Unschedule(name)
	job := &simpleIntervalJob{
		id:       name,
		name:     name,
		interval: time.Second,
		fn: func() {
			// One-shot: drop the recurring registration before the work
			// runs. Safe from inside Execute — executeJob does not hold
			// the scheduler mutex across job callbacks.
			_ = a.sched.Unschedule(name)
			fn()
		},
	}
	if _, err := a.sched.Schedule(job); err != nil {
		a.logger.Warn("goal round deferred turn: schedule failed",
			"job_id", name, "error", err)
	}
}

// ---------------------------------------------------------------------------
// 1. Goal tools (goal_get / goal_create / goal_update)
// ---------------------------------------------------------------------------

// wireGoalProvider registers the "goal" tool-provider group, gated by
// [tool_providers] exactly like the scheduler group. The goal seam binds the
// LIVE *employee.Manager through the exported goalManager adapter
// (goal_adapter.go) and a current-agent owner resolver. The seam FAILS
// CLOSED: a nil manager (employee framework disabled / not yet wired) leaves
// the provider unbound and toolprov.RegisterAll rejects it — zero goal tools,
// byte-identical to "not registered".
func wireGoalProvider(c *Components, cfg *config.Config, logger *slog.Logger) {
	// Typed-nil-guarded setter: SetGoalProvider binds the adapter only when
	// it carries a live manager. A nil manager (employee framework disabled /
	// not yet wired) leaves the provider UNBOUND — clear any stale factory and
	// return so toolprov.RegisterAll fails closed to zero goal tools.
	adapter := newGoalManagerAdapter(c)
	if adapter == nil || adapter.m == nil {
		builtin.SetGoalProvider(nil, nil)
		if logger != nil {
			logger.Debug("goal provider unbound: employee manager not available; fails closed")
		}
		return
	}
	builtin.SetGoalProvider(adapter, resolveGoalOwner(c))

	names := enabledToolProviderNames(cfg, []string{"goal"}, logger)
	if len(names) == 0 {
		logger.Debug("goal provider disabled by [tool_providers]; skipping")
		return
	}
	if _, err := toolprov.RegisterAll(c.ToolRegistry, names); err != nil {
		logger.Warn("goal tool registration failed", "error", err)
		return
	}
	logger.Debug("Registered goal tools (via provider)")
}

// newGoalManagerAdapter returns the *employee.Manager goal adapter, or a
// typed-nil *goalEmployeeManagerAdapter whose methods return
// errGoalStoreUnavailable when the employee framework is absent. It is
// deliberately NOT nil — the SetGoalProvider reflectNil guard then rejects
// only a genuinely nil boxed pointer, never a valid-but-unwired store handle.
func newGoalManagerAdapter(c *Components) *goalEmployeeManagerAdapter {
	if c == nil {
		return nil
	}
	return &goalEmployeeManagerAdapter{m: c.EmployeeManager}
}

// resolveGoalOwner returns the goal owner resolver bound into the goal tools.
// It resolves in precedence order:
//  1. an employee-driven turn source (agent.ACPTurnSourceFromContext) — the
//     autonomous-goal-round / employee lane, so tools address the employee
//     whose goal they run for; and
//  2. the daemon-wide default owner ("orchestrator", matching the messaging
//     wiring's default sender) so goal tools still work from a sourced chat
//     turn when no goal round owns the context.
//
// A nil Components degrades to the default owner only.
func resolveGoalOwner(c *Components) func(ctx context.Context) string {
	defaultOwner := "orchestrator"
	return func(ctx context.Context) string {
		if kind, empID, ok := agent.ACPTurnSourceFromContext(ctx); ok &&
			kind == agent.ACPTurnSourceEmployee && empID != "" {
			return empID
		}
		_ = c // c provides no per-turn owner today; kept for future seam wiring
		return defaultOwner
	}
}

// ---------------------------------------------------------------------------
// 2. Delegate tools (subagent_* + workflow)
// ---------------------------------------------------------------------------

// wireDelegateProvider constructs the C1 DelegateManager in the daemon, wires
// its B4 route resolver (modelselect), durable store, and ACP manager, then
// builds the DelegateController and registers the "delegate" tool group gated
// by [tool_providers]. A nil agent registry or dispatcher skips the group
// (children cannot execute/steer) and leaves the provider unbound = fail
// closed.
func wireDelegateProvider(c *Components, cfg *config.Config, msgBus *bus.MessageBus, logger *slog.Logger) {
	if c.AgentRegistry == nil || c.Dispatcher == nil {
		logger.Debug("delegate provider skipped: agent registry or dispatcher not available")
		return
	}

	dm := agent.NewDelegateManager(c.AgentRegistry, delegateBusPublisher(msgBus, logger), nil)
	dm.SetRouteResolver(delegateRouteResolver(cfg, c.LLMResolver, logger))
	// C10: enforce the resolved route at EXECUTION — the child's first LLM
	// call runs the allowlist-gated route (one-shot ApplyRequestModel) rather
	// than silently falling through to the child spec's own model pin.
	if c.AgentRegistry != nil {
		dm.SetRunFunc(c.AgentRegistry.RunAgentWithModelRef)
	}
	// DSH-1 fork-seed: bind the registry's conversation-seed adapter so a
	// full-context fork child's seed is stamped onto its conversation before
	// its first turn (delegate.go's gate already restricts seeding to agents
	// whose own config sets delegate_context: full).
	dm.SetForkSeedWriter(c.AgentRegistry.ForkSeedWriter())
	if c.SessionStore != nil {
		// B6 durable child-descriptor store (SaveChildDescriptor/Get...): each
		// settled child persists its descriptor here (best-effort) so an
		// interrupted child can be cold-resumed after a restart.
		dm.SetStore(c.SessionStore)
	}
	if c.ACPManager != nil {
		// Mode=="acp" children route through the existing *acp.Manager lane.
		dm.SetACPManager(c.ACPManager)
	}
	// SetPersist is deliberately left nil: SetStore already persists settled
	// child descriptors; a second hook would be a redundant sink.

	// Build the controller over the manager, dispatcher, and ACP manager
	// (plain-data, fail-closed per primitive). The caller-session resolver
	// (C14) is bound to the loop-injected tool-context session id
	// (tools.ContextWithSessionID — the same carrier delegate_task's spawn
	// attribution uses): interrupt/send/list enforce parent-of against it.
	// UNBOUND it stays fail-closed (interrupt/send deny; list denies) —
	// cross-session control is never open, resolver bound or not.
	ctrl := builtin.NewDelegateController(dm, c.Dispatcher, c.ACPManager)
	ctrl.SetCallerSessionResolver(func(ctx context.Context) string {
		return tools.SessionIDFromContext(ctx)
	})
	if msgBus != nil {
		// DSH-8: background workflow runs deliver completion on the same
		// bus lane as child settlements (agent.delegate.result).
		ctrl.SetCompletionBus(delegateCompletionBus{msgBus})
	}
	c.DelegateManager = dm
	c.DelegateController = ctrl

	// Bind the async manager onto the legacy registered delegate_task tool so
	// it spawns asynchronously and returns a child handle (child_id). The
	// tool keeps its legacy synchronous path when no manager is bound — here
	// the binding happens only when the delegate provider is wired. Typed-nil
	// safe: a missing/renamed registration leaves delegate_task on its legacy
	// sync path.
	if dt, ok := c.ToolRegistry.Get("delegate_task").(*builtin.DelegateTaskTool); ok {
		dt.SetDelegateManager(dm)
	}

	builtin.SetDelegateProvider(ctrl)
	names := enabledToolProviderNames(cfg, []string{"delegate"}, logger)
	if len(names) == 0 {
		logger.Debug("delegate provider disabled by [tool_providers]; skipping")
		return
	}
	if _, err := toolprov.RegisterAll(c.ToolRegistry, names); err != nil {
		logger.Error("delegate tool registration failed", "error", err)
	} else {
		logger.Debug("Registered delegate tools (via provider)")
	}
}

// delegateCompletionBus adapts *bus.MessageBus onto builtin's workflowBus
// interface (the background workflow completion delivery lane, DSH-8).
type delegateCompletionBus struct {
	b *bus.MessageBus
}

func (a delegateCompletionBus) Publish(topic string, msg *models.BusMessage) int {
	return a.b.Publish(topic, msg)
}

// delegateRouteResolver returns the B4 per-child model-route resolver built
// from the [delegation] config block (dsh-convergence DSH-5). The resolver
// runs at ChildOptions finalization on EVERY delegate spawn:
//
//   - [delegation] enabled=false (default): model selection is DISABLED and
//     the resolver is modelselect's identity path (the child inherits the
//     request/parent route, fail-safe).
//   - enabled=true: the request route merges parent < configured < request
//     and asserts against the allowed_routes allowlist. An EMPTY allowlist
//     means no restriction (every resolvable route permitted); a non-empty
//     one requires an exact provider+model member match.
//   - whenever a resolved route carries provider+model, modelselect.
//     PreflightRoute verifies it resolves through the live LLM resolver
//     BEFORE the child spawns, so an unresolvable route errors visibly at
//     the tool boundary with no child created. A nil resolver (unit-test
//     wiring) skips the preflight.
func delegateRouteResolver(cfg *config.Config, resolver *llm.Resolver, logger *slog.Logger) func(agent.ChildOptions, agent.DelegateRequest) (agent.ChildOptions, error) {
	var policy *modelselect.ModelSelectionPolicy
	enabled := false
	if cfg != nil && cfg.Delegation.Enabled {
		enabled = true
		if len(cfg.Delegation.AllowedRoutes) > 0 {
			policy = &modelselect.ModelSelectionPolicy{}
			for _, r := range cfg.Delegation.AllowedRoutes {
				policy.Routes = append(policy.Routes, modelselect.AllowedModelRoute{
					Provider: r.Provider,
					Model:    r.Model,
				})
			}
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return func(parent agent.ChildOptions, req agent.DelegateRequest) (agent.ChildOptions, error) {
		resolved, err := modelselect.ResolveChildRoute(parent, nil, req, policy, enabled)
		if err != nil {
			return parent, err
		}
		if resolved == nil {
			return parent, nil
		}
		// Preflight the effective route through the live resolver BEFORE
		// the child spawns (DSH-5). Fail-closed: an unresolvable route
		// returns an error, which Spawn surfaces as a failed delegation
		// with no child created.
		if resolver != nil && resolved.Provider != "" && resolved.Model != "" {
			if err := modelselect.PreflightRoute(context.Background(), resolver, *resolved); err != nil {
				logger.Warn("delegate route preflight failed",
					"provider", resolved.Provider, "model", resolved.Model, "error", err)
				return parent, err
			}
		}
		return *resolved, nil
	}
}

// delegateBusPublisher adapts the daemon message bus onto the DelegateManager's
// `func(topic string, payload any)` settlement publisher. Best-effort: a
// marshal failure is logged, never fatal (the child lifecycle owns its own
// record; the bus event is fan-out only).
func delegateBusPublisher(b *bus.MessageBus, logger *slog.Logger) func(string, any) {
	if b == nil {
		return nil
	}
	return func(topic string, payload any) {
		msg, err := models.NewBusMessage(models.MessageType(topic), "delegate-manager", payload)
		if err != nil {
			logger.Warn("delegate manager bus event: marshal failed", "topic", topic, "error", err)
			return
		}
		b.Publish(topic, msg)
	}
}

// ---------------------------------------------------------------------------
// 3. Plan mode (per-session toggle + exit_plan_mode + plan:policy injection)
// ---------------------------------------------------------------------------

// wirePlanMode constructs the per-session PlanModeController, registers the
// exit_plan_mode tool, injects the plan-policy prompt section while active,
// and reconciles the dispatcher's /plan branch (which consults the controller
// as a toggle/steer rather than force-creating a plan).
func wirePlanMode(c *Components, logger *slog.Logger) {
	// Persistence adapter: the session store carries the durable plan_mode
	// column (DSH-9c: Store.SetPlanMode / Session.PlanMode), so the
	// toggle survives a daemon restart. When the store is unavailable
	// (or the session row does not exist yet — brand-new sessions before
	// their first persist), the adapter falls back to the process-local
	// memory map so the toggle still works within the process.
	// planModeStorePersistence satisfies builtin.PlanModePersistence over
	// the session store; the controller's GetPlanMode consults it on an
	// in-memory cache miss (builtin.PlanModeController.PlanMode).
	var persist builtin.PlanModePersistence = newPlanModeMemoryPersistence()
	if c.SessionStore != nil {
		persist = newPlanModeStorePersistence(c.SessionStore, newPlanModeMemoryPersistence())
	}
	ctrl := builtin.NewPlanModeController(persist)
	c.PlanModeController = ctrl

	// Construct exit_plan_mode EXPLICITLY and bind the daemon's real plan gate
	// (the durable *plan.PlanManager) so the Approve path lands on the same
	// gate the CLI's `plans list/approve` and the PlanModeController's /plan
	// surface use. Registering this same instance (rather than the default
	// one RegisterPlanModeTools creates) is what lets the loop injection seam
	// (SetResponseFunc + SetSessionID per turn) and the plan-gate binding
	// reach the tool the model actually calls.
	exitTool := builtin.NewExitPlanModeTool(ctrl)
	if c.PlanManager != nil {
		exitTool.SetPlanGate(c.PlanManager)
	}
	c.ToolRegistry.Register(exitTool)

	// The per-turn user-response callback + session binding are injected by
	// the agent loop (PlanModeResponseSetter seam); the daemon owns only the
	// durable gate above. The policy section + /plan reconcile follow.

	if c.InstructionContextInjector != nil {
		// plan:policy prompt injection — present only while at least one
		// session is in plan mode (controller returns "" otherwise).
		c.InstructionContextInjector.SetPlanModePolicySection(ctrl.PlanPolicySection)
	}

	if c.Dispatcher != nil {
		// /plan reconcile: /plan is now a plan-mode toggle (on/off/steer);
		// the dispatcher consults this controller via the bridge.
		c.Dispatcher.SetPlanModeController(planModeCommandBridge{ctrl: ctrl})
	}
}

// planModeCommandBridge adapts *builtin.PlanModeController onto the
// dispatcher's local agent.PlanModeController interface (agent cannot import
// builtin; builtin imports agent).
type planModeCommandBridge struct {
	ctrl *builtin.PlanModeController
}

func (b planModeCommandBridge) HandlePlanCommand(sessionID, input string) (steer, reply string, consumed bool) {
	if b.ctrl == nil {
		return "", "", false
	}
	cr, err := b.ctrl.HandlePlanCommand(sessionID, input)
	if err != nil {
		// A toggle/persistence failure is still consumed: surface the error as
		// the reply rather than falling through to legacy plan creation.
		return "", err.Error(), true
	}
	if cr == nil {
		return "", "", false
	}
	return cr.Steer, cr.Message, true
}

// planModeMemoryPersistence is a process-local builtin.PlanModePersistence.
// It is the FALLBACK lane of the plan-mode persistence adapter: it keeps the
// per-session toggle working when the session store is unavailable or the
// session row does not exist yet (brand-new sessions). It is NOT
// cross-restart durable; the store-backed adapter (planModeStorePersistence)
// owns durability.
type planModeMemoryPersistence struct {
	mu sync.Mutex
	m  map[string]bool
}

func newPlanModeMemoryPersistence() *planModeMemoryPersistence {
	return &planModeMemoryPersistence{m: make(map[string]bool)}
}

func (p *planModeMemoryPersistence) GetPlanMode(sessionID string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[sessionID], nil
}

func (p *planModeMemoryPersistence) SetPlanMode(sessionID string, on bool) error {
	p.mu.Lock()
	p.m[sessionID] = on
	p.mu.Unlock()
	return nil
}

var _ builtin.PlanModePersistence = (*planModeMemoryPersistence)(nil)

// planModeStorePersistence is the DURABLE builtin.PlanModePersistence
// (DSH-9c): writes land in the session store's plan_mode column via
// session.Store.SetPlanMode so the toggle survives a daemon restart, and
// reads consult the store (Session.PlanMode via Get) so a fresh process
// restores the flag instead of silently resetting to OFF.
//
// Fallback contract: when the store has no row for the session (a brand-new
// session before its first persist — SetPlanMode errors with "session not
// found") both directions degrade to the memory lane, so the toggle still
// works within the process without resurrecting stale cross-restart state
// for a session that was never persisted. Store unavailability is logged
// best-effort, never fatal to the toggle caller.
type planModeStorePersistence struct {
	store session.Store
	mem   *planModeMemoryPersistence
}

func newPlanModeStorePersistence(store session.Store, mem *planModeMemoryPersistence) *planModeStorePersistence {
	return &planModeStorePersistence{store: store, mem: mem}
}

func (p *planModeStorePersistence) GetPlanMode(sessionID string) (bool, error) {
	if sess := p.store.Get(sessionID); sess != nil {
		return sess.PlanMode, nil
	}
	// No session row (or store unavailable): fall back to the process-local
	// lane so the in-process toggle survives.
	return p.mem.GetPlanMode(sessionID)
}

func (p *planModeStorePersistence) SetPlanMode(sessionID string, on bool) error {
	if err := p.store.SetPlanMode(sessionID, on); err != nil {
		// New session without a store row (or a store write failure): keep
		// the toggle functional in-process via the memory lane.
		return p.mem.SetPlanMode(sessionID, on)
	}
	// Keep the memory lane in sync so a later Get that falls back to it
	// (row deleted mid-process) agrees with the durable value.
	return p.mem.SetPlanMode(sessionID, on)
}

var _ builtin.PlanModePersistence = (*planModeStorePersistence)(nil)
