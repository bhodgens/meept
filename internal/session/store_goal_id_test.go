package session

import (
	"testing"
)

// TestSQLiteStore_GoalIDRoundTrip pins goal-completion leaf 03 Task 1: a
// session's explicitly bound goal_id survives the store round-trip (set ->
// Get from the SQLite store after a fresh read, not just the shared pointer).
func TestSQLiteStore_GoalIDRoundTrip(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	sess, err := store.Create("goal-roundtrip")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.GoalID != "" {
		t.Fatalf("fresh session GoalID = %q, want empty", sess.GoalID)
	}

	if err := store.SetGoalID(sess.ID, "goal_abc"); err != nil {
		t.Fatalf("SetGoalID: %v", err)
	}

	// Re-read through getByColumn (a fresh SELECT, not the shared pointer).
	got := store.GetByConversationID(sess.ConversationID)
	if got == nil {
		t.Fatal("session not found after SetGoalID")
	}
	if got.GoalID != "goal_abc" {
		t.Errorf("GoalID = %q, want %q", got.GoalID, "goal_abc")
	}

	// Overwrite then clear.
	if err := store.SetGoalID(sess.ID, "goal_def"); err != nil {
		t.Fatalf("SetGoalID overwrite: %v", err)
	}
	got = store.GetByConversationID(sess.ConversationID)
	if got == nil || got.GoalID != "goal_def" {
		t.Fatalf("GoalID after overwrite = %q, want %q", got.GoalID, "goal_def")
	}
	if err := store.SetGoalID(sess.ID, ""); err != nil {
		t.Fatalf("SetGoalID clear: %v", err)
	}
	got = store.GetByConversationID(sess.ConversationID)
	if got == nil || got.GoalID != "" {
		t.Fatalf("GoalID after clear = %q, want empty", got.GoalID)
	}

	// Unknown session surfaces an error (mirrors SetDetectionContext).
	if err := store.SetGoalID("session-does-not-exist", "goal_x"); err == nil {
		t.Error("SetGoalID on missing session: expected error, got nil")
	}
}

// TestMemoryStore_GoalIDRoundTrip pins the memory-store parity path.
func TestMemoryStore_GoalIDRoundTrip(t *testing.T) {
	store := NewMemoryStore(nil)

	sess, err := store.Create("goal-roundtrip-mem")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.GoalID != "" {
		t.Fatalf("fresh session GoalID = %q, want empty", sess.GoalID)
	}
	if err := store.SetGoalID(sess.ID, "goal_mem"); err != nil {
		t.Fatalf("SetGoalID: %v", err)
	}
	got := store.Get(sess.ID)
	if got == nil || got.GoalID != "goal_mem" {
		t.Fatalf("GoalID = %q, want %q", got.GoalID, "goal_mem")
	}
	if err := store.SetGoalID("session-missing", "goal_mem"); err == nil {
		t.Error("SetGoalID on missing session: expected error, got nil")
	}
}
