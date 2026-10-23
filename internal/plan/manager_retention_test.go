package plan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedPlan inserts a plan row directly through the store with an explicit
// UpdatedAt (backdatable), bypassing the manager's create path so retention
// tests control plan age precisely.
func seedPlan(t *testing.T, m *PlanManager, id string, state PlanState, updated time.Time, filePath string) {
	t.Helper()
	p := &Plan{
		ID:        id,
		Title:     "seed " + id,
		State:     state,
		FilePath:  filePath,
		CreatedAt: updated,
		UpdatedAt: updated,
	}
	if err := m.store.CreatePlan(context.Background(), p); err != nil {
		t.Fatalf("seedPlan(%s): %v", id, err)
	}
}

// TestPruneStalePlans deletes old draft/cancelled/failed rows (and their md
// files), cancels stale pending_approval, and never touches executing or
// completed plans or plans within the retention window.
func TestPruneStalePlans(t *testing.T) {
	m, dir := setupTestManagerWithDir(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -30) // 30 days old
	fresh := time.Now().UTC()

	// An old draft WITH a markdown file on disk: both must go.
	oldDraftFile := filepath.Join(dir, "old-draft.md")
	if err := os.WriteFile(oldDraftFile, []byte("# old draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPlan(t, m, "old-draft", StateDraft, old, oldDraftFile)

	// An old cancelled row: deleted.
	seedPlan(t, m, "old-cancelled", StateCancelled, old, "")

	// A stale pending_approval: cancelled (not deleted) on this pass.
	seedPlan(t, m, "stale-pending", StatePendingApproval, old, "")

	// An old EXECUTING plan: live work, never touched.
	seedPlan(t, m, "old-executing", StateExecuting, old, "")

	// An old COMPLETED plan: audit record, never touched.
	seedPlan(t, m, "old-completed", StateCompleted, old, "")

	// A FRESH draft (within window): kept.
	seedPlan(t, m, "fresh-draft", StateDraft, fresh, "")

	deleted, cancelled, err := m.PruneStalePlans(ctx, 14)
	if err != nil {
		t.Fatalf("PruneStalePlans: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (old-draft + old-cancelled)", deleted)
	}
	if cancelled != 1 {
		t.Fatalf("cancelled = %d, want 1 (stale-pending)", cancelled)
	}

	// The old draft's markdown file is gone.
	if _, statErr := os.Stat(oldDraftFile); !os.IsNotExist(statErr) {
		t.Fatalf("old draft md file still present after prune: %v", statErr)
	}

	// Row-level assertions.
	assertState := func(id string, want PlanState) {
		t.Helper()
		p, gerr := m.store.GetPlan(ctx, id)
		if gerr != nil {
			t.Fatalf("GetPlan(%s): %v", id, gerr)
		}
		if p.State != want {
			t.Fatalf("plan %s state = %s, want %s", id, p.State, want)
		}
	}
	assertDeleted := func(id string) {
		t.Helper()
		if _, gerr := m.store.GetPlan(ctx, id); gerr == nil {
			t.Fatalf("plan %s still present, want deleted", id)
		}
	}

	assertDeleted("old-draft")
	assertDeleted("old-cancelled")
	assertState("stale-pending", StateCancelled) // cancelled, not deleted
	assertState("old-executing", StateExecuting) // untouched
	assertState("old-completed", StateCompleted) // untouched
	assertState("fresh-draft", StateDraft)       // within window
}

// TestPruneStalePlans_DisabledOnNonPositive proves retentionDays<=0 is a no-op
// (the documented disable switch).
func TestPruneStalePlans_DisabledOnNonPositive(t *testing.T) {
	m, _ := setupTestManagerWithDir(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -90)
	seedPlan(t, m, "old-draft", StateDraft, old, "")

	deleted, cancelled, err := m.PruneStalePlans(ctx, 0)
	if err != nil || deleted != 0 || cancelled != 0 {
		t.Fatalf("PruneStalePlans(0) = (%d,%d,%v), want (0,0,nil)", deleted, cancelled, err)
	}
	if _, gerr := m.store.GetPlan(ctx, "old-draft"); gerr != nil {
		t.Fatalf("plan deleted despite retention disabled: %v", gerr)
	}
}

// TestPruneStalePlans_NilManager proves the nil-receiver guard (a manager that
// is nil must not panic; the evolver calls this on a possibly-nil planMgr).
func TestPruneStalePlans_NilManager(t *testing.T) {
	var m *PlanManager
	deleted, cancelled, err := m.PruneStalePlans(context.Background(), 14)
	if err != nil || deleted != 0 || cancelled != 0 {
		t.Fatalf("nil PruneStalePlans = (%d,%d,%v), want (0,0,nil)", deleted, cancelled, err)
	}
}
