package session

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestDB creates a fresh in-memory SQLite database for testing.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create the designation_history table.
	if err := migrateDesignationHistory(db); err != nil {
		t.Fatalf("failed to migrate designation_history: %v", err)
	}
	return db
}

func TestDesignationHistory_Record(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())
	ctx := context.Background()

	err := store.Record(ctx, "sess-1", DesignationWaitingHuman, DesignationRequiresApproval, "user escalation")
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	entries, err := store.List(ctx, "sess-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.SessionID != "sess-1" {
		t.Errorf("expected session_id 'sess-1', got %q", e.SessionID)
	}
	if e.FromStatus != DesignationWaitingHuman {
		t.Errorf("expected from_status %q, got %q", DesignationWaitingHuman, e.FromStatus)
	}
	if e.ToStatus != DesignationRequiresApproval {
		t.Errorf("expected to_status %q, got %q", DesignationRequiresApproval, e.ToStatus)
	}
	if e.Reason != "user escalation" {
		t.Errorf("expected reason 'user escalation', got %q", e.Reason)
	}
	if e.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestDesignationHistory_ListMultiple(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())
	ctx := context.Background()

	// Record several transitions.
	_ = store.Record(ctx, "sess-1", "", DesignationWaitingHuman, "initial")
	time.Sleep(1 * time.Millisecond)
	_ = store.Record(ctx, "sess-1", DesignationWaitingHuman, DesignationRequiresApproval, "escalation")
	time.Sleep(1 * time.Millisecond)
	_ = store.Record(ctx, "sess-1", DesignationRequiresApproval, DesignationBotThinking, "approved")

	entries, err := store.List(ctx, "sess-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Verify ordering (oldest-first by ID).
	if entries[0].ToStatus != DesignationWaitingHuman {
		t.Errorf("first entry should be initial designation, got to_status %q", entries[0].ToStatus)
	}
	if entries[1].FromStatus != DesignationWaitingHuman {
		t.Errorf("second entry from_status mismatch: %q", entries[1].FromStatus)
	}
	if entries[1].ToStatus != DesignationRequiresApproval {
		t.Errorf("second entry to_status mismatch: %q", entries[1].ToStatus)
	}
	if entries[2].ToStatus != DesignationBotThinking {
		t.Errorf("third entry to_status mismatch: %q", entries[2].ToStatus)
	}
}

func TestDesignationHistory_EmptyFromStatus(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())
	ctx := context.Background()

	// First designation should have empty from_status when DesignationNone is passed.
	err := store.Record(ctx, "sess-1", DesignationNone, DesignationWaitingHuman, "first designation")
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	entries, err := store.List(ctx, "sess-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// FromStatus should be "none" (the string form of DesignationNone).
	if entries[0].FromStatus != DesignationNone {
		t.Errorf("expected from_status %q, got %q", DesignationNone, entries[0].FromStatus)
	}
}

func TestDesignationHistory_ListEmpty(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())
	ctx := context.Background()

	entries, err := store.List(ctx, "no-such-session")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for non-existent session, got %d", len(entries))
	}
}

func TestDesignationHistory_IsolatedSessions(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())
	ctx := context.Background()

	_ = store.Record(ctx, "sess-a", DesignationNone, DesignationWaitingHuman, "a")
	_ = store.Record(ctx, "sess-b", DesignationNone, DesignationRequiresApproval, "b")
	_ = store.Record(ctx, "sess-a", DesignationWaitingHuman, DesignationBotThinking, "a2")

	aEntries, _ := store.List(ctx, "sess-a")
	bEntries, _ := store.List(ctx, "sess-b")

	if len(aEntries) != 2 {
		t.Errorf("expected 2 entries for sess-a, got %d", len(aEntries))
	}
	if len(bEntries) != 1 {
		t.Errorf("expected 1 entry for sess-b, got %d", len(bEntries))
	}
}

func TestSession_SetDesignation_NilHistoryStore(t *testing.T) {
	// SetDesignation should be safe when no history store is attached.
	s := &Session{ID: "test-session"}

	// This should not panic.
	s.SetDesignation(DesignationWaitingHuman, "test reason", "normal")

	if s.Designation == nil || s.Designation.Status != DesignationWaitingHuman {
		t.Fatal("designation was not set correctly")
	}

	// Change designation — should still not panic without a store.
	s.SetDesignation(DesignationRequiresApproval, "escalated", "high")

	if s.Designation.Status != DesignationRequiresApproval {
		t.Fatal("designation was not updated correctly")
	}
}

func TestSession_SetDesignation_WithHistoryStore(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())

	s := &Session{ID: "sess-history-test"}
	s.SetDesignationHistoryStore(store)

	// First designation — records transition from "none" to "waiting_human".
	s.SetDesignation(DesignationWaitingHuman, "initial", "normal")

	// Second designation — records transition from "waiting_human" to "requires_approval".
	s.SetDesignation(DesignationRequiresApproval, "escalated", "high")

	entries, err := store.List(context.Background(), "sess-history-test")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(entries))
	}

	if entries[0].FromStatus != DesignationNone {
		t.Errorf("first entry from_status: expected %q, got %q", DesignationNone, entries[0].FromStatus)
	}
	if entries[0].ToStatus != DesignationWaitingHuman {
		t.Errorf("first entry to_status: expected %q, got %q", DesignationWaitingHuman, entries[0].ToStatus)
	}
	if entries[1].FromStatus != DesignationWaitingHuman {
		t.Errorf("second entry from_status: expected %q, got %q", DesignationWaitingHuman, entries[1].FromStatus)
	}
	if entries[1].ToStatus != DesignationRequiresApproval {
		t.Errorf("second entry to_status: expected %q, got %q", DesignationRequiresApproval, entries[1].ToStatus)
	}
}

func TestSession_SetDesignation_SameStatusNoHistory(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteDesignationHistoryStore(db, slog.Default())

	s := &Session{ID: "sess-same-status"}
	s.SetDesignationHistoryStore(store)

	// Set the same designation twice — only one history entry should be recorded.
	s.SetDesignation(DesignationWaitingHuman, "first", "normal")
	s.SetDesignation(DesignationWaitingHuman, "second", "normal")

	entries, _ := store.List(context.Background(), "sess-same-status")
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for same-status re-set, got %d", len(entries))
	}
}

// TestSQLiteStore_UpdateDesignation_RecordsHistory tests that the SQLiteStore's
// UpdateDesignation method records history transitions when a history store is attached.
func TestSQLiteStore_UpdateDesignation_RecordsHistory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_sessions.db")

	store, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("failed to create SQLiteStore: %v", err)
	}
	defer store.Close()

	// Create a session.
	sess, err := store.Create("test-session")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// First designation update.
	err = store.UpdateDesignation(sess.ID, DesignationWaitingHuman, "waiting", "normal")
	if err != nil {
		t.Fatalf("UpdateDesignation failed: %v", err)
	}

	// Second designation update.
	err = store.UpdateDesignation(sess.ID, DesignationRequiresApproval, "escalated", "high")
	if err != nil {
		t.Fatalf("UpdateDesignation failed: %v", err)
	}

	// Verify history was recorded via the store's embedded history store.
	historyStore := store.GetDesignationHistory()
	if historyStore == nil {
		t.Fatal("expected designation history store to be initialized")
	}

	entries, err := historyStore.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("history List failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(entries))
	}
}
