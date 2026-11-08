package daemon

// dsh_wiring_typednil_test.go pins the dsh-convergence wiring seams' FAIL-CLOSED
// contracts through the REAL wiring function (internal/daemon/AGENTS.md
// typed-nil rule): a minimal Components with every runtime dependency nil must
// complete without panic, leave the goal + delegate providers unbound (zero
// tools, byte-identical to "not registered"), and still arm the plan-mode
// controller + exit_plan_mode tool. The positive path is exercised by the
// existing NewComponents integration tests every daemon build runs.

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// TestWireDSHConvergenceNilDepsFailClosed runs wireDSHConvergence over a
// Components whose runtime deps (agent registry, dispatcher, ACP manager,
// session store, context injector) are ALL nil. It must bind nothing that
// arms against a nil receiver: plan mode still comes up (fully self-contained),
// but goal + delegate stay fail-closed and the manager fields stay nil.
func TestWireDSHConvergenceNilDepsFailClosed(t *testing.T) {
	// Isolate this test from global builtin provider state left by daemon
	// integration tests (and clean up after ourselves).
	builtin.ResetGoalProviderForTest()
	builtin.ResetDelegateProviderForTest()
	t.Cleanup(func() {
		builtin.ResetGoalProviderForTest()
		builtin.ResetDelegateProviderForTest()
	})

	logger := slog.New(slog.DiscardHandler)
	reg := tools.NewRegistry(logger)
	c := &Components{
		ToolRegistry: reg, // the ONLY required dependency; all else nil
	}

	wireDSHConvergence(c, nil, nil, logger)

	// Plan mode is fully self-contained and must come up even with no other
	// dependency: the controller exists and exit_plan_mode is registered.
	if c.PlanModeController == nil {
		t.Fatal("WireDSHConvergence with nil deps: PlanModeController not constructed")
	}
	if reg.Get("exit_plan_mode") == nil {
		t.Fatal("exit_plan_mode missing from the registry after plan-mode wiring with nil deps")
	}

	// Delegate: with no agent registry/dispatcher the manager + controller stay
	// nil and the provider stays unbound (RegisterAll fails closed).
	if c.DelegateManager != nil {
		t.Error("DelegateManager should be nil when AgentRegistry/Dispatcher are nil")
	}
	if c.DelegateController != nil {
		t.Error("DelegateController should be nil when AgentRegistry/Dispatcher are nil")
	}
	if _, err := toolprov.RegisterAll(tools.NewRegistry(logger), []string{"delegate"}); err != nil {
		t.Logf("delegate provider fail-closed (expected with nil deps): %v", err)
	} else {
		t.Error("delegate provider registered while agent registry/dispatcher are nil; must fail closed")
	}

	// Goal: the adapter cannot be built (unexported types), so the provider is
	// unbound and registration fails closed.
	if _, err := toolprov.RegisterAll(tools.NewRegistry(logger), []string{"goal"}); err != nil {
		t.Logf("goal provider fail-closed (expected: goalManager adapter types unexported): %v", err)
	} else {
		t.Error("goal provider registered while unbound; must fail closed")
	}
}

// TestDshWiringDelegateRouteIdentity pins that delegateRouteResolver is the
// modelselect identity path when model selection is disabled: the request
// route passes through unchanged and never panics on a nil resolved route.
// Nil cfg/resolver/logger = disabled selection, the production default
// ([delegation] absent), so the identity posture is exercised exactly.
func TestDshWiringDelegateRouteIdentity(t *testing.T) {
	resolve := delegateRouteResolver(nil, nil, nil)
	in := agent.ChildOptions{Provider: "openai", Model: "gpt", ReasoningEffort: "high"}
	out, err := resolve(in, agent.DelegateRequest{Provider: "openai", Model: "gpt", ReasoningEffort: "high"})
	if err != nil {
		t.Fatalf("identity resolve errored: %v", err)
	}
	if out.Provider != in.Provider || out.Model != in.Model || out.ReasoningEffort != in.ReasoningEffort {
		t.Errorf("identity resolver changed the route: got %+v, want %+v", out, in)
	}
}

// TestDshWiringPlanModePersistence pins the memory-backed PlanModePersistence:
// it stores and returns per-session values with a nil error (process-local
// durability, the only seam available until a session-store column lands).
func TestDshWiringPlanModePersistence(t *testing.T) {
	p := newPlanModeMemoryPersistence()
	if on, err := p.GetPlanMode("sess-a"); err != nil || on {
		t.Fatalf("fresh GetPlanMode = %v, %v; want false, nil", on, err)
	}
	if err := p.SetPlanMode("sess-a", true); err != nil {
		t.Fatalf("SetPlanMode: %v", err)
	}
	if on, err := p.GetPlanMode("sess-a"); err != nil || !on {
		t.Fatalf("GetPlanMode after set = %v, %v; want true, nil", on, err)
	}
	// Different session is independent.
	if on, _ := p.GetPlanMode("sess-b"); on {
		t.Fatal("plan mode leaked across sessions")
	}
}

// TestDshWiringPlanModeBridge pins that the dispatcher bridge adapts a /plan
// toggle onto the builtin controller: a bare /plan activates plan mode and is
// consumed; /plan off clears it.
func TestDshWiringPlanModeBridge(t *testing.T) {
	ctrl := builtin.NewPlanModeController(newPlanModeMemoryPersistence())
	builtin.RegisterPlanModeTools(tools.NewRegistry(slog.New(slog.DiscardHandler)), ctrl)
	b := planModeCommandBridge{ctrl: ctrl}

	steer, reply, consumed := b.HandlePlanCommand("sess-a", "/plan")
	if !consumed || steer != "" || reply != "plan mode on" {
		t.Fatalf("bare /plan: consumed=%v steer=%q reply=%q; want true \"\" \"plan mode on\"", consumed, steer, reply)
	}
	if !ctrl.PlanMode("sess-a") {
		t.Fatal("plan mode should be ON after /plan")
	}

	_, reply, consumed = b.HandlePlanCommand("sess-a", "/plan off")
	if !consumed || reply != "plan mode off" {
		t.Fatalf("/plan off: consumed=%v reply=%q; want true \"plan mode off\"", consumed, reply)
	}
	if ctrl.PlanMode("sess-a") {
		t.Fatal("plan mode should be OFF after /plan off")
	}

	// A real task steers.
	steer, _, consumed = b.HandlePlanCommand("sess-b", "/plan design the login flow")
	if !consumed || steer != "design the login flow" {
		t.Fatalf("steer: consumed=%v steer=%q; want true \"design the login flow\"", consumed, steer)
	}
	// Nil controller fails open (falls through to legacy plan creation).
	if _, _, consumed = (planModeCommandBridge{}).HandlePlanCommand("s", "/plan x"); consumed {
		t.Fatal("nil bridge consumed a command; must fall through")
	}
}
