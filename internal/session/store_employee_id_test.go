package session

import (
	"testing"
)

// TestSQLiteStore_EmployeeIDRoundTrip pins the session→employee binding
// (goal-completion follow-up #5): a session's explicitly bound employee_id
// survives the store round-trip (set -> fresh SELECT from the SQLite store,
// not just the shared pointer). Mirrors the goal_id column precedent
// (TestSQLiteStore_GoalIDRoundTrip): empty = unbound, which keeps
// single-employee installs on the sole-employee fallback.
func TestSQLiteStore_EmployeeIDRoundTrip(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	sess, err := store.Create("employee-roundtrip")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.EmployeeID != "" {
		t.Fatalf("fresh session EmployeeID = %q, want empty", sess.EmployeeID)
	}

	if err := store.SetEmployeeID(sess.ID, "emp_abc"); err != nil {
		t.Fatalf("SetEmployeeID: %v", err)
	}

	// Re-read through getByColumn (a fresh SELECT, not the shared pointer).
	got := store.GetByConversationID(sess.ConversationID)
	if got == nil {
		t.Fatal("session not found after SetEmployeeID")
	}
	if got.EmployeeID != "emp_abc" {
		t.Errorf("EmployeeID = %q, want %q", got.EmployeeID, "emp_abc")
	}

	// Overwrite then clear.
	if err := store.SetEmployeeID(sess.ID, "emp_def"); err != nil {
		t.Fatalf("SetEmployeeID overwrite: %v", err)
	}
	got = store.GetByConversationID(sess.ConversationID)
	if got == nil || got.EmployeeID != "emp_def" {
		t.Fatalf("EmployeeID after overwrite = %q, want %q", got.EmployeeID, "emp_def")
	}
	if err := store.SetEmployeeID(sess.ID, ""); err != nil {
		t.Fatalf("SetEmployeeID clear: %v", err)
	}
	got = store.GetByConversationID(sess.ConversationID)
	if got == nil || got.EmployeeID != "" {
		t.Fatalf("EmployeeID after clear = %q, want empty", got.EmployeeID)
	}

	// Unknown session surfaces an error (mirrors SetGoalID/SetDetectionContext).
	if err := store.SetEmployeeID("session-does-not-exist", "emp_x"); err == nil {
		t.Error("SetEmployeeID on missing session: expected error, got nil")
	}
}

// TestMemoryStore_EmployeeIDRoundTrip pins the memory-store parity path.
func TestMemoryStore_EmployeeIDRoundTrip(t *testing.T) {
	store := NewMemoryStore(nil)

	sess, err := store.Create("employee-roundtrip-mem")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.EmployeeID != "" {
		t.Fatalf("fresh session EmployeeID = %q, want empty", sess.EmployeeID)
	}
	if err := store.SetEmployeeID(sess.ID, "emp_mem"); err != nil {
		t.Fatalf("SetEmployeeID: %v", err)
	}
	got := store.Get(sess.ID)
	if got == nil || got.EmployeeID != "emp_mem" {
		t.Fatalf("EmployeeID = %q, want %q", got.EmployeeID, "emp_mem")
	}
	if err := store.SetEmployeeID("session-missing", "emp_mem"); err == nil {
		t.Error("SetEmployeeID on missing session: expected error, got nil")
	}
}
