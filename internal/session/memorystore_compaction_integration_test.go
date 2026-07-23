package session

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// makeMessageChain builds a parent-linked chain of `count` messages.
// SaveMessages assigns sequential IDs starting at 1, so message N's
// parent is N-1. The first message has a nil ParentID (root).
func makeMessageChain(count int) []Message {
	msgs := make([]Message, 0, count)
	for i := 0; i < count; i++ {
		msg := Message{
			Role:      "user",
			Content:   fmt.Sprintf("message %d", i+1),
			EntryType: "message",
			BranchID:  "main",
			Timestamp: time.Now().UTC(),
		}
		if i > 0 {
			pid := int64(i) // parent is previous message (IDs are 1-based)
			msg.ParentID = &pid
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// containsEntryType reports whether any message in path has the given EntryType.
func containsEntryType(path []Message, entryType string) bool {
	for _, m := range path {
		if m.EntryType == entryType {
			return true
		}
	}
	return false
}

// pathIDSet returns a set of all message IDs present in path.
func pathIDSet(path []Message) map[int64]bool {
	set := make(map[int64]bool, len(path))
	for _, m := range path {
		set[m.ID] = true
	}
	return set
}

// TestMemoryStore_CompactionWorkflow_FullCycle exercises the complete
// compaction workflow: insert a compaction summary, reparent the message
// following the compacted range, and verify GetMessagePath walks through
// the compaction entry while skipping the compacted messages.
func TestMemoryStore_CompactionWorkflow_FullCycle(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("integration-test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 1. Create 5 messages in a chain: 1←2←3←4←5
	if err := store.SaveMessages(session.ID, makeMessageChain(5)); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	// 2. Set the session leaf to message 5.
	if _, err := store.NavigateToBranch(session.ID, 5); err != nil {
		t.Fatalf("NavigateToBranch(5) failed: %v", err)
	}

	// 3. Insert a compaction entry summarizing messages 1-3, parented to
	//    message 1. The compaction entry receives the next available ID (6).
	compactionID, err := store.InsertCompaction(session.ID, 1, "compacted msgs 1-3", []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("InsertCompaction failed: %v", err)
	}
	if compactionID != 6 {
		t.Fatalf("expected compaction ID 6, got %d", compactionID)
	}

	// 4. Reparent the message after the compacted range (message 4, whose
	//    ParentID is 3) so it now points to the compaction entry.
	if err := store.ReparentAfterCompaction(session.ID, 3, compactionID); err != nil {
		t.Fatalf("ReparentAfterCompaction failed: %v", err)
	}

	// 5. Verify the path from the leaf (5) walks through the compaction
	//    entry: expected path 5 → 4 → compaction → 1.
	path, err := store.GetMessagePath(session.ID, 5)
	if err != nil {
		t.Fatalf("GetMessagePath failed: %v", err)
	}

	ids := pathIDSet(path)

	// The compaction entry must be present, and its content must decode.
	if !containsEntryType(path, "compaction") {
		t.Fatal("expected path to contain a compaction entry")
	}
	for _, m := range path {
		if m.EntryType != "compaction" {
			continue
		}
		if m.ID != compactionID {
			t.Errorf("expected compaction message ID %d, got %d", compactionID, m.ID)
		}
		var cc CompactionContent
		if err := json.Unmarshal([]byte(m.Content), &cc); err != nil {
			t.Errorf("failed to unmarshal compaction content: %v", err)
			continue
		}
		if cc.Summary != "compacted msgs 1-3" {
			t.Errorf("expected summary %q, got %q", "compacted msgs 1-3", cc.Summary)
		}
		if len(cc.CompressedIDs) != 3 {
			t.Errorf("expected 3 compressed IDs, got %d", len(cc.CompressedIDs))
		}
	}

	// Messages 5, 4, and 1 must be on the path.
	for _, want := range []int64{5, 4, 1} {
		if !ids[want] {
			t.Errorf("expected path to contain message %d", want)
		}
	}
	// Messages 2 and 3 were compacted away and must NOT be on the path.
	for _, skip := range []int64{2, 3} {
		if ids[skip] {
			t.Errorf("expected path to NOT contain compacted message %d", skip)
		}
	}
}

// TestMemoryStore_CompactionWorkflow_NavigateAfterCompaction verifies that
// after compaction the session leaf can be navigated to a message inside the
// compacted chain and that GetMessagePath from that message still traverses
// the compaction entry.
func TestMemoryStore_CompactionWorkflow_NavigateAfterCompaction(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("integration-test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.SaveMessages(session.ID, makeMessageChain(5)); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	// Leaf starts at message 5.
	if _, err := store.NavigateToBranch(session.ID, 5); err != nil {
		t.Fatalf("NavigateToBranch(5) failed: %v", err)
	}

	// Compact messages 1-3 and reparent message 4 onto the compaction entry.
	compactionID, err := store.InsertCompaction(session.ID, 1, "compacted msgs 1-3", []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("InsertCompaction failed: %v", err)
	}
	if err := store.ReparentAfterCompaction(session.ID, 3, compactionID); err != nil {
		t.Fatalf("ReparentAfterCompaction failed: %v", err)
	}

	// Navigate the leaf back to message 4.
	oldLeaf, err := store.NavigateToBranch(session.ID, 4)
	if err != nil {
		t.Fatalf("NavigateToBranch(4) failed: %v", err)
	}
	if oldLeaf != 5 {
		t.Errorf("expected oldLeaf=5, got %d", oldLeaf)
	}

	// The current leaf should now be 4.
	leafID, err := store.GetLeafMessageID(session.ID)
	if err != nil {
		t.Fatalf("GetLeafMessageID failed: %v", err)
	}
	if leafID != 4 {
		t.Errorf("expected leaf=4, got %d", leafID)
	}

	// Path from 4 must still traverse the compaction entry.
	path, err := store.GetMessagePath(session.ID, 4)
	if err != nil {
		t.Fatalf("GetMessagePath failed: %v", err)
	}
	if !containsEntryType(path, "compaction") {
		t.Error("expected path from 4 to contain a compaction entry")
	}
}

// TestMemoryStore_CompactionWorkflow_MultipleCompactions performs two
// sequential compaction rounds on disjoint ranges and verifies that
// GetCompactionEntries reports both and that the path through the latest
// compaction skips its compacted messages.
func TestMemoryStore_CompactionWorkflow_MultipleCompactions(t *testing.T) {
	store := NewMemoryStore(nil)
	session, err := store.Create("integration-test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Create 6 messages in a chain: 1←2←3←4←5←6
	if err := store.SaveMessages(session.ID, makeMessageChain(6)); err != nil {
		t.Fatalf("SaveMessages failed: %v", err)
	}

	// First compaction: compact messages [1, 2], parented to message 1.
	compaction1ID, err := store.InsertCompaction(session.ID, 1, "first", []int64{1, 2})
	if err != nil {
		t.Fatalf("InsertCompaction (first) failed: %v", err)
	}
	// Message 3 (ParentID==2) now points to compaction1.
	if err := store.ReparentAfterCompaction(session.ID, 2, compaction1ID); err != nil {
		t.Fatalf("ReparentAfterCompaction (first) failed: %v", err)
	}

	// Second compaction: compact messages [4, 5], parented to message 4.
	compaction2ID, err := store.InsertCompaction(session.ID, 4, "second", []int64{4, 5})
	if err != nil {
		t.Fatalf("InsertCompaction (second) failed: %v", err)
	}
	// Message 6 (ParentID==5) now points to compaction2.
	if err := store.ReparentAfterCompaction(session.ID, 5, compaction2ID); err != nil {
		t.Fatalf("ReparentAfterCompaction (second) failed: %v", err)
	}

	// Both compaction entries should be retrievable.
	entries, err := store.GetCompactionEntries(session.ID)
	if err != nil {
		t.Fatalf("GetCompactionEntries failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 compaction entries, got %d", len(entries))
	}

	// Path from 6 must traverse compaction2 and skip messages 4 and 5.
	path, err := store.GetMessagePath(session.ID, 6)
	if err != nil {
		t.Fatalf("GetMessagePath failed: %v", err)
	}
	ids := pathIDSet(path)

	if !ids[compaction2ID] {
		t.Errorf("expected path from 6 to contain compaction2 (id %d)", compaction2ID)
	}
	if !ids[6] {
		t.Error("expected path to contain message 6")
	}
	// Message 4 is compaction2's parent/anchor, so it survives on the path
	// (consistent with FullCycle where the parent anchor is retained). Only
	// message 5 — the strictly-intermediate message between the anchor and
	// the reparented child — is skipped by the compaction.
	if !ids[4] {
		t.Error("expected path to retain anchor message 4 (compaction parent)")
	}
	if ids[5] {
		t.Error("expected path to NOT contain compacted message 5")
	}
}
