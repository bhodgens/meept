// Package employee — preexec_adapter.go bridges the employee enforcement
// engine's PreExecChecker (which returns the employee package's own Decision
// type) to the pkg/security.PreExecChecker interface (which returns
// pkg/security.PreExecDecision). The adapter lives in a separate file so
// that only this file imports pkg/security; the rest of the employee
// package stays free of that dependency edge.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md (Checkpoint 1,
// Gap A: Active-path PreExecChecker integration).
package employee

import (
	"github.com/caimlas/meept/pkg/security"
)

// preExecAdapter wraps an employee PreExecChecker and adapts it to the
// pkg/security.PreExecChecker interface. The employee Decision type carries
// Severity and EscalateTo fields that the pkg/security.PreExecDecision
// does not have; those are dropped (the security pipeline doesn't act on
// them — escalation routing is handled by the enforcement engine itself).
type preExecAdapter struct {
	inner *PreExecChecker
}

// NewPreExecAdapter wraps an employee PreExecChecker so it can be registered
// with pkg/security.PermissionChecker via SetPreExecChecker. Returns nil
// when inner is nil (typed-nil guard: callers must check before passing to
// SetPreExecChecker).
func NewPreExecAdapter(inner *PreExecChecker) security.PreExecChecker {
	if inner == nil {
		return nil
	}
	return preExecAdapter{inner: inner}
}

// Check implements security.PreExecChecker. Delegates to the employee
// PreExecChecker.Check and converts the returned Decision to
// security.PreExecDecision.
func (a preExecAdapter) Check(action, toolName string, details map[string]string) security.PreExecDecision {
	dec := a.inner.Check(action, toolName, details)
	return security.PreExecDecision{
		Allowed:      dec.Allowed,
		Reason:       dec.Reason,
		RequiresPlan: dec.RequiresPlan,
		EscalateTo:   dec.EscalateTo,
	}
}
