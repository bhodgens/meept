// Package security — pre_exec.go defines the PreExecChecker interface and
// PreExecDecision type used by the PermissionChecker's pre-execution hook.
//
// This interface mirrors internal/security.PreExecChecker so that the
// employee enforcement engine can register constitution-based checks with
// the leaf-level pkg/security.PermissionChecker without creating an import
// cycle. The employee package provides an adapter (internal/employee/preexec_adapter.go)
// that converts its native Decision type to PreExecDecision.
package security

// PreExecChecker is the interface implemented by the employee enforcement
// engine's pre-execution gate. When registered with PermissionChecker via
// SetPreExecChecker, the Check method is invoked BEFORE the financial/path/risk
// pipeline stages. A deny decision short-circuits the remaining checks.
//
// Implementations MUST be safe for concurrent use. The Check method may be
// called from multiple goroutines simultaneously.
type PreExecChecker interface {
	// Check evaluates a single tool call against the employee's
	// constitution. Returns a PreExecDecision describing whether the
	// call is allowed, denied, or escalated to plan signoff.
	Check(action, toolName string, details map[string]string) PreExecDecision
}

// PreExecDecision is the result of PreExecChecker.Check. When Allowed is
// false the permission checker blocks the action immediately. RequiresPlan
// triggers the NeedsConfirm flag on the resulting CheckResult so the caller
// routes the action through plan signoff. EscalateTo lists the agent IDs
// (or role sentinels like "role:user") that must approve an escalated
// action.
type PreExecDecision struct {
	Allowed      bool
	Reason       string
	RequiresPlan bool
	EscalateTo   []string
}
