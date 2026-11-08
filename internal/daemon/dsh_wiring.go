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
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/agent/modelselect"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/toolprov"
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
}

// ---------------------------------------------------------------------------
// 1. Goal tools (goal_get / goal_create / goal_update)
// ---------------------------------------------------------------------------

// wireGoalProvider registers the "goal" tool-provider group, gated by
// [tool_providers] exactly like the scheduler group. The goal seam FAILS
// CLOSED by design: builtin's goalManager interface
// (internal/tools/builtin/goal_tools.go) declares methods whose signatures
// reference UNEXPORTED types (goalCreateSpec, goalRecord), so an adapter onto
// the live *employee.Manager CANNOT be constructed from the daemon package —
// implementing the interface is a compile-time error. SetGoalProvider is
// therefore never bound with a live adapter here; the factory stays nil and
// toolprov.RegisterAll rejects the nil provider, landing zero goal tools
// (byte-identical to "not registered", the ACP-disabled-default contract).
// The group is registered in its OWN RegisterAll so this expected fail-closed
// error never aborts the sibling delegate group. To activate: export the
// leaf's goalCreateSpec/goalRecord (or add an exported adapter factory in
// builtin), build the *employee.Manager adapter here, and the same gated
// RegisterAll immediately arms the tools.
func wireGoalProvider(c *Components, cfg *config.Config, logger *slog.Logger) {
	// Typed-nil-guarded setter: SetGoalProvider guards internally
	// (reflectNil => factory unbound => fail-closed). A nil adapter is the
	// ONLY bindable handle today because the adapter type itself cannot be
	// written externally — never wire a hand-rolled fake manager.
	builtin.SetGoalProvider(nil, nil)

	names := enabledToolProviderNames(cfg, []string{"goal"}, logger)
	if len(names) == 0 {
		logger.Debug("goal provider disabled by [tool_providers]; skipping")
		return
	}
	if _, err := toolprov.RegisterAll(c.ToolRegistry, names); err != nil {
		// Expected while the goalManager adapter types are unexported: the
		// provider factory returns nil and registration fails closed. Debug
		// (not Warn/Error) keeps the known-unwireable seam quiet on every boot.
		logger.Debug("goal provider not registered (goalManager adapter types unexported; fails closed)",
			"error", err)
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
	dm.SetRouteResolver(delegateRouteResolver())
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
	// (plain-data, fail-closed per primitive). The caller-session resolver seam
	// is left UNBOUND: the tool-execution context carries no session id today,
	// and resolving it would require inventing a context key. The controller
	// fails closed safely without it (subagent_interrupt denies; list_children
	// requires an explicit parent_session_id).
	ctrl := builtin.NewDelegateController(dm, c.Dispatcher, c.ACPManager)
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

// delegateRouteResolver returns the B4 per-child model-route resolver. No
// [delegation] policy is configured, so model selection is DISABLED and the
// resolver is modelselect's identity path (the child inherits the
// request/parent route, fail-safe). It is wired anyway so the live modelselect
// code path runs on every spawn; a future delegation policy flips enabled +
// policy here.
func delegateRouteResolver() func(agent.ChildOptions, agent.DelegateRequest) (agent.ChildOptions, error) {
	return func(parent agent.ChildOptions, req agent.DelegateRequest) (agent.ChildOptions, error) {
		resolved, err := modelselect.ResolveChildRoute(parent, nil, req, nil, false)
		if err != nil {
			return parent, err
		}
		if resolved == nil {
			return parent, nil
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
	// Persistence adapter: the session store exposes no plan_mode accessor
	// (leaf 04 defined the PlanModePersistence seam but shipped no durable
	// column; adding one is a session-store change outside daemon ownership).
	// A memory-backed adapter satisfies the seam and keeps the toggle durable
	// FOR THE PROCESS; a dedicated session.Store column would swap it in place.
	ctrl := builtin.NewPlanModeController(newPlanModeMemoryPersistence())
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

// planModeMemoryPersistence is a process-local builtin.PlanModePersistence. It
// makes the per-session plan_mode toggle durable for the controller's lifetime
// (satisfying the seam a durable store accessor would fill) but is NOT
// cross-restart durable — the session store exposes no plan_mode accessor. A
// later session.Store column can replace this in place.
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
