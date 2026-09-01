package agent

// ModelResolver resolves an alias name or "provider/model" ref to a
// usable model reference. Satisfied by *llm.Resolver (leaf 04) and tests.
type ModelResolver interface {
	// ResolveEscalationRef returns a model ref string usable as a loop
	// model ref. Named ResolveEscalationRef (audit B2): *llm.Resolver
	// already has ResolveRef(ref string) *ModelConfig — a same-name /
	// different-signature method would not compile. Returns an error
	// when the ref names no alias/model.
	ResolveEscalationRef(ref string) (string, error)
}

// EscalationDecision is what handleFail should do on loop exhaustion.
type EscalationDecision struct {
	Escalate bool
	ModelRef string
	Reason   string // "fix_loops_exhausted" | "no_escalation_model" | "resolution_failed"
}

// DecideEscalation never errors; resolution failure degrades to
// Escalate=false with Reason "resolution_failed".
func DecideEscalation(spec *AgentSpec, resolver ModelResolver) EscalationDecision {
	// No configured escalation target: not an error, just disabled (D3).
	if spec == nil || spec.EscalationModel == "" {
		return EscalationDecision{Escalate: false, Reason: "no_escalation_model"}
	}

	// Nil resolver with a configured target cannot resolve; degrade to
	// the legacy user-escalation path rather than panic.
	if resolver == nil {
		return EscalationDecision{Escalate: false, Reason: "resolution_failed"}
	}

	resolved, err := resolver.ResolveEscalationRef(spec.EscalationModel)
	if err != nil {
		return EscalationDecision{Escalate: false, Reason: "resolution_failed"}
	}

	return EscalationDecision{
		Escalate: true,
		ModelRef: resolved,
		Reason:   "fix_loops_exhausted",
	}
}
