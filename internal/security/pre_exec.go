package security

// PreExecChecker is the interface implemented by the employee enforcement
// engine's pre-execution gate (internal/employee.PreExecChecker). The
// security engine calls Check between Stage 3 (context analysis) and
// Stage 4 (override check) for agents that have a registered checker.
//
// IMPORTANT: Check is invoked while the security Engine holds its mu as
// an RLock. Therefore implementations MUST NOT call any Engine methods
// that acquire the Engine lock (Check, RecordOverride, AllowOnce, etc.).
// The employee PreExecChecker performs only in-memory constitution
// comparisons — no I/O, no lock acquisition — so this contract is
// satisfied. See engine.go CheckForAgent for the call site.
type PreExecChecker interface {
	// Check evaluates a single tool call against the employee's
	// constitution. Returns a PreExecDecision describing whether the
	// call is allowed, denied, or escalated to plan signoff.
	Check(action, toolName string, details map[string]string) PreExecDecision
}

// PreExecDecision is the result of PreExecChecker.Check. When Allowed is
// false the security engine blocks the action. RequiresPlan triggers
// plan signoff for an escalation. EscalateTo lists the agent IDs (or role
// sentinels like "role:user") that must approve an escalated action.
type PreExecDecision struct {
	Allowed      bool
	Reason       string
	RiskLevel    RiskLevel
	RequiresPlan bool
	EscalateTo   []string
}
