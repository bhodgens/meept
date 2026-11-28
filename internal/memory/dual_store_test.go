package memory

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/migrate"
)

func TestNewDualStore(t *testing.T) {
	dir := t.TempDir()
	logger := slog.Default()

	ds, err := NewDualStore(dir, "test-node-1", logger)
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if ds.localNodeID != "test-node-1" {
		t.Errorf("localNodeID = %q, want %q", ds.localNodeID, "test-node-1")
	}
	if ds.localDB == nil {
		t.Fatal("localDB is nil")
	}
	if ds.gossipDB == nil {
		t.Fatal("gossipDB is nil")
	}
}

func TestNewDualStoreRejectsEmptyNodeID(t *testing.T) {
	dir := t.TempDir()
	_, err := NewDualStore(dir, "", slog.Default())
	if err == nil {
		t.Fatal("expected error for empty nodeID")
	}
}

func TestDualStoreCreatesDBFiles(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	localPath := filepath.Join(dir, localDBName)
	gossipPath := filepath.Join(dir, gossipDBName)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Fatal("local.db was not created")
	}
	if _, err := os.Stat(gossipPath); os.IsNotExist(err) {
		t.Fatal("sync-gossip.db was not created")
	}
}

// TestDualStoreLegacyTimestampPadding pins the #196 migration on the dual
// store's memories/sessions tables: rows written in the legacy
// variable-width RFC3339Nano shape are padded to the fixed IsoTime shape at
// store open (representation-only, value-preserving, idempotent), and
// ORDER BY created_at agrees with time semantics afterwards.
func TestDualStoreLegacyTimestampPadding(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Seed local.db directly with legacy shapes BEFORE NewDualStore opens
	// (schema DDL is CREATE IF NOT EXISTS, so the rows survive).
	seedPath := filepath.Join(dir, localDBName)
	seed, err := sql.Open("sqlite", seedPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE memories (
		id TEXT PRIMARY KEY, type TEXT NOT NULL DEFAULT 'episodic',
		category TEXT NOT NULL DEFAULT 'conversation', content TEXT NOT NULL,
		metadata_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL,
		updated_at TEXT, agent_id TEXT, session_id TEXT, bot_id TEXT)`); err != nil {
		t.Fatalf("seed memories: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '',
		conversation_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
		last_activity TEXT NOT NULL, attached_clients TEXT NOT NULL DEFAULT '[]',
		worker_ids TEXT NOT NULL DEFAULT '[]', description TEXT NOT NULL DEFAULT '',
		leaf_message_id INTEGER, project_id TEXT NOT NULL DEFAULT '',
		project_path TEXT NOT NULL DEFAULT '', no_fence INTEGER NOT NULL DEFAULT 0,
		metadata_json TEXT NOT NULL DEFAULT '{}')`); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	// The RCA collision pair plus a NULL updated_at (sentinel must stay NULL).
	if _, err := seed.Exec(`INSERT INTO memories (id, type, category, content, metadata_json, created_at, updated_at)
		VALUES ('m-older', 'episodic', 'conversation', 'older', '{}', '2026-11-28T01:08:12.1833Z', NULL),
		       ('m-newer', 'episodic', 'conversation', 'newer', '{}', '2026-11-28T01:08:12.183388Z', '2026-11-28T01:08:13.5Z')`); err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO sessions (id, created_at, last_activity)
		VALUES ('s-1', '2026-11-28T01:08:12Z', '2026-11-28T02:00:00.1Z')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed: %v", err)
	}

	ds, err := NewDualStore(dir, "pin-node", testLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	checks := []struct {
		q    string
		want string
	}{
		{`SELECT created_at FROM memories WHERE id='m-older'`, "2026-11-28T01:08:12.183300000Z"},
		{`SELECT created_at FROM memories WHERE id='m-newer'`, "2026-11-28T01:08:12.183388000Z"},
		{`SELECT updated_at FROM memories WHERE id='m-newer'`, "2026-11-28T01:08:13.500000000Z"},
		{`SELECT created_at FROM sessions WHERE id='s-1'`, "2026-11-28T01:08:12.000000000Z"},
		{`SELECT last_activity FROM sessions WHERE id='s-1'`, "2026-11-28T02:00:00.100000000Z"},
	}
	for _, c := range checks {
		var got sql.NullString
		if err := ds.localDB.QueryRowContext(ctx, c.q).Scan(&got); err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if got.String != c.want {
			t.Errorf("%s = %q, want %q", c.q, got.String, c.want)
		}
	}
	// NULL sentinel untouched.
	var up sql.NullString
	if err := ds.localDB.QueryRowContext(ctx, `SELECT updated_at FROM memories WHERE id='m-older'`).Scan(&up); err != nil {
		t.Fatalf("updated_at probe: %v", err)
	}
	if up.Valid {
		t.Errorf("NULL updated_at was rewritten to %q; must stay NULL", up.String)
	}
	// Lexical order now matches time order (DESC puts the newer first).
	var first string
	if err := ds.localDB.QueryRowContext(ctx,
		`SELECT id FROM memories ORDER BY created_at DESC LIMIT 1`).Scan(&first); err != nil {
		t.Fatalf("order probe: %v", err)
	}
	if first != "m-newer" {
		t.Errorf("ORDER BY created_at DESC first = %q, want m-newer", first)
	}
	// Fresh writes carry the fixed width end-to-end through the writer.
	newer := time.Date(2026, 11, 28, 1, 8, 12, 183388000, time.UTC)
	if err := ds.StoreMemory(ctx, &Memory{ID: "m-fresh", Type: MemoryTypeEpisodic, Category: "conversation", Content: "fresh", CreatedAt: newer}); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}
	var fresh string
	if err := ds.localDB.QueryRowContext(ctx, `SELECT created_at FROM memories WHERE id='m-fresh'`).Scan(&fresh); err != nil {
		t.Fatalf("fresh probe: %v", err)
	}
	if len(fresh) != isoTimeLen {
		t.Errorf("fresh write %q width %d, want %d", fresh, len(fresh), isoTimeLen)
	}
	// Idempotence: a second open rewires nothing.
	n, err := migrate.BackfillFixedWidthTimestamps(ctx, ds.localDB, "memories", "created_at", "updated_at")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if n != 0 {
		t.Errorf("second padding pass rewrote %d values, want 0", n)
	}
}

func TestDualStoreSyncMetadata(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-1", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	// Check that sync_metadata tables exist and have version markers.
	var val string
	err = ds.localDB.QueryRow("SELECT value FROM sync_metadata WHERE key='schema_version'").Scan(&val)
	if err != nil {
		t.Fatalf("local sync_metadata schema_version not found: %v", err)
	}
	if val != syncMetaVersion {
		t.Errorf("local schema_version = %q, want %q", val, syncMetaVersion)
	}

	err = ds.gossipDB.QueryRow("SELECT value FROM sync_metadata WHERE key='schema_version'").Scan(&val)
	if err != nil {
		t.Fatalf("gossip sync_metadata schema_version not found: %v", err)
	}
}

func TestDualStoreClose(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}

	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Second close should be idempotent (no panic).
	if err := ds.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestIsLocal(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-alpha", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if !ds.IsLocal("node-alpha") {
		t.Error("IsLocal(node-alpha) should be true")
	}
	if ds.IsLocal("node-beta") {
		t.Error("IsLocal(node-beta) should be false")
	}
}

func TestDualStoreEmptyReadReturnsNil(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty store, got %d results", len(results))
	}
}
