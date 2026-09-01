package agent

import "testing"

// TestAgentSpec_EscalationModel_DefaultEmpty verifies that the zero-value
// AgentSpec leaves EscalationModel empty, i.e. escalation is disabled
// (llm-resilience-forest tree 01 leaf 01-spec-config; D14: config surface
// only — the field is inert until the escalation hook consumes it).
func TestAgentSpec_EscalationModel_DefaultEmpty(t *testing.T) {
	spec := &AgentSpec{ID: "x", Enabled: true}
	if spec.EscalationModel != "" {
		t.Errorf("EscalationModel = %q, want empty (disabled)", spec.EscalationModel)
	}
}
