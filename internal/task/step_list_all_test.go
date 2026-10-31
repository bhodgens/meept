package task

import (
	"testing"
	"time"
)

// TestStepStore_ListAllSteps pins the read-only full listing added for the
// activity aggregator: every step across tasks, ordered by updated_at.
func TestStepStore_ListAllSteps(t *testing.T) {
	store := newTestStepStore(t)

	earlier := time.Now().UTC().Add(-2 * time.Hour)
	later := time.Now().UTC()

	s2 := NewTaskStep("task-b", "b step", 1)
	s2.CreatedAt = later
	s2.UpdatedAt = later
	if err := store.Create(s2); err != nil {
		t.Fatalf("Create s2: %v", err)
	}
	s1 := NewTaskStep("task-a", "a step", 1)
	s1.CreatedAt = earlier
	s1.UpdatedAt = earlier
	if err := store.Create(s1); err != nil {
		t.Fatalf("Create s1: %v", err)
	}

	got, err := store.ListAllSteps()
	if err != nil {
		t.Fatalf("ListAllSteps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != s1.ID || got[1].ID != s2.ID {
		t.Errorf("order = [%s, %s], want [%s, %s]", got[0].ID, got[1].ID, s1.ID, s2.ID)
	}
	if got[0].TaskID != "task-a" || got[1].TaskID != "task-b" {
		t.Errorf("task ids wrong: %s, %s", got[0].TaskID, got[1].TaskID)
	}

	// Empty store returns nil slice, no error.
	empty, err := newTestStepStore(t).ListAllSteps()
	if err != nil {
		t.Fatalf("empty ListAllSteps: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("empty store returned %d steps", len(empty))
	}
}
