package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdatePlanStatus_PreservesExtraMeta pins the round-trip fidelity the
// approval actuator depends on: UpdatePlanStatus rewrites the plan file from
// a parse → write cycle, and unknown `## Meta` keys (the skill-evolver
// provenance stamp origin/proposal_id/action, plus the applied marker) must
// survive. Regression for the leaf-03 discovery that status updates silently
// stripped machine-readable provenance.
func TestUpdatePlanStatus_PreservesExtraMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill-evolution-archive-x.md")

	p := NewPlan("Skill evolution: archive x", "d", "", path, "")
	if err := WritePlanMarkdown(path, p, nil); err != nil {
		t.Fatalf("WritePlanMarkdown: %v", err)
	}
	// Stamp provenance the way the evolver does (leaf 01 encoding).
	base, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	stamped := strings.Replace(string(base),
		"- status: planning\n",
		"- status: planning\n- origin: skill-evolver\n- proposal_id: evo-archive-x-000001\n- action: archive\n",
		1)
	if err := os.WriteFile(path, []byte(stamped), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	// The status update Synthesize performs after approval.
	if err := UpdatePlanStatus(path, StateExecuting, nil); err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}

	after, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"origin: skill-evolver",
		"proposal_id: evo-archive-x-000001",
		"action: archive",
		"status: executing",
	} {
		if !strings.Contains(string(after), want) {
			t.Errorf("round-trip lost or mangled %q\nfile:\n%s", want, after)
		}
	}
}
