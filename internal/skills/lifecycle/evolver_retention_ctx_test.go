package lifecycle

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/skills"
)

// TestEvolver_RetentionRunsOnExpiredCycleContext pins the retention-pass
// context fix (observed live 2026-10-23): the retention pass runs at the END
// of RunCycle, after ~9 min of LLM calls, so the cycle context has typically
// expired and PruneStalePlans' store reads failed with "context deadline
// exceeded" - the prune never ran. The pass must use its OWN short-lived
// context so it still executes even when the cycle context is already done.
//
// The test runs RunCycle with an ALREADY-CANCELLED context: the proposal
// passes fail fast (they honor the cycle ctx), but the retention pass must
// still prune an old draft plan via its independent context.
func TestEvolver_RetentionRunsOnExpiredCycleContext(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	registry := skills.NewRegistry()
	writer.SetRegistry(registry)

	usage := newStubUsageTracker()
	usage.SetLowPerformers(nil) // no proposals; only the retention pass runs

	evolver, planMgr, store := newDedupeTestEvolver(t, usage, writer, registry)
	_ = planMgr

	// Seed an OLD draft plan (30 days ago) directly into the sink store.
	old := time.Now().UTC().AddDate(0, 0, -30)
	seed := &plan.Plan{
		ID:        "old-draft",
		Title:     "old draft",
		State:     plan.StateDraft,
		FilePath:  filepath.Join(dir, "old-draft.md"),
		CreatedAt: old,
		UpdatedAt: old,
	}
	if err := store.CreatePlan(context.Background(), seed); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	// Run the cycle with an already-cancelled context. The proposal passes
	// honor the cycle ctx and fail fast; the retention pass must still run.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	evolver.cfg.PlanRetentionDays = 14
	if _, err := evolver.RunCycle(ctx); err != nil {
		t.Fatalf("RunCycle: %v", err)
	}

	// The old draft must have been pruned despite the expired cycle context.
	if _, err := store.GetPlan(context.Background(), "old-draft"); err == nil {
		t.Fatal("old draft plan still present after RunCycle with cancelled ctx - retention pass did not run")
	}
}
