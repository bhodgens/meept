package session

// G2 gate (03-session-storage/00T-tests.md): store schema-version accessors.
// Pins contract S1 (.hermes/contracts/03-session-storage.md):
//   - sessions rows carry schema_version INTEGER NOT NULL DEFAULT current
//   - GetSchemaVersion reports the stored version
//   - MigrateSchema is forward-only via the catalog, refuses stored-newer
//     (ErrSessionFormatNewer) and refuses gaps (ErrSessionFormatGap).
//
// RED until leaf 01-session-version lands (symbols undefined today).

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
)

func newG2Store(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	store, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSchemaVersion_DefaultIsCurrent(t *testing.T) {
	store := newG2Store(t)

	sess, err := store.Create("g2-default")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetSchemaVersion(sess.ID)
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if got != SessionFormatVersion {
		t.Errorf("fresh session schema version = %d, want current %d", got, SessionFormatVersion)
	}
}

func TestSchemaVersion_UnknownSessionErrors(t *testing.T) {
	store := newG2Store(t)

	_, err := store.GetSchemaVersion("no-such-session")
	if err == nil {
		t.Fatal("GetSchemaVersion on unknown session must error, got nil")
	}
}

func TestMigrateSchema_NoopAtCurrent(t *testing.T) {
	store := newG2Store(t)

	sess, err := store.Create("g2-noop")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.MigrateSchema(sess.ID); err != nil {
		t.Fatalf("MigrateSchema at current version must be a no-op, got %v", err)
	}
	got, err := store.GetSchemaVersion(sess.ID)
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if got != SessionFormatVersion {
		t.Errorf("version after no-op migrate = %d, want %d", got, SessionFormatVersion)
	}
}

func TestMigrateSchema_RefusesStoredNewer(t *testing.T) {
	store := newG2Store(t)

	sess, err := store.Create("g2-newer")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Simulate a row written by a future build: stamp a version ahead of
	// SessionFormatVersion directly on the sessions row (same-package test,
	// unexported handle).
	if _, err := store.db.Exec(
		"UPDATE sessions SET schema_version = ? WHERE id = ?",
		SessionFormatVersion+1, sess.ID,
	); err != nil {
		t.Fatalf("stamp future version: %v", err)
	}

	err = store.MigrateSchema(sess.ID)
	if !errors.Is(err, ErrSessionFormatNewer) {
		t.Errorf("MigrateSchema on stored-newer = %v, want ErrSessionFormatNewer", err)
	}
}

func TestMigrateSchema_ForwardChain(t *testing.T) {
	store := newG2Store(t)

	sess, err := store.Create("g2-chain")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Stamp version 0 so the catalog must plan 0 -> SessionFormatVersion.
	if _, err := store.db.Exec(
		"UPDATE sessions SET schema_version = 0 WHERE id = ?", sess.ID,
	); err != nil {
		t.Fatalf("stamp v0: %v", err)
	}

	if err := store.MigrateSchema(sess.ID); err != nil {
		t.Fatalf("MigrateSchema from v0: %v", err)
	}
	got, err := store.GetSchemaVersion(sess.ID)
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if got != SessionFormatVersion {
		t.Errorf("version after chain migrate = %d, want %d", got, SessionFormatVersion)
	}
}
