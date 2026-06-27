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

// TestParsePhaseOutput_RemapsDependsOnAfterFiltering verifies that when empty
// phases are dropped during the repair pass, depends_on indices are remapped
// to the new (filtered) index space. Without the remap, a depends_on index
// pointing to a dropped phase would become a dangling reference.
func TestParsePhaseOutput_RemapsDependsOnAfterFiltering(t *testing.T) {
	// 3 phases: [empty(dropped), P1(depends_on:[0]), P2(depends_on:[0,1])]
	// After filtering: [P1, P2] where P1.depends_on should be []
	// (index 0 pointed to the dropped empty phase), and P2.depends_on should
	// be [0] (index 1 was P1, which survived at new index 0).
	raw := `{"phases":[
		{"name":"","steps":[],"produces":[],"consumes":[],"depends_on":[]},
		{"name":"P1","steps":[{"description":"a"}],"produces":[],"consumes":[],"depends_on":[0]},
		{"name":"P2","steps":[{"description":"b"}],"produces":[],"consumes":[],"depends_on":[0,1]}
	]}`
	out, err := parsePhaseOutput(raw, 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) != 2 {
		t.Fatalf("got %d phases; want 2 (empty phase should be dropped)", len(out.Phases))
	}
	// P1 (now at index 0) should have depends_on: [] (index 0 referred to
	// the dropped empty phase).
	if len(out.Phases[0].DependsOn) != 0 {
		t.Errorf("P1 depends_on = %v; want [] (original index 0 was dropped)", out.Phases[0].DependsOn)
	}
	// P2 (now at index 1) should have depends_on: [0] (original index 1 was
	// P1, which survived at new index 0; original index 0 was dropped).
	if len(out.Phases[1].DependsOn) != 1 || out.Phases[1].DependsOn[0] != 0 {
		t.Errorf("P2 depends_on = %v; want [0] (remapped from original [0,1])", out.Phases[1].DependsOn)
	}
}

// TestParsePhaseOutput_DropsDependsOnAfterCap verifies that when phase count
// exceeds maxPhases, the truncation re-validates depends_on and drops indices
// pointing past the truncated list.
func TestParsePhaseOutput_DropsDependsOnAfterCap(t *testing.T) {
	// 3 phases, all valid. Cap at 2. Phase 2 has depends_on: [2] which
	// becomes out-of-range after capping to 2 phases.
	raw := `{"phases":[
		{"name":"P1","steps":[{"description":"a"}],"produces":[],"consumes":[],"depends_on":[]},
		{"name":"P2","steps":[{"description":"b"}],"produces":[],"consumes":[],"depends_on":[0]},
		{"name":"P3","steps":[{"description":"c"}],"produces":[],"consumes":[],"depends_on":[2]}
	]}`
	out, err := parsePhaseOutput(raw, 2)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Phases) != 2 {
		t.Fatalf("got %d phases; want 2 (capped)", len(out.Phases))
	}
	// P3 was truncated. P2 should still have depends_on: [0].
	if len(out.Phases[1].DependsOn) != 1 || out.Phases[1].DependsOn[0] != 0 {
		t.Errorf("P2 depends_on = %v; want [0]", out.Phases[1].DependsOn)
	}
}

// TestParsePhaseOutput_NoPhasesSurvive verifies that the function returns an
// error when all phases are dropped during the repair pass.
func TestParsePhaseOutput_NoPhasesSurvive(t *testing.T) {
	raw := `{"phases":[
		{"name":"","steps":[],"produces":[],"consumes":[],"depends_on":[]},
		{"name":"","steps":[],"produces":[],"consumes":[],"depends_on":[]}
	]}`
	_, err := parsePhaseOutput(raw, 12)
	if err == nil {
		t.Fatal("expected error when no phases survive repair")
	}
}

// TestParsePhaseOutput_NestedJSONInStrings verifies that ExtractJSON correctly
// handles JSON objects with string values containing embedded JSON braces.
func TestParsePhaseOutput_NestedJSONInStrings(t *testing.T) {
	// The description field contains a string with braces that should NOT
	// confuse the brace-matching in ExtractJSON/findBalancedEnd.
	raw := `{"phases":[{"name":"P1","description":"use { for map init","steps":[{"description":"do x"}],"produces":[],"consumes":[],"depends_on":[]}]}`
	out, err := parsePhaseOutput(raw, 12)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Phases[0].Description != "use { for map init" {
		t.Errorf("description = %q; want %q", out.Phases[0].Description, "use { for map init")
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
