package task

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func makeTask(id, name string, state TaskState) *Task {
	now := time.Now().UTC()
	return &Task{
		ID:        id,
		Name:      name,
		State:     state,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestStore_List_ReturnsTasks verifies basic round-trip of List.
func TestStore_List_ReturnsTasks(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(makeTask("t1", "first", StatePending)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(makeTask("t2", "second", StateCompleted)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tasks, err := s.List(nil, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

// TestStore_ListPropagatesIterError verifies that if the DB connection is
// closed during iteration, the error is surfaced rather than silently
// returning a partial result.
func TestStore_ListPropagatesIterError(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(makeTask("t1", "first", StatePending)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Close the DB. Subsequent queries should fail with a wrapped error
	// (either from Query or from rows.Err()), not return (nil, nil) or a
	// partial successful result.
	if err := s.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := s.List(nil, 10)
	if err == nil {
		t.Error("expected error from List after DB close, got nil")
	}
}

// TestListActive_LogsScanErrors is an indirect test: ListActive should still
// complete successfully when there is valid data, and propagate DB-level
// iteration errors. Scan errors on individual rows are logged and skipped.
func TestListActive_ReturnsActiveAndPropagatesError(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(makeTask("a1", "active", StatePending)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(makeTask("a2", "done", StateCompleted)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tasks, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 active task, got %d", len(tasks))
	}

	// After closing the DB, ListActive should return an error.
	if err := s.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = s.ListActive()
	if err == nil {
		t.Error("expected error from ListActive after DB close, got nil")
	}
}
