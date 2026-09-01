package task

import (
	"path/filepath"
	"testing"
)

// newInteractiveStepStore builds a StepStore on a fresh DB, running the full
// migration path.
func newInteractiveStepStore(t *testing.T) *StepStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "steps.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store.StepStore()
}

// TestStep_SessionIDRoundTrip pins the R4 plumbing storage contract: the
// originating session provenance (tree 04 leaf 02) survives Create, Update,
// and scan. Without persistence the tactical scheduler's stamped SessionID
// would be lost on every step reload, making the interactive stamp dead for
// validation-retry jobs that rebuild payloads from the stored step.
func TestStep_SessionIDRoundTrip(t *testing.T) {
	store := newInteractiveStepStore(t)

	step := NewTaskStep("task-sess-1", "step with session provenance", 1)
	step.SessionID = "sess-abc-123"
	if err := store.Create(step); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.SessionID != "sess-abc-123" {
		t.Errorf("after Create: SessionID = %q, want %q", got.SessionID, "sess-abc-123")
	}

	// Update path (evidence persistence etc.) must not drop the session.
	got.SessionID = "sess-updated-456"
	if err := store.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.SessionID != "sess-updated-456" {
		t.Errorf("after Update: SessionID = %q, want %q", got2.SessionID, "sess-updated-456")
	}

	// Zero-value stays empty, not stale.
	orphan := NewTaskStep("task-sess-1", "step without session", 2)
	if orphan.SessionID != "" {
		t.Errorf("new step SessionID should default to empty, got %q", orphan.SessionID)
	}
}
