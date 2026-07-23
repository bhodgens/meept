package session

import (
	"testing"
	"time"
)

// helper to fetch a single message by ID from the store.
func mustGetMessage(t *testing.T, store *MemoryStore, sessionID string, id int64) Message {
	t.Helper()
	msgs, err := store.GetMessages(sessionID, 0, 1000)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for _, m := range msgs {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("message id %d not found", id)
	return Message{}
}

func TestMemoryStore_ReparentAfterCompaction_Basic(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	pid1 := int64(1)
	pid2 := int64(2)
	msgs := []Message{
		{Role: "user", Content: "root", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid1, Role: "assistant", Content: "child1", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid2, Role: "user", Content: "child2", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
	}
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	// Before: msg3.ParentID == 2
	m2 := mustGetMessage(t, store, session.ID, 2)
	if m2.ParentID == nil || *m2.ParentID != 1 {
		t.Fatalf("msg2 parent before: got %v, want 1", m2.ParentID)
	}
	m3 := mustGetMessage(t, store, session.ID, 3)
	if m3.ParentID == nil || *m3.ParentID != 2 {
		t.Fatalf("msg3 parent before: got %v, want 2", m3.ParentID)
	}

	if err := store.ReparentAfterCompaction(session.ID, 2, 99); err != nil {
		t.Fatalf("ReparentAfterCompaction: %v", err)
	}

	// After: msg3.ParentID == 99, msg2 unchanged (== 1)
	m2 = mustGetMessage(t, store, session.ID, 2)
	if m2.ParentID == nil || *m2.ParentID != 1 {
		t.Fatalf("msg2 parent after: got %v, want 1 (unchanged)", m2.ParentID)
	}
	m3 = mustGetMessage(t, store, session.ID, 3)
	if m3.ParentID == nil || *m3.ParentID != 99 {
		t.Fatalf("msg3 parent after: got %v, want 99", m3.ParentID)
	}
}

func TestMemoryStore_ReparentAfterCompaction_MultipleChildren(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	pid1 := int64(1)
	msgs := []Message{
		{Role: "user", Content: "root", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid1, Role: "assistant", Content: "child1", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid1, Role: "user", Content: "child2", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid1, Role: "assistant", Content: "child3", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
	}
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	if err := store.ReparentAfterCompaction(session.ID, 1, 100); err != nil {
		t.Fatalf("ReparentAfterCompaction: %v", err)
	}

	for _, id := range []int64{2, 3, 4} {
		m := mustGetMessage(t, store, session.ID, id)
		if m.ParentID == nil || *m.ParentID != 100 {
			t.Fatalf("msg %d parent after: got %v, want 100", id, m.ParentID)
		}
	}

	// root unchanged (ParentID == nil)
	m1 := mustGetMessage(t, store, session.ID, 1)
	if m1.ParentID != nil {
		t.Fatalf("msg1 parent after: got %v, want nil", m1.ParentID)
	}
}

func TestMemoryStore_ReparentAfterCompaction_NoMatch(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	pid1 := int64(1)
	msgs := []Message{
		{Role: "user", Content: "root", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid1, Role: "assistant", Content: "child1", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
	}
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	// No message has parent == 999
	if err := store.ReparentAfterCompaction(session.ID, 999, 777); err != nil {
		t.Fatalf("ReparentAfterCompaction: %v", err)
	}

	m1 := mustGetMessage(t, store, session.ID, 1)
	if m1.ParentID != nil {
		t.Fatalf("msg1 parent: got %v, want nil", m1.ParentID)
	}
	m2 := mustGetMessage(t, store, session.ID, 2)
	if m2.ParentID == nil || *m2.ParentID != 1 {
		t.Fatalf("msg2 parent: got %v, want 1 (unchanged)", m2.ParentID)
	}
}

func TestMemoryStore_ReparentAfterCompaction_EmptySession(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// No messages saved. Should be a no-op returning nil.
	if err := store.ReparentAfterCompaction(session.ID, 1, 99); err != nil {
		t.Fatalf("ReparentAfterCompaction on empty session: got %v, want nil", err)
	}

	msgs, err := store.GetMessages(session.ID, 0, 100)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestMemoryStore_ReparentAfterCompaction_GetMessagePathAfter(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Build chain: 1 -> 2 -> 3 -> 4
	pid1 := int64(1)
	pid2 := int64(2)
	pid3 := int64(3)
	originalMsgs := []Message{
		{Role: "user", Content: "root", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid1, Role: "assistant", Content: "c1", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid2, Role: "user", Content: "c2", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
		{ParentID: &pid3, Role: "assistant", Content: "c3", EntryType: "message", BranchID: "main", Timestamp: time.Now().UTC()},
	}
	if err := store.SaveMessages(session.ID, originalMsgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	// Insert a compaction entry. It will receive the next sequential ID (5).
	compactionMsgs := []Message{
		{ParentID: &pid1, Role: "system", Content: "compaction summary", EntryType: "compaction", BranchID: "main", Timestamp: time.Now().UTC()},
	}
	if err := store.SaveMessages(session.ID, compactionMsgs); err != nil {
		t.Fatalf("SaveMessages compaction: %v", err)
	}

	compactionID := int64(5)

	// Reparent message 4's parent from 3 to the compaction entry (5).
	if err := store.ReparentAfterCompaction(session.ID, 3, compactionID); err != nil {
		t.Fatalf("ReparentAfterCompaction: %v", err)
	}

	// Verify reparent took effect.
	m4 := mustGetMessage(t, store, session.ID, 4)
	if m4.ParentID == nil || *m4.ParentID != compactionID {
		t.Fatalf("msg4 parent after reparent: got %v, want %d", m4.ParentID, compactionID)
	}

	// Verify GetMessagePath reaches the compaction node when walking from msg 4.
	// The path from msg4 should include the compaction entry (id 5).
	path, err := store.GetMessagePath(session.ID, 4)
	if err != nil {
		t.Fatalf("GetMessagePath: %v", err)
	}

	foundCompaction := false
	for _, m := range path {
		if m.ID == compactionID {
			foundCompaction = true
			if m.EntryType != "compaction" {
				t.Fatalf("compaction node EntryType: got %q, want %q", m.EntryType, "compaction")
			}
		}
	}
	if !foundCompaction {
		t.Fatalf("compaction node (id %d) not found in path; path ids: %v",
			compactionID, messageIDs(path))
	}
}

// messageIDs extracts IDs from a slice of messages (test helper).
func messageIDs(msgs []Message) []int64 {
	ids := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	return ids
}
