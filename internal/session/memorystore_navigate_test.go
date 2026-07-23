package session

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// makeMessages builds n Message structs suitable for SaveMessages. BranchID is
// set to "main"; IDs are assigned by SaveMessages starting at 1.
func makeMessages(n int) []Message {
	msgs := make([]Message, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = Message{
			Role:      role,
			Content:   fmt.Sprintf("msg %d", i+1),
			EntryType: "message",
			BranchID:  "main",
			Timestamp: time.Now().UTC(),
		}
	}
	return msgs
}

func TestMemoryStore_NavigateToBranch_Basic(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	msgs := makeMessages(5)
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	// Set the current leaf to message 5.
	if err := store.SetLeafMessageID(session.ID, 5); err != nil {
		t.Fatalf("SetLeafMessageID failed: %v", err)
	}

	// Navigate to message 3.
	oldLeaf, err := store.NavigateToBranch(session.ID, 3)
	if err != nil {
		t.Fatalf("NavigateToBranch failed: %v", err)
	}
	if oldLeaf != 5 {
		t.Errorf("expected oldLeaf=5, got %d", oldLeaf)
	}

	// Verify the leaf is now 3.
	newLeaf, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("GetLeafMessageID failed: %v", err)
	}
	if newLeaf != 3 {
		t.Errorf("expected newLeaf=3, got %d", newLeaf)
	}
}

func TestMemoryStore_NavigateToBranch_NoPreviousLeaf(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	msgs := makeMessages(3)
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	// Note: leaf is NOT set before navigating.

	oldLeaf, err := store.NavigateToBranch(session.ID, 2)
	if err != nil {
		t.Fatalf("NavigateToBranch failed: %v", err)
	}
	if oldLeaf != 0 {
		t.Errorf("expected oldLeaf=0 (no previous leaf), got %d", oldLeaf)
	}

	newLeaf, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("GetLeafMessageID failed: %v", err)
	}
	if newLeaf != 2 {
		t.Errorf("expected newLeaf=2, got %d", newLeaf)
	}
}

func TestMemoryStore_NavigateToBranch_TargetNotFound(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	msgs := makeMessages(3)
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	if err := store.SetLeafMessageID(session.ID, 3); err != nil {
		t.Fatalf("SetLeafMessageID failed: %v", err)
	}

	oldLeaf, err := store.NavigateToBranch(session.ID, 999)
	if err == nil {
		t.Fatal("expected error for nonexistent target message, got nil")
	}
	if oldLeaf != 0 {
		t.Errorf("expected oldLeaf=0 on error, got %d", oldLeaf)
	}

	// Leaf should remain unchanged after a failed navigation.
	curLeaf, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("GetLeafMessageID failed: %v", err)
	}
	if curLeaf != 3 {
		t.Errorf("expected leaf unchanged at 3 after failed nav, got %d", curLeaf)
	}
}

func TestMemoryStore_NavigateToBranch_SessionNotFound(t *testing.T) {
	store := NewMemoryStore(nil)

	oldLeaf, err := store.NavigateToBranch("nonexistent-session", 1)
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
	if oldLeaf != 0 {
		t.Errorf("expected oldLeaf=0 on error, got %d", oldLeaf)
	}
}

func TestMemoryStore_NavigateToBranch_NoOpSameAsLeaf(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	msgs := makeMessages(3)
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	if err := store.SetLeafMessageID(session.ID, 3); err != nil {
		t.Fatalf("SetLeafMessageID failed: %v", err)
	}

	// Navigate to the same message that is already the leaf.
	oldLeaf, err := store.NavigateToBranch(session.ID, 3)
	if err != nil {
		t.Fatalf("NavigateToBranch (no-op) failed: %v", err)
	}
	if oldLeaf != 3 {
		t.Errorf("expected oldLeaf=3, got %d", oldLeaf)
	}

	curLeaf, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("GetLeafMessageID failed: %v", err)
	}
	if curLeaf != 3 {
		t.Errorf("expected leaf still 3, got %d", curLeaf)
	}
}

func TestMemoryStore_NavigateToBranch_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	msgs := makeMessages(10)
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	if err := store.SetLeafMessageID(session.ID, 10); err != nil {
		t.Fatalf("SetLeafMessageID failed: %v", err)
	}

	// Launch many goroutines navigating to various targets concurrently.
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	panics := make(chan any, goroutines)

	for i := 0; i < goroutines; i++ {
		// Target cycles through valid message IDs 1..10.
		target := int64(i%10 + 1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()

			_, err := store.NavigateToBranch(session.ID, target)
			if err != nil {
				errs <- err
				return
			}
			errs <- nil
		}()
	}

	wg.Wait()
	close(errs)
	close(panics)

	for p := range panics {
		t.Fatalf("goroutine panicked: %v", p)
	}

	// All calls target valid messages, so none should error.
	for e := range errs {
		if e != nil {
			t.Errorf("unexpected error from concurrent NavigateToBranch: %v", e)
		}
	}

	// Verify the store is still usable after concurrent access.
	finalLeaf, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("GetLeafMessageID failed after concurrent access: %v", err)
	}
	if finalLeaf < 1 || finalLeaf > 10 {
		t.Errorf("final leaf %d out of expected range [1,10]", finalLeaf)
	}
}
