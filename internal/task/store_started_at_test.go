package task

import (
	"testing"
	"time"
)

// TestStore_StartedAt_Roundtrip pins the "completed in 0s" defect
// (2026-10-22): StartedAt was set by SetState on pending→active but never
// persisted — no column, no INSERT/UPDATE/SELECT. The finalize path
// re-reads the task from the store, so task.completed carried
// execution_time "0s" for a 35s job. StartedAt must survive
// Create→GetByID and Update→GetByID roundtrips; a zero StartedAt must
// scan back as zero (NULL), and Update must not clobber a persisted
// StartedAt with a zero value (COALESCE).
func TestStore_StartedAt_Roundtrip(t *testing.T) {
	s := newTestStore(t)

	tk := makeTask("t-started", "task", StatePending)
	if err := s.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Zero StartedAt scans back zero.
	got, err := s.GetByID("t-started")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.StartedAt.IsZero() {
		t.Fatalf("StartedAt = %v, want zero", got.StartedAt)
	}

	// SetState stamps StartedAt; Update must persist it.
	got.SetState(StateExecuting)
	started := got.StartedAt
	if started.IsZero() {
		t.Fatal("SetState(StateExecuting) did not stamp StartedAt")
	}
	if err := s.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, err := s.GetByID("t-started")
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	// RFC3339 persistence is second-precision (matches created_at/updated_at).
	if !got2.StartedAt.Equal(started.Truncate(time.Second)) {
		t.Fatalf("StartedAt = %v, want %v (not persisted)", got2.StartedAt, started)
	}
	if got2.ExecutionTime() <= 0 {
		t.Fatalf("ExecutionTime() = %v, want > 0", got2.ExecutionTime())
	}

	// A later Update with a zero-StartedAt snapshot (e.g. a stale
	// in-memory copy) must NOT erase the persisted stamp (COALESCE).
	stale := *got2
	stale.StartedAt = time.Time{}
	if err := s.Update(&stale); err != nil {
		t.Fatalf("Update stale: %v", err)
	}
	got3, err := s.GetByID("t-started")
	if err != nil {
		t.Fatalf("GetByID after stale update: %v", err)
	}
	if got3.StartedAt.IsZero() {
		t.Fatal("zero-StartedAt Update erased the persisted started_at (COALESCE missing)")
	}

	// Terminal-state ExecutionTime uses UpdatedAt-StartedAt. Backdate
	// StartedAt (RFC3339 persistence is second-precision; a same-second
	// start/finish is legitimately 0s).
	got3.StartedAt = time.Now().UTC().Add(-90 * time.Second).Truncate(time.Second)
	got3.SetState(StateCompleted)
	if err := s.Update(got3); err != nil {
		t.Fatalf("Update completed: %v", err)
	}
	got4, err := s.GetByID("t-started")
	if err != nil {
		t.Fatalf("GetByID after complete: %v", err)
	}
	if got4.ExecutionTime() <= 0 {
		t.Fatalf("terminal ExecutionTime() = %v, want > 0", got4.ExecutionTime())
	}
}

// TestStore_Migration_AddsStartedAtColumn proves an EXISTING pre-migration
// database gains the started_at column on reopen (legacy rows keep NULL).
func TestStore_Migration_AddsStartedAtColumn(t *testing.T) {
	dbPath := t.TempDir() + "/tasks.db"

	// Open once (creates schema with started_at), close, reopen: the
	// ALTER TABLE migration path runs against the existing db and must
	// be idempotent (duplicate-column errors are tolerated).
	s1, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore first: %v", err)
	}
	tk := makeTask("t-legacy", "legacy", StatePending)
	if err := s1.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s1.Close()

	s2, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore reopen: %v", err)
	}
	defer s2.Close()
	got, err := s2.GetByID("t-legacy")
	if err != nil {
		t.Fatalf("GetByID after reopen: %v", err)
	}
	if !got.StartedAt.IsZero() {
		t.Fatalf("legacy StartedAt = %v, want zero", got.StartedAt)
	}
}
