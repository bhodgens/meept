// Package builtin — delegate_register.go populates the toolprov provider seam
// for the model-facing delegate child-control tools (dsh-convergence leaf
// docs/plans/20261102-dsh-convergence/01-delegation/03-child-control.md,
// contract C2): subagent_send_message / subagent_interrupt /
// subagent_list_children.
//
// The tools' construction input — the DelegateController bound to the live C1
// DelegateManager, the Dispatcher and the ACP manager — is a runtime value
// only the daemon can supply, so this registers a provider FACTORY whose
// handle is captured at Discover time via SetDelegateProvider — the same
// acp_register_tools.go / goal_register.go pattern. An unbound factory fails
// closed: toolprov.RegisterAll rejects the nil Provider and the delegate
// control tools stay absent from the registry until the daemon wiring binds
// and enables the provider (byte-identical to "not registered").
package builtin

import (
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

func init() {
	if err := toolprov.Register("delegate", delegateProviderFactory); err != nil {
		panic("builtin: delegate provider registration: " + err.Error())
	}
}

// delegateProviderFactoryFunc builds the delegate provider from the handle a
// Discover-time caller captures. Declared as a variable so the daemon can
// bind the live controller before toolprov.RegisterAll constructs the
// provider.
var delegateProviderFactoryFunc func() toolprov.Provider

// delegateProviderFactory is the thunk handed to toolprov.Register. It
// defers to delegateProviderFactoryFunc; an unbound factory (no
// SetDelegateProvider yet — the default) returns nil and toolprov.RegisterAll
// fails closed on the nil Provider.
func delegateProviderFactory() toolprov.Provider {
	if delegateProviderFactoryFunc == nil {
		return nil
	}
	return delegateProviderFactoryFunc()
}

// ResetDelegateProviderForTest restores the unbound (default) factory state
// in tests that run enabled wiring before the fail-closed default. Production
// never calls this.
func ResetDelegateProviderForTest() {
	delegateProviderFactoryFunc = nil
}

// SetDelegateProvider binds the delegate controller to the provider seam. A
// typed-nil controller — either a true nil or a nil-boxed interface — leaves
// the factory unbound so the provider fails closed and no delegate control
// tool arms against a nil controller.
func SetDelegateProvider(ctrl *DelegateController) {
	if reflectNil(ctrl) {
		delegateProviderFactoryFunc = nil
		return
	}
	delegateProviderFactoryFunc = func() toolprov.Provider {
		return &delegateProvider{ctrl: ctrl}
	}
}

// delegateProvider is the toolprov.Provider for the delegate tool group.
type delegateProvider struct {
	ctrl *DelegateController
}

// Name implements toolprov.Provider.
func (p *delegateProvider) Name() string { return "delegate" }

// Discover implements toolprov.Provider. All three control tools plus the
// workflow orchestration tool share the same bound controller.
func (p *delegateProvider) Discover() []tools.Tool {
	return []tools.Tool{
		NewSubagentSendMessageTool(p.ctrl),
		NewSubagentInterruptTool(p.ctrl),
		NewSubagentListChildrenTool(p.ctrl),
		// workflow: the confined, model-authored orchestration tool (contract
		// C3). It fans out through the controller's C1 DelegateManager.
		NewWorkflowTool(p.ctrl),
		// workflow_status: background wf-job status (leaf 07) — reads the
		// same controller's bounded job registry. Read-only.
		NewWorkflowStatusTool(p.ctrl),
	}
}

// PermissionClass implements toolprov.Provider. The delegate control tools
// steer and interrupt delegated child AGENT subprocesses across trust
// boundaries — the privileged ceiling is declared deliberately.
func (p *delegateProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskPrivileged
}

// Broker implements toolprov.Provider. The delegate control tools hold no
// secret material, so the broker binding is nil.
func (p *delegateProvider) Broker() *secrets.Broker { return nil }

// Compile-time interface assertion.
var _ toolprov.Provider = (*delegateProvider)(nil)
