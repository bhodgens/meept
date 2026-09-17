package agent

// Exported test seam for external-package tests (tests/) that must wire
// the unexported refusal-fallback fields on AgentLoop from outside the
// package. Production code never calls this; the daemon wires the same
// fields at construction (components.go) and in-package tests set the
// fields directly.
//
// refusal-fallback tree leaf 05: the e2e scenarios assert the FULL bus
// path for the agent.model_escalated event, which requires installing the
// refusalEventPublisher closure exactly the way the daemon does.

// RefusalSeams carries the injectable refusal-fallback collaborators.
type RefusalSeams struct {
	loop *AgentLoop
	// EventPublisher installs the agent.model_escalated publisher closure.
	EventPublisher func(topic string, payload map[string]any)
}

// ExportedRefusalSeams returns a mutable view of the loop's refusal-fallback
// seams. Call Apply to write any set fields back onto the loop.
func ExportedRefusalSeams(l *AgentLoop) *RefusalSeams {
	return &RefusalSeams{loop: l}
}

// Apply writes any configured seams onto the loop. Nil entries are skipped
// (plain-nil guard per repo convention).
func (s *RefusalSeams) Apply() {
	if s.loop == nil {
		return
	}
	if s.EventPublisher != nil {
		s.loop.refusalEventPublisher = s.EventPublisher
	}
}
