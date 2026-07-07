package daemon

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// TestReflectionProposerAdapter_FieldMapping verifies that the adapter
// correctly converts agent.ReflectionProposal to lifecycle.ReflectionProposal
// field-by-field. This is the boundary between the agent package's reflection
// system and the lifecycle package's evolver; field drops here would silently
// lose data.
func TestReflectionProposerAdapter_FieldMapping(t *testing.T) {
	// Construct a real ReflectionCollector with a test queue path so
	// DrainPendingProposals has something to drain.
	tmp := t.TempDir()
	queuePath := filepath.Join(tmp, "improvements.md")
	rc := agent.NewReflectionCollector(
		config.ReflectionCollectorConfig{Enabled: true},
		nil, // classifier — not needed; we Append directly
		"",
		nil,             // templateReg — not needed for drain
		queuePath,
		slog.Default(),
	)

	// Append a proposal with all fields populated.
	src := agent.ReflectionProposal{
		Type:          "skill_create",
		Target:        "my-skill",
		Change:        "# content",
		Justification: "observed pattern",
		Confidence:    0.88,
		Source:        "test",
	}
	if err := agent.NewExternalProposalQueue(queuePath).Append(src); err != nil {
		t.Fatalf("Append: %v", err)
	}

	adapter := &reflectionProposerAdapter{rc: rc}
	got, err := adapter.DrainPending()
	if err != nil {
		t.Fatalf("DrainPending: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(got))
	}

	want := lifecycle.ReflectionProposal{
		Type:          "skill_create",
		Target:        "my-skill",
		Change:        "# content",
		Justification: "observed pattern",
		Confidence:    0.88,
	}
	if got[0] != want {
		t.Errorf("field mapping mismatch:\n  got:  %+v\n  want: %+v", got[0], want)
	}

	// Verify drain is destructive — a second call returns nothing.
	second, err := adapter.DrainPending()
	if err != nil {
		t.Fatalf("second DrainPending: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("expected 0 on second drain, got %d", len(second))
	}
}

// TestReflectionProposerAdapter_NilSafe verifies that the adapter does not
// panic when constructed with a nil ReflectionCollector. The evolver treats
// nil proposer output as "no proposals this cycle".
func TestReflectionProposerAdapter_NilSafe(t *testing.T) {
	var adapter *reflectionProposerAdapter // nil pointer
	got, err := adapter.DrainPending()
	if err != nil {
		t.Fatalf("nil adapter DrainPending: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil from nil adapter, got %v", got)
	}

	// Also test the zero-value (non-nil pointer, nil rc field).
	adapter2 := &reflectionProposerAdapter{rc: nil}
	got2, err2 := adapter2.DrainPending()
	if err2 != nil {
		t.Fatalf("nil rc DrainPending: %v", err2)
	}
	if got2 != nil {
		t.Errorf("expected nil from nil rc, got %v", got2)
	}
}

// TestReflectionProposerAdapter_SatisfiesInterface is a compile-time check
// that *reflectionProposerAdapter implements lifecycle.ReflectionProposer.
// If the interface drifts, this fails at compile time rather than at runtime
// in the daemon wiring.
func TestReflectionProposerAdapter_SatisfiesInterface(t *testing.T) {
	var _ lifecycle.ReflectionProposer = (*reflectionProposerAdapter)(nil)
}
