package agent

import (
	"encoding/json"
	"testing"
)

func TestParsePhaseOutput_Valid(t *testing.T) {
	raw := `{"phases":[{"name":"P1","description":"x","steps":[{"description":"a","tool_hint":"code"}],"produces":[{"name":"f","kind":"file","required":true}],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw, 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) != 1 {
		t.Fatalf("got %d phases; want 1", len(out.Phases))
	}
	if out.Phases[0].Produces[0].Name != "f" {
		t.Errorf("produce name = %q", out.Phases[0].Produces[0].Name)
	}
}

func TestParsePhaseOutput_RepairDanglingConsumes(t *testing.T) {
	raw := `{"phases":[{"name":"P1","steps":[],"produces":[],"consumes":[{"name":"ghost","required":true}],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw, 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Dangling required consume should be downgraded to optional (or dropped).
	if len(out.Phases[0].Consumes) > 0 && out.Phases[0].Consumes[0].Required {
		t.Errorf("dangling required consume should be repaired to optional or dropped")
	}
}

func TestParsePhaseOutput_RepairInvalidKind(t *testing.T) {
	raw := `{"phases":[{"name":"P1","steps":[],"produces":[{"name":"x","kind":"banana","required":true}],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw, 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Phases[0].Produces[0].Kind != "file" {
		t.Errorf("invalid kind should be repaired to 'file'; got %q", out.Phases[0].Produces[0].Kind)
	}
}

func TestParsePhaseOutput_EmptyPhasesDropped(t *testing.T) {
	raw := `{"phases":[{"name":"","steps":[],"produces":[],"consumes":[],"depends_on":[]},{"name":"P1","steps":[{"description":"x"}],"produces":[],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw, 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) != 1 {
		t.Errorf("empty phases should be dropped; got %d", len(out.Phases))
	}
}

func TestParsePhaseOutput_CapsPhaseCount(t *testing.T) {
	var phases []map[string]any
	for i := 0; i < 50; i++ {
		phases = append(phases, map[string]any{
			"name":  "P",
			"steps": []map[string]any{{"description": "x"}},
		})
	}
	raw, _ := json.Marshal(map[string]any{"phases": phases})
	out, err := parsePhaseOutput(string(raw), 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) > 12 {
		t.Errorf("phase count = %d; want <= 12", len(out.Phases))
	}
}

// --- Deferred checkPhaseReady tests (migrated from Task 1) ---

func TestCheckPhaseReady_MissingRequired(t *testing.T) {
	phase := &PlanPhaseSpec{
		Name: "P2",
		Consumes: []Artifact{
			{Name: "ghost", Required: true},
		},
	}
	store := newArtifactStore()
	err := checkPhaseReady(phase, store)
	if err == nil {
		t.Fatal("expected error for missing required consume")
	}
}

func TestCheckPhaseReady_OptionalMissingOK(t *testing.T) {
	phase := &PlanPhaseSpec{
		Name: "P2",
		Consumes: []Artifact{
			{Name: "ghost", Required: false},
		},
	}
	store := newArtifactStore()
	if err := checkPhaseReady(phase, store); err != nil {
		t.Errorf("optional missing consume should not error: %v", err)
	}
}

func TestCheckPhaseReady_RequiredPresent(t *testing.T) {
	phase := &PlanPhaseSpec{
		Name: "P2",
		Consumes: []Artifact{
			{Name: "spec", Required: true},
		},
	}
	store := newArtifactStore()
	store.Add(Artifact{Name: "spec", Kind: "file"}, "step-1")
	if err := checkPhaseReady(phase, store); err != nil {
		t.Errorf("required consume present should not error: %v", err)
	}
}
