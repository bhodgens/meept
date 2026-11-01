package builtin

import (
	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// acp_register_tools.go populates the toolprov provider seam for the
// ACP agent tool group (contract C1,
// docs/plans/20261031-tool-provider-seam/03-migrate.md T3).
//
// DEFAULT-OFF SEMANTICS PRESERVED: the provider factory registers at
// init like every other compiled-in provider, but the daemon binds it
// ONLY when RegisterACPTools' existing guard holds (non-nil manager,
// mgr.Enabled() true - i.e. [acp] enabled=true). With [acp] at its
// default enabled=false the manager is nil / disabled, the factory
// stays unbound and returns nil, and toolprov.RegisterAll fails closed
// on the nil Provider: zero tools registered, no subprocess path armed,
// byte-identical to the pre-seam guard. No code path constructs a
// subprocess-capable tool unless the manager was already live under
// the old direct-Register rule.
//
// The provider contributes the SAME tool (name "acp_agent") the former
// direct Register call built, with the same constructor argument.
func init() {
	if err := toolprov.Register("acp", acpProviderFactory); err != nil {
		panic("builtin: acp provider registration: " + err.Error())
	}
}

// acpProviderFactoryFunc builds the acp provider from the handle a
// Discover-time caller captures. Declared as a variable so the daemon
// can bind the live manager before toolprov.RegisterAll constructs the
// provider.
var acpProviderFactoryFunc func() toolprov.Provider

// acpProviderFactory is the thunk handed to toolprov.Register. It
// defers to acpProviderFactoryFunc; an unbound factory (acp disabled -
// the default) returns nil and toolprov.RegisterAll fails closed on
// the nil Provider.
func acpProviderFactory() toolprov.Provider {
	if acpProviderFactoryFunc == nil {
		return nil
	}
	return acpProviderFactoryFunc()
}

// ResetACPProviderForTest restores the unbound (disabled-default)
// factory state in tests that run enabled-manager wiring before the
// default-off pin. Production code never calls this - the factory
// binding is Discover-time state a live daemon sets per [acp] config.
func ResetACPProviderForTest() {
	acpProviderFactoryFunc = nil
}

// SetACPProvider binds the acp provider's construction input. Callers
// must apply the RegisterACPTools guard (non-nil, enabled manager)
// BEFORE binding - this setter intentionally does not re-check, so the
// guard lives in exactly one place (the daemon call site).
func SetACPProvider(mgr *acp.Manager, broker *secrets.Broker) {
	acpProviderFactoryFunc = func() toolprov.Provider {
		return &acpProvider{mgr: mgr, broker: broker}
	}
}

// acpProvider is the toolprov.Provider for the acp tool group.
type acpProvider struct {
	mgr    *acp.Manager
	broker *secrets.Broker
}

// Name implements toolprov.Provider.
func (p *acpProvider) Name() string { return "acp" }

// Discover implements toolprov.Provider. It mirrors the former direct
// registration exactly: the same single tool from the same constructor
// with the same argument.
func (p *acpProvider) Discover() []tools.Tool {
	return []tools.Tool{
		NewACPAgentTool(p.mgr),
	}
}

// PermissionClass implements toolprov.Provider. The acp_agent tool
// launches and drives EXTERNAL AGENT SUBPROCESSES - the privileged
// ceiling is the whole point of declaring it.
func (p *acpProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskPrivileged
}

// Broker implements toolprov.Provider.
func (p *acpProvider) Broker() *secrets.Broker {
	return p.broker
}
