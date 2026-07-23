package session

import (
	"encoding/json"
	"testing"
	"time"
)

// helper to seed a session with N plain messages and return the store + session ID.
func seedStoreWithMessages(t *testing.T, msgCount int) (*MemoryStore, string) {
	t.Helper()
	store := NewMemoryStore(nil)
	session, err := store.Create("test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	msgs := make([]Message, 0, msgCount)
	for i := 0; i < msgCount; i++ {
		msgs = append(msgs, Message{
			Role:      "user",
			Content:   "msg",
			EntryType: "message",
			BranchID:  BranchMain,
			Timestamp: time.Now().UTC(),
		})
	}
	if err := store.SaveMessages(session.ID, msgs); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}
	return store, session.ID
}

func TestMemoryStore_InsertCompaction_Basic(t *testing.T) {
	store, sessionID := seedStoreWithMessages(t, 3)

	id, err := store.InsertCompaction(sessionID, 1, "test summary", []int64{1, 2})
	if err != nil {
		t.Fatalf("InsertCompaction failed: %v", err)
	}

	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}
	if id != 4 {
		t.Fatalf("expected ID 4 (next sequential), got %d", id)
	}

	msgs, err := store.GetMessages(sessionID, 0, 1000)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}

	var compaction *Message
	for i := range msgs {
		if msgs[i].ID == id {
			compaction = &msgs[i]
			break
		}
	}
	if compaction == nil {
		t.Fatalf("compaction message with ID %d not found", id)
	}

	if compaction.EntryType != "compaction" {
		t.Errorf("EntryType = %q, want %q", compaction.EntryType, "compaction")
	}
	if compaction.Role != "system" {
		t.Errorf("Role = %q, want %q", compaction.Role, "system")
	}
	if compaction.BranchID != BranchMain {
		t.Errorf("BranchID = %q, want %q", compaction.BranchID, BranchMain)
	}
	if compaction.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", compaction.SessionID, sessionID)
	}
	if compaction.ParentID == nil {
		t.Fatal("ParentID is nil, want 1")
	} else if *compaction.ParentID != 1 {
		t.Errorf("ParentID = %d, want 1", *compaction.ParentID)
	}

	var content CompactionContent
	if err := json.Unmarshal([]byte(compaction.Content), &content); err != nil {
		t.Fatalf("failed to unmarshal content: %v", err)
	}
	if content.Summary != "test summary" {
		t.Errorf("Summary = %q, want %q", content.Summary, "test summary")
	}
	if len(content.CompressedIDs) != 2 {
		t.Fatalf("CompressedIDs len = %d, want 2", len(content.CompressedIDs))
	}
	if content.CompressedIDs[0] != 1 || content.CompressedIDs[1] != 2 {
		t.Errorf("CompressedIDs = %v, want [1 2]", content.CompressedIDs)
	}

	if compaction.Timestamp.IsZero() {
		t.Error("Timestamp is zero, want non-zero")
	}
}

func TestMemoryStore_InsertCompaction_EmptyCompressedIDs(t *testing.T) {
	store, sessionID := seedStoreWithMessages(t, 2)

	id, err := store.InsertCompaction(sessionID, 1, "empty ids summary", []int64{})
	if err != nil {
		t.Fatalf("InsertCompaction failed: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	msgs, err := store.GetMessages(sessionID, 0, 1000)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}

	var compaction *Message
	for i := range msgs {
		if msgs[i].ID == id {
			compaction = &msgs[i]
			break
		}
	}
	if compaction == nil {
		t.Fatalf("compaction message with ID %d not found", id)
	}

	var content CompactionContent
	if err := json.Unmarshal([]byte(compaction.Content), &content); err != nil {
		t.Fatalf("failed to unmarshal content: %v", err)
	}
	if content.Summary != "empty ids summary" {
		t.Errorf("Summary = %q, want %q", content.Summary, "empty ids summary")
	}
	if content.CompressedIDs == nil {
		t.Error("CompressedIDs is nil, want non-nil (marshaled empty slice)")
	}
	if len(content.CompressedIDs) != 0 {
		t.Errorf("CompressedIDs len = %d, want 0", len(content.CompressedIDs))
	}
}

func TestMemoryStore_InsertCompaction_MultipleCompactions(t *testing.T) {
	store, sessionID := seedStoreWithMessages(t, 3)

	// First compaction with parent pointing at message 1.
	id1, err := store.InsertCompaction(sessionID, 1, "first compaction", []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("first InsertCompaction failed: %v", err)
	}
	if id1 != 4 {
		t.Fatalf("expected first compaction ID 4, got %d", id1)
	}

	// Second compaction chained off the first compaction's ID.
	id2, err := store.InsertCompaction(sessionID, id1, "second compaction", []int64{id1})
	if err != nil {
		t.Fatalf("second InsertCompaction failed: %v", err)
	}
	if id2 != 5 {
		t.Fatalf("expected second compaction ID 5, got %d", id2)
	}

	entries, err := store.GetCompactionEntries(sessionID)
	if err != nil {
		t.Fatalf("GetCompactionEntries failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 compaction entries, got %d", len(entries))
	}

	// Entries should be retrievable; IDs ordered as inserted.
	if entries[0].ID != id1 {
		t.Errorf("entries[0].ID = %d, want %d", entries[0].ID, id1)
	}
	if entries[1].ID != id2 {
		t.Errorf("entries[1].ID = %d, want %d", entries[1].ID, id2)
	}

	if entries[0].ParentID == nil {
		t.Error("entries[0].ParentID is nil, want 1")
	} else if *entries[0].ParentID != 1 {
		t.Errorf("entries[0].ParentID = %d, want 1", *entries[0].ParentID)
	}
	if entries[1].ParentID == nil {
		t.Error("entries[1].ParentID is nil, want first compaction ID")
	} else if *entries[1].ParentID != id1 {
		t.Errorf("entries[1].ParentID = %d, want %d", *entries[1].ParentID, id1)
	}
}

func TestMemoryStore_InsertCompaction_SessionNotFound(t *testing.T) {
	store := NewMemoryStore(nil)

	id, err := store.InsertCompaction("nonexistent-session", 1, "summary", []int64{1})
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
	if id != 0 {
		t.Errorf("expected ID 0 on error, got %d", id)
	}
}

func TestMemoryStore_InsertCompaction_GetCompactionEntries_RoundTrip(t *testing.T) {
	store, sessionID := seedStoreWithMessages(t, 2)
	parentID := int64(1)

	id, err := store.InsertCompaction(sessionID, parentID, "round trip summary", []int64{1, 2})
	if err != nil {
		t.Fatalf("InsertCompaction failed: %v", err)
	}

	entries, err := store.GetCompactionEntries(sessionID)
	if err != nil {
		t.Fatalf("GetCompactionEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 compaction entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.ID != id {
		t.Errorf("ID = %d, want %d", entry.ID, id)
	}
	if entry.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, sessionID)
	}
	if entry.ParentID == nil {
		t.Error("ParentID is nil, want 1")
	} else if *entry.ParentID != parentID {
		t.Errorf("ParentID = %d, want %d", *entry.ParentID, parentID)
	}
	if len(entry.CompressedIDs) != 2 {
		t.Fatalf("CompressedIDs len = %d, want 2", len(entry.CompressedIDs))
	}
	if entry.CompressedIDs[0] != 1 || entry.CompressedIDs[1] != 2 {
		t.Errorf("CompressedIDs = %v, want [1 2]", entry.CompressedIDs)
	}
	if entry.Timestamp.IsZero() {
		t.Error("Timestamp is zero, want non-zero")
	}

	// Verify the content field is well-formed JSON matching the summary.
	var content CompactionContent
	if err := json.Unmarshal([]byte(entry.Content), &content); err != nil {
		t.Fatalf("failed to unmarshal entry content: %v", err)
	}
	if content.Summary != "round trip summary" {
		t.Errorf("Summary = %q, want %q", content.Summary, "round trip summary")
	}
}
