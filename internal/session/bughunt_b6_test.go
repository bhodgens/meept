package session

// Bughunt 2026-11-10 B6 pin: the schema_version column migration must
// stamp LEGACY rows (rows that predate the column) at 0 — the pre-v1
// sentinel the forward-only catalog hops FROM — not at
// SessionFormatVersion, which made the catalog treat every legacy row as
// already-current and would silently skip all future format hops.
//
// Re-run safety is pinned too: a row stamped 1 by the OLD migration
// (DEFAULT = SessionFormatVersion) keeps its value on re-open.

import (
	"path/filepath"
	"testing"
)

// TestSchemaVersionLegacyRowsStampAtZero opens a store against a database
// that already contains a sessions row WITHOUT the schema_version column —
// the legacy-database shape — and asserts migrate() back-fills that row at
// 0, while a row created AFTER the migration carries the current version.
func TestSchemaVersionLegacyRowsStampAtZero(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")

	// Build the LEGACY database by hand: sessions table as it existed
	// before the schema_version column was added, with one legacy row.
	// Raw modernc open — no store migration runs here.
	raw, err := openRawSQLite(dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	const legacySchema = `
CREATE TABLE sessions (
	id              TEXT PRIMARY KEY,
	name            TEXT NOT NULL,
	conversation_id TEXT UNIQUE NOT NULL,
	created_at      TEXT NOT NULL,
	last_activity   TEXT NOT NULL,
	attached_clients TEXT DEFAULT '[]',
	worker_ids      TEXT DEFAULT '[]'
);
INSERT INTO sessions (id, name, conversation_id, created_at, last_activity)
VALUES ('session-legacy', 'legacy', 'conv-legacy', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z');`
	if _, err := raw.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Open through the store: migrate() adds schema_version. The DEFAULT
	// must be the legacy sentinel (0), not SessionFormatVersion.
	store, err := NewSQLiteStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	v, err := store.GetSchemaVersion("session-legacy")
	if err != nil {
		t.Fatalf("GetSchemaVersion(legacy): %v", err)
	}
	if v != 0 {
		t.Errorf("legacy row stamped schema_version = %d, want 0 (pre-v1 sentinel) — "+
			"stamping it at the current version makes every future format hop skip it", v)
	}

	// A FRESH row must sit at the current version.
	s, err := store.Create("fresh")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	v, err = store.GetSchemaVersion(s.ID)
	if err != nil {
		t.Fatalf("GetSchemaVersion(fresh): %v", err)
	}
	if v != SessionFormatVersion {
		t.Errorf("fresh row stamped schema_version = %d, want %d", v, SessionFormatVersion)
	}

	// The catalog half of the pin: the legacy row (0) gets the FIRST
	// MigrateUp hop applied — MigrateSchema plans 0->1 and lands it at
	// the current version. A current-stamped legacy row would have had an
	// empty plan (the B6 bug: future hops would skip it too).
	if err := store.MigrateSchema("session-legacy"); err != nil {
		t.Fatalf("MigrateSchema(legacy): %v", err)
	}
	v, err = store.GetSchemaVersion("session-legacy")
	if err != nil {
		t.Fatalf("GetSchemaVersion(migrated): %v", err)
	}
	if v != SessionFormatVersion {
		t.Errorf("legacy row after MigrateSchema = %d, want %d", v, SessionFormatVersion)
	}
}

// TestSchemaVersionRerunKeepsStamps pins migration re-run safety: rows
// already stamped 1 (by the old DEFAULT-SessionFormatVersion migration, or
// by a completed hop) must NOT be reset by re-opening the store.
func TestSchemaVersionRerunKeepsStamps(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")

	store, err := NewSQLiteStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	s, err := store.Create("rerun")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open: migrationAddColumn no-ops; stamps stay.
	store2, err := NewSQLiteStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store2.Close()
	v, err := store2.GetSchemaVersion(s.ID)
	if err != nil {
		t.Fatalf("GetSchemaVersion after reopen: %v", err)
	}
	if v != SessionFormatVersion {
		t.Errorf("schema_version after re-open = %d, want %d (re-run must not re-stamp)", v, SessionFormatVersion)
	}
}
