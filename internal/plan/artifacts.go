package plan

// Artifact represents a produced or consumed work-product declared by a
// phase. Shared between PlanPhaseSpec (planner output), PlanPhase (persisted
// record), ParsedPhase (markdown round-trip), and StepHandoff (Thread B).
//
// Artifact lives in the plan package (lower-level than agent) to avoid
// import cycles: agent.artifactStore references plan.Artifact via a type
// alias (agent.Artifact = plan.Artifact).
type Artifact struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // file|interface|schema|decision|test_suite
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// IsValidKind returns true if the kind is one of the supported values.
func (a Artifact) IsValidKind() bool {
	switch a.Kind {
	case "file", "interface", "schema", "decision", "test_suite":
		return true
	}
	return false
}
