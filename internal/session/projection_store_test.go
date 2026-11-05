package session

// Leaf 03-session-storage/02 tasks T2 + T4 integration pins (the committed
// G3 gate covers the registry in isolation; these cover the store wiring):
//   - T2: SaveMessages drives each committed message into the wired registry
//     post-commit; the fold reflects every message written.
//   - T4: a session whose stored schema_version is ahead of this build is
//     refused (ErrSessionFormatNewer) on Snapshot and skipped on fold.

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func newT2Store(t *testing.T) (*SQLiteStore, *SessionProjectionRegistry) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	store, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cache, err := NewSessionProjectionCache(store.db)
	if err != nil {
		t.Fatalf("NewSessionProjectionCache: %v", err)
	}
	reg := NewSessionProjectionRegistry(cache)
	if _, err := reg.Register(SessionSummaryDefinition()); err != nil {
		t.Fatalf("Register(session-summary): %v", err)
	}
	store.SetProjectionRegistry(reg)
	return store, reg
}

func TestProjection_T2_StoreDrivesRegistryPostCommit(t *testing.T) {
	store, reg := newT2Store(t)

	sess, err := store.Create("t2-drive")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessages(sess.ID, []Message{
		{Role: "user", Content: "hello there", Timestamp: now},
		{Role: "assistant", Content: "hi", Timestamp: now.Add(time.Second)},
	}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	snap, err := reg.Snapshot(sess.ID, SessionSummaryKey)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got, ok := snap.Values[SessionSummaryKey].(*SessionSummaryState)
	if !ok {
		t.Fatalf("summary value type = %T, want *SessionSummaryState", snap.Values[SessionSummaryKey])
	}
	if got.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2 (both committed messages folded)", got.MessageCount)
	}
	if !got.HasResponse {
		t.Error("HasResponse = false, want true (assistant message folded)")
	}
	if got.LastRole != "assistant" || got.LastText != "hi" {
		t.Errorf("last fold = %q/%q, want assistant/hi", got.LastRole, got.LastText)
	}

	// The fold is checkpointed: a ver-matching cache row exists.
	cache2, err := NewSessionProjectionCache(store.db)
	if err != nil {
		t.Fatalf("reopen cache: %v", err)
	}
	cached, err := cache2.LoadSession(sess.ID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	row, ok := cached[SessionSummaryKey]
	if !ok {
		t.Fatal("no checkpoint row persisted for session-summary")
	}
	if row.Seq != 2 {
		t.Errorf("checkpoint seq = %d, want 2", row.Seq)
	}
	if row.Ver != sessionSummaryStateVersion {
		t.Errorf("checkpoint ver = %d, want %d", row.Ver, sessionSummaryStateVersion)
	}
}

func TestProjection_T4_VersionGuardRefusesNewerSession(t *testing.T) {
	store, reg := newT2Store(t)

	sess, err := store.Create("t2-guard")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Stamp the session row ahead of this build (simulates a future writer).
	if _, err := store.db.Exec(
		"UPDATE sessions SET schema_version = ? WHERE id = ?",
		SessionFormatVersion+1, sess.ID,
	); err != nil {
		t.Fatalf("stamp future version: %v", err)
	}

	if _, err := reg.Snapshot(sess.ID, SessionSummaryKey); !errors.Is(err, ErrSessionFormatNewer) {
		t.Errorf("Snapshot on newer-version session = %v, want ErrSessionFormatNewer", err)
	}

	// Fold is skipped too: the OnAppend drive refuses the newer session,
	// so no live state appears for it.
	reg.OnAppend(sess.ID, Message{
		Role: "user", Content: "should not fold", Timestamp: time.Now().UTC(),
	})
	reg.mu.RLock()
	_, hasLive := reg.states[sess.ID][SessionSummaryKey]
	reg.mu.RUnlock()
	if hasLive {
		t.Error("OnAppend folded a message for a newer-version session; want refusal")
	}
}
