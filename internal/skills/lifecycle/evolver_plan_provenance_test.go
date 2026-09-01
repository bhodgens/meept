package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// setupProvenanceTestManager creates a real PlanManager backed by a temp
// SQLite store with an ExternalPath sink inside the temp dir, so provenance
// round-trip tests exercise the real writer path (WritePlanMarkdown) — never
// the developer's home.
func setupProvenanceTestManager(t *testing.T) (*plan.PlanManager, string) {
	t.Helper()
	logger := slog.Default()
	dir := t.TempDir()
	store, err := plan.NewSQLiteStore(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	sink := filepath.Join(dir, "sink", "evolver")
	cfg := config.PlansConfig{
		Mode: "off",
		Storage: config.PlansStorageConfig{
			ExternalPath: sink,
		},
	}
	return plan.NewPlanManager(store, nil, cfg, nil, logger), sink
}

// TestEvolverPlanProvenance_StampAndAccessors verifies that stamping a plan
// with evolver provenance makes every accessor return the stamped values.
func TestEvolverPlanProvenance_StampAndAccessors(t *testing.T) {
	p := &plan.Plan{ID: "plan-test-0001"}
	StampEvolverPlan(p, "prop-001", "archive")
	if !IsEvolverPlan(p) {
		t.Fatal("IsEvolverPlan = false after stamping, want true")
	}
	if got := EvolverPlanProposalID(p); got != "prop-001" {
		t.Fatalf("EvolverPlanProposalID = %q, want %q", got, "prop-001")
	}
	if got := EvolverPlanAction(p); got != "archive" {
		t.Fatalf("EvolverPlanAction = %q, want %q", got, "archive")
	}
}

// TestEvolverPlanProvenance_NonEvolverPlanSafe verifies the accessors on a
// plain (human-authored) plan return false/empty without panicking.
func TestEvolverPlanProvenance_NonEvolverPlanSafe(t *testing.T) {
	var p plan.Plan
	if IsEvolverPlan(&p) {
		t.Fatal("IsEvolverPlan on plain plan = true, want false")
	}
	if got := EvolverPlanProposalID(&p); got != "" {
		t.Fatalf("EvolverPlanProposalID on plain plan = %q, want empty", got)
	}
	if got := EvolverPlanAction(&p); got != "" {
		t.Fatalf("EvolverPlanAction on plain plan = %q, want empty", got)
	}
}

// TestEvolverPlanProvenance_RoundTrip is the core leaf contract: create a
// plan through the evolver path (stamp + real writer), then read the file
// back through the real parser and recover the provenance via the accessors.
func TestEvolverPlanProvenance_RoundTrip(t *testing.T) {
	mgr, sink := setupProvenanceTestManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Skill evolution: archive hashline-file-editing", "Low effectiveness", "proj-1", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	// CreatePlan must have landed the file inside the sink, not a repo.
	if !strings.HasPrefix(p.FilePath, sink+string(filepath.Separator)) {
		t.Fatalf("plan file %q not under sink %q", p.FilePath, sink)
	}
	// Persist through the real writer path, stamp in place, and re-read the
	// file to recover provenance.
	if err := plan.WritePlanMarkdown(p.FilePath, p, nil); err != nil {
		t.Fatalf("WritePlanMarkdown: %v", err)
	}
	StampEvolverPlan(p, "prop-042", "archive")
	if !IsEvolverPlanFile(p.FilePath) {
		t.Fatal("IsEvolverPlanFile on re-read file = false, want true")
	}
	if got := EvolverPlanProposalIDFile(p.FilePath); got != "prop-042" {
		t.Fatalf("EvolverPlanProposalIDFile = %q, want %q", got, "prop-042")
	}
	if got := EvolverPlanActionFile(p.FilePath); got != "archive" {
		t.Fatalf("EvolverPlanActionFile = %q, want %q", got, "archive")
	}

	// The written file's Meta section carries the three provenance lines in
	// the existing `- key: value` plan format.
	data, err := os.ReadFile(p.FilePath)
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	meta := string(data)
	for _, want := range []string{
		"- origin: " + EvolverPlanOrigin,
		"- proposal_id: prop-042",
		"- action: archive",
	} {
		if !strings.Contains(meta, want) {
			t.Fatalf("plan file missing meta line %q\n---\n%s", want, meta)
		}
	}
}

// TestEvolverPlanProvenance_NilPlansSafe verifies nil-tolerance of the
// accessors (defensive; keeps call sites terse).
func TestEvolverPlanProvenance_NilPlansSafe(t *testing.T) {
	if IsEvolverPlan(nil) {
		t.Fatal("IsEvolverPlan(nil) = true, want false")
	}
	if EvolverPlanProposalID(nil) != "" || EvolverPlanAction(nil) != "" {
		t.Fatal("value accessors on nil plan must return empty")
	}
}
