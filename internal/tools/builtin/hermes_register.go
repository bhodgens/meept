package builtin

import (
	"github.com/caimlas/meept/internal/hermes"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// hermes_register.go populates the toolprov provider seam for the
// hermes gateway tool group (contract C1,
// docs/plans/20261031-tool-provider-seam/03-migrate.md T2).
//
// The provider's construction inputs (the live *hermes.Client and the
// auto_approve knob) are runtime values the daemon resolves from
// [hermes] config, so this registers a provider FACTORY whose inputs
// are captured at Discover time via SetHermesProvider - the same
// Discover-time opt-capture pattern github_register.go and
// schedule_register.go use. The daemon call site replicates
// RegisterHermesTools' gating EXACTLY (enabled AND api key resolved)
// and binds the provider only in that case, so an unbound factory
// (disabled config, missing key) fails closed: toolprov.RegisterAll
// rejects the nil Provider and the hermes tool stays absent from the
// registry, matching the pre-seam "not registered" outcome. The API
// key material is held by the client only and never logged.
//
// The provider contributes the SAME tool (name "hermes") the former
// direct Register call built, so names and behaviors are identical to
// the pre-seam registration path.
func init() {
	if err := toolprov.Register("hermes", hermesProviderFactory); err != nil {
		panic("builtin: hermes provider registration: " + err.Error())
	}
}

// hermesProviderFactoryFunc builds the hermes provider from the inputs
// a Discover-time caller captures. Declared as a variable so the daemon
// can bind the live client before toolprov.RegisterAll constructs the
// provider.
var hermesProviderFactoryFunc func() toolprov.Provider

// hermesProviderFactory is the thunk handed to toolprov.Register. It
// defers to hermesProviderFactoryFunc; an unbound factory returns nil
// and toolprov.RegisterAll fails closed on the nil Provider.
func hermesProviderFactory() toolprov.Provider {
	if hermesProviderFactoryFunc == nil {
		return nil
	}
	return hermesProviderFactoryFunc()
}

// SetHermesProvider binds the hermes provider's construction inputs.
// The broker binding is the in-process secrets broker.
func SetHermesProvider(client *hermes.Client, autoApprove bool, broker *secrets.Broker) {
	hermesProviderFactoryFunc = func() toolprov.Provider {
		return &hermesProvider{client: client, autoApprove: autoApprove, broker: broker}
	}
}

// hermesProvider is the toolprov.Provider for the hermes tool group.
type hermesProvider struct {
	client      *hermes.Client
	autoApprove bool
	broker      *secrets.Broker
}

// Name implements toolprov.Provider.
func (p *hermesProvider) Name() string { return "hermes" }

// Discover implements toolprov.Provider. It mirrors the former direct
// registration exactly: the same single tool from the same constructor
// with the same arguments.
func (p *hermesProvider) Discover() []tools.Tool {
	return []tools.Tool{
		NewHermesTool(p.client, p.autoApprove),
	}
}

// PermissionClass implements toolprov.Provider. The hermes tool drives
// a remote gateway (chat/run/steer/stop over HTTP) - mutating state on
// the far side of the API; it spawns no local processes, so it is not
// privileged.
func (p *hermesProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskMutating
}

// Broker implements toolprov.Provider.
func (p *hermesProvider) Broker() *secrets.Broker {
	return p.broker
}
