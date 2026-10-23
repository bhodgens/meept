package lifecycle

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// TestEvolver_PassC_SkipsStaleTarget pins the stale-target guard (decision
// brief 2026-10-22 item 2): a low-performer whose skill directory is already
// gone from disk must be SKIPPED, never proposed for archive. Before this
// guard, Pass C proposed archiving deleted skills off stale usage stats and
// every such apply failed with skills.ErrSkillNotFound - the 0/252 record.
func TestEvolver_PassC_SkipsStaleTarget(t *testing.T) {
	missing := "already-deleted-skill"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	// Deliberately DO NOT write the skill - usage stats reference a skill
	// that no longer exists on disk (the stale-stats case).
	registry := skills.NewRegistry()
	writer.SetRegistry(registry)

	usage := newStubUsageTracker()
	usage.SetLowPerformers([]*UsageStats{
		{SkillName: missing, InjectCount: 15, Effectiveness: 0.1},
	})

	cfg := defaultEvolverConfig()
	// AutoApply=false + nil planMgr makes the guard observable: WITH the
	// guard, passCPrune counts the stale target as Skipped and returns
	// before processProposal. WITHOUT it, processProposal runs the verifier
	// but (AutoApply off, no planMgr) neither applies nor plans, so Skipped
	// stays 0. Skipped>=1 therefore proves the guard fired and no doomed
	// archive/plan was attempted.
	cfg.AutoApply = false

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		newTestVerifier(true), &mockLLMChatter{}, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Pruned != 0 {
		t.Fatalf("Pruned = %d, want 0 (a stale target must not be archived)", report.Pruned)
	}
	if report.Skipped < 1 {
		t.Fatalf("Skipped = %d, want >=1 (guard must skip the stale target before proposing an archive)", report.Skipped)
	}
	// No archive directory should have been created for the missing skill.
	archivedPath := filepath.Join(dir+".archived", missing, "SKILL.md")
	if _, statErr := os.Stat(archivedPath); statErr == nil {
		t.Fatalf("stale target was archived to %s; guard did not fire", archivedPath)
	}
}
