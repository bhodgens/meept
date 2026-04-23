package task

import (
	"log/slog"
	"os"
	"testing"
)

func TestInterruptManager_GetOrCreate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mgr := NewInterruptManager(logger)

	// GetOrCreate should create new token
	tok1 := mgr.GetOrCreate("task-1")
	if tok1 == nil {
		t.Fatal("GetOrCreate should return non-nil token")
	}

	// Second call should return same token
	tok2 := mgr.GetOrCreate("task-1")
	if tok1 != tok2 {
		t.Fatal("GetOrCreate should return same token")
	}

	// Different task should get different token
	tok3 := mgr.GetOrCreate("task-2")
	if tok1 == tok3 {
		t.Fatal("different tasks should have different tokens")
	}
}

func TestInterruptManager_Get(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mgr := NewInterruptManager(logger)

	// Get non-existent task
	_, ok := mgr.Get("task-1")
	if ok {
		t.Fatal("should not find non-existent task")
	}

	// Create and get
	mgr.GetOrCreate("task-1")
	tok, ok := mgr.Get("task-1")
	if !ok {
		t.Fatal("should find existing task")
	}
	if tok == nil {
		t.Fatal("token should not be nil")
	}
}

func TestInterruptManager_Trigger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mgr := NewInterruptManager(logger)

	// Trigger non-existent task (should create and trigger)
	err := mgr.Trigger("task-1", ReasonUserCancelled, "test")
	if err != nil {
		t.Fatalf("Trigger failed: %v", err)
	}

	tok, ok := mgr.Get("task-1")
	if !ok {
		t.Fatal("token should exist after Trigger")
	}
	if !tok.IsTriggered() {
		t.Fatal("token should be triggered")
	}
	if tok.Reason() != ReasonUserCancelled {
		t.Errorf("got reason %v, want %v", tok.Reason(), ReasonUserCancelled)
	}
}

func TestInterruptManager_Remove(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mgr := NewInterruptManager(logger)

	mgr.GetOrCreate("task-1")
	mgr.Remove("task-1")

	_, ok := mgr.Get("task-1")
	if ok {
		t.Fatal("token should be removed")
	}
}

func TestInterruptManager_ListActive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mgr := NewInterruptManager(logger)

	mgr.GetOrCreate("task-1")
	mgr.GetOrCreate("task-2")
	mgr.GetOrCreate("task-3")

	ids := mgr.ListActive()
	if len(ids) != 3 {
		t.Fatalf("expected 3 active IDs, got %d", len(ids))
	}

	// Check all IDs are present
	found := make(map[string]bool)
	for _, id := range ids {
		found[id] = true
	}
	if !found["task-1"] || !found["task-2"] || !found["task-3"] {
		t.Errorf("missing expected task IDs in %v", ids)
	}
}

func TestInterruptManager_Close(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mgr := NewInterruptManager(logger)

	mgr.GetOrCreate("task-1")
	mgr.GetOrCreate("task-2")

	err := mgr.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// All tokens should be triggered
	tok1, ok := mgr.Get("task-1")
	if ok && !tok1.IsTriggered() {
		t.Fatal("token should be triggered after Close")
	}

	// Tokens map should be cleared
	if len(mgr.tokens) != 0 {
		t.Errorf("tokens map should be empty, got %d", len(mgr.tokens))
	}
}
