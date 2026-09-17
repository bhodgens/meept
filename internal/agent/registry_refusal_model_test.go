package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agents"
)

// --- refusal_model config surface (refusal-fallback tree 02 leaf 02) ---

// TestRefusalModel_DefinitionToSpec mirrors the escalation_model surface
// test: the per-agent refusal_model field flows frontmatter → definition
// → spec.
func TestRefusalModel_DefinitionToSpec(t *testing.T) {
	r := &AgentRegistry{logger: silentLogger()}
	def := &agents.AgentDefinition{
		AgentMetadata: agents.AgentMetadata{
			ID:           "ref-agent",
			Name:         "Refusal Agent",
			Role:         "executor",
			RefusalModel: "mlx-local/uncensored-8b",
		},
	}

	spec := r.definitionToSpec(def)
	if spec.RefusalModel != "mlx-local/uncensored-8b" {
		t.Errorf("definitionToSpec RefusalModel = %q, want %q", spec.RefusalModel, "mlx-local/uncensored-8b")
	}

	// Empty definition field maps to empty spec field (inherit global/off).
	def.RefusalModel = ""
	if got := r.definitionToSpec(def).RefusalModel; got != "" {
		t.Errorf("definitionToSpec RefusalModel = %q, want empty (inherit)", got)
	}
}

// TestRefusalModel_MergeSpec_Reload: a spec already carries refusal_model
// from a prior AGENT.md load; mergeSpec must preserve it, and a re-load
// carrying its own value overrides the base (prefer AGENT.md).
func TestRefusalModel_MergeSpec_Reload(t *testing.T) {
	r := &AgentRegistry{logger: silentLogger()}
	base := &AgentSpec{
		ID:           "ref-agent",
		Name:         "Refusal Agent",
		Role:         RoleExecutor,
		Enabled:      true,
		RefusalModel: "mlx-local/uncensored-8b",
	}
	def := &agents.AgentDefinition{
		AgentMetadata: agents.AgentMetadata{ID: "ref-agent", Name: "Refusal Agent", Role: "executor"},
	}

	merged := r.mergeSpec(base, def)
	if merged.RefusalModel != "mlx-local/uncensored-8b" {
		t.Errorf("mergeSpec RefusalModel = %q, want %q (preserve base)", merged.RefusalModel, "mlx-local/uncensored-8b")
	}

	def.RefusalModel = "other-alias"
	merged = r.mergeSpec(base, def)
	if merged.RefusalModel != "other-alias" {
		t.Errorf("mergeSpec RefusalModel = %q, want %q (prefer AGENT.md)", merged.RefusalModel, "other-alias")
	}
}

// TestRefusalModel_JSONRoundTrip verifies the field parses from employee
// JSON5 (encoding/json parses the JSON5-legal subset), an absent key
// leaves it empty, and it marshals back with the exact refusal_model key.
func TestRefusalModel_JSONRoundTrip(t *testing.T) {
	data := []byte(`{"id": "ref-agent", "name": "Refusal Agent", "role": "executor", "refusal_model": "mlx-local/uncensored-8b"}`)
	var spec AgentSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if spec.RefusalModel != "mlx-local/uncensored-8b" {
		t.Fatalf("spec.RefusalModel = %q, want %q", spec.RefusalModel, "mlx-local/uncensored-8b")
	}

	out, err := json.Marshal(&spec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"refusal_model":"mlx-local/uncensored-8b"`) {
		t.Fatalf("marshaled spec missing refusal_model key: %s", out)
	}

	// Absent key leaves the field empty.
	var empty AgentSpec
	if err := json.Unmarshal([]byte(`{"id": "x"}`), &empty); err != nil {
		t.Fatalf("Unmarshal empty: %v", err)
	}
	if empty.RefusalModel != "" {
		t.Fatalf("absent key produced %q, want empty", empty.RefusalModel)
	}
}
