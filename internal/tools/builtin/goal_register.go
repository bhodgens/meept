// Package builtin — goal_register.go populates the toolprov provider seam
// for the model-facing goal tools (dsh-convergence leaf
// docs/plans/20261102-dsh-convergence/02-goals-plans/02-model-goal-tools.md,
// contract C4b): goal_get / goal_create / goal_update.
//
// The tools' construction inputs (the live goal store adapter and the
// current-agent owner resolver) are runtime values only the daemon can
// supply, so this registers a provider FACTORY whose inputs are captured at
// Discover time via SetGoalProvider — the same acp_register_tools.go /
// hermes_register.go / schedule_register.go pattern. An unbound factory
// fails closed: toolprov.RegisterAll rejects the nil Provider and the goal
// tools stay absent from the registry until the daemon wiring leaf binds and
// enables the provider, byte-identical to the "not registered" outcome.
//
// Import cycle: internal/tools/builtin cannot import internal/employee
// (employee → bot → … → tools/builtin), so the manager adapter is defined
// locally (goal_tools.go) and the daemon wiring leaf supplies the mapping
// onto *employee.Manager — the same inversion-of-control seam
// agent_message.go uses for its store.
package builtin

import (
	"reflect"

	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

func init() {
	if err := toolprov.Register("goal", goalProviderFactory); err != nil {
		panic("builtin: goal provider registration: " + err.Error())
	}
}

// goalProviderFactoryFunc builds the goal provider from the handle a
// Discover-time caller captures. Declared as a variable so the daemon can
// bind the live manager adapter before toolprov.RegisterAll constructs the
// provider.
var goalProviderFactoryFunc func() toolprov.Provider

// goalProviderFactory is the thunk handed to toolprov.Register. It defers
// to goalProviderFactoryFunc; an unbound factory (no SetGoalProvider yet —
// the default) returns nil and toolprov.RegisterAll fails closed on the nil
// Provider.
func goalProviderFactory() toolprov.Provider {
	if goalProviderFactoryFunc == nil {
		return nil
	}
	return goalProviderFactoryFunc()
}

// ResetGoalProviderForTest restores the unbound (default) factory state in
// tests that run enabled wiring before the fail-closed default. Production
// never calls this.
func ResetGoalProviderForTest() {
	goalProviderFactoryFunc = nil
}

// SetGoalProvider binds the goal provider's construction inputs: the goal
// store adapter (onto *employee.Manager) and the owner resolver that maps a
// calling turn to its agent id. Typed-nil guard is applied to the seam (a
// nil adapter — either a true nil or a nil pointer boxed into the interface
// — leaves the factory unbound so the provider fails closed and no goal tool
// arms against a nil store).
func SetGoalProvider(mgr goalManager, owner goalOwner) {
	if reflectNil(mgr) {
		goalProviderFactoryFunc = nil
		return
	}
	goalProviderFactoryFunc = func() toolprov.Provider {
		return &goalProvider{mgr: mgr, owner: owner}
	}
}

// goalProvider is the toolprov.Provider for the goal tool group.
type goalProvider struct {
	mgr   goalManager
	owner goalOwner
}

// Name implements toolprov.Provider.
func (p *goalProvider) Name() string { return "goal" }

// Discover implements toolprov.Provider. It constructs the three goal tools
// with the same bound manager adapter and owner resolver.
func (p *goalProvider) Discover() []tools.Tool {
	return []tools.Tool{
		NewGoalGetTool(p.mgr, p.owner),
		NewGoalCreateTool(p.mgr, p.owner),
		NewGoalUpdateTool(p.mgr, p.owner),
	}
}

// PermissionClass implements toolprov.Provider. The goal tools create and
// mutate goals (mutating state); they never spawn processes, so they are not
// privileged.
func (p *goalProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskMutating
}

// Broker implements toolprov.Provider. The goal tools hold no secret
// material, so the broker binding is nil.
func (p *goalProvider) Broker() *secrets.Broker { return nil }

// reflectNil is the typed-nil guard for the goalManager seam: it reports
// true for a true nil and for a nil concrete pointer boxed into the
// interface (the classic boxed-nil wiring bug). Established use: reject the
// binding so the provider fails closed instead of arming tools against a nil
// store that would panic on first deref.
func reflectNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
