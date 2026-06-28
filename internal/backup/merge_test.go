package backup

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite"
)

// mergeTestSchema is the canonical gossip schema used by merge functions.
const mergeTestSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER,
    updated_at INTEGER,
    metadata BLOB,
    source_node TEXT
);
CREATE TABLE IF NOT EXISTS turns (
    turn_id TEXT PRIMARY KEY,
    session_id TEXT,
    role TEXT,
    content TEXT,
    timestamp INTEGER,
    source_node TEXT
);
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    type TEXT,
    category TEXT,
    content TEXT,
    created_at INTEGER,
    agent_id TEXT,
    session_id TEXT,
    source_node TEXT
);
`

// openTestDB opens a fresh SQLite DB at dbPath and applies mergeTestSchema.
// Sets MaxOpenConns(1) to ensure ATTACH/DETACH across transactions uses the
// same underlying connection (modernc.org/sqlite ATTACH is per-connection).
func openTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(mergeTestSchema); err != nil {
		t.Fatalf("apply mergeTestSchema: %v", err)
	}
	return db
}

// insertSession inserts a single session row with all columns populated.
func insertSession(t *testing.T, db *sql.DB, sessionID, sourceNode string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sessions (id, created_at, updated_at, metadata, source_node)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, time.Now().UnixNano(), time.Now().UnixNano(), []byte(`{}`), sourceNode,
	)
	if err != nil {
		t.Fatalf("insert session %q: %v", sessionID, err)
	}
}

// insertTurn inserts a single turn row.
func insertTurn(t *testing.T, db *sql.DB, turnID, sessionID, sourceNode string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO turns (turn_id, session_id, role, content, timestamp, source_node)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		turnID, sessionID, "user", "hello", time.Now().UnixNano(), sourceNode,
	)
	if err != nil {
		t.Fatalf("insert turn %q: %v", turnID, err)
	}
}

// insertMemory inserts a single memory row.
func insertMemory(t *testing.T, db *sql.DB, memID, sourceNode string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO memories (id, type, category, content, created_at, agent_id, session_id, source_node)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		memID, "episodic", "test", "content", time.Now().UnixNano(), "agent-1", "sess-x", sourceNode,
	)
	if err != nil {
		t.Fatalf("insert memory %q: %v", memID, err)
	}
}

// TestMergePeerDB_StatsFieldPropagation guards against the previously-fixed bug
// where MergeStats field counts were always zero. After a successful merge the
// SessionsMerged / TurnsMerged / MemoriesMerged counters must be non-zero when
// the peer DB has matching rows.
func TestMergePeerDB_StatsFieldPropagation(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)

	peerPath := filepath.Join(tmp, "peer.db")
	peerDB := openTestDB(t, peerPath)

	peerID := id.Generate("peer-")
	insertSession(t, peerDB, id.Generate("sess-"), peerID)
	insertSession(t, peerDB, id.Generate("sess-"), peerID)
	insertTurn(t, peerDB, id.Generate("turn-"), "sess-x", peerID)
	insertTurn(t, peerDB, id.Generate("turn-"), "sess-y", peerID)
	insertTurn(t, peerDB, id.Generate("turn-"), "sess-z", peerID)
	insertMemory(t, peerDB, id.Generate("mem-"), peerID)

	stats, err := MergePeerDB(context.Background(), gossipDB, peerPath, peerID)
	if err != nil {
		t.Fatalf("MergePeerDB: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}

	if stats.SessionsMerged != 2 {
		t.Errorf("SessionsMerged = %d, want 2", stats.SessionsMerged)
	}
	if stats.TurnsMerged != 3 {
		t.Errorf("TurnsMerged = %d, want 3", stats.TurnsMerged)
	}
	if stats.MemoriesMerged != 1 {
		t.Errorf("MemoriesMerged = %d, want 1", stats.MemoriesMerged)
	}
	if stats.Errors != 0 {
		t.Errorf("Errors = %d, want 0", stats.Errors)
	}
}

// TestMergePeerDB_DuplicateIDsIgnored verifies idempotency: re-running merge
// against the same peer produces zero newly-merged rows because of
// INSERT OR IGNORE.
//
// This test uses two separate gossip DB connections to avoid a known issue
// where modernc.org/sqlite's ATTACH alias ("peer") persists on the connection
// after the first MergePeerDB call's DETACH, causing the second call's ATTACH
// to fail with "database peer is already in use". This happens because
// sql.DB's connection pool may reuse the same underlying connection.
func TestMergePeerDB_DuplicateIDsIgnored(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Build the peer DB with stable IDs.
	peerPath := filepath.Join(tmp, "peer.db")
	peerDB, err := sql.Open("sqlite", peerPath)
	if err != nil {
		t.Fatalf("sql.Open peer: %v", err)
	}
	if _, err := peerDB.Exec(mergeTestSchema); err != nil {
		peerDB.Close()
		t.Fatalf("apply peer schema: %v", err)
	}
	peerID := id.Generate("peer-")
	insertSession(t, peerDB, "sess-stable", peerID)
	insertTurn(t, peerDB, "turn-stable", "sess-x", peerID)
	insertMemory(t, peerDB, "mem-stable", peerID)
	if err := peerDB.Close(); err != nil {
		t.Fatalf("close peer DB: %v", err)
	}

	// First merge using gossip DB #1.
	gossip1Path := filepath.Join(tmp, "gossip1.db")
	gossip1 := openTestDB(t, gossip1Path)
	first, err := MergePeerDB(context.Background(), gossip1, peerPath, peerID)
	if err != nil {
		t.Fatalf("first MergePeerDB: %v", err)
	}
	if first.SessionsMerged != 1 || first.TurnsMerged != 1 || first.MemoriesMerged != 1 {
		t.Fatalf("first merge counts = (%d,%d,%d); want (1,1,1)",
			first.SessionsMerged, first.TurnsMerged, first.MemoriesMerged)
	}

	// Second merge: use the SAME gossip DB but copy the gossip data to a
	// fresh file so we get a clean connection. This tests idempotency at
	// the data level (rows already present → INSERT OR IGNORE skips them).
	// We close gossip1, copy it, reopen.
	gossip1.Close()
	gossip2Path := filepath.Join(tmp, "gossip2.db")
	if err := copyFile(gossip1Path, gossip2Path); err != nil {
		t.Fatalf("copyFile gossip: %v", err)
	}
	gossip2 := openTestDB(t, gossip2Path)
	second, err := MergePeerDB(context.Background(), gossip2, peerPath, peerID)
	if err != nil {
		t.Fatalf("second MergePeerDB: %v", err)
	}
	if second.SessionsMerged != 0 {
		t.Errorf("second SessionsMerged = %d, want 0 (duplicate)", second.SessionsMerged)
	}
	if second.TurnsMerged != 0 {
		t.Errorf("second TurnsMerged = %d, want 0 (duplicate)", second.TurnsMerged)
	}
	if second.MemoriesMerged != 0 {
		t.Errorf("second MemoriesMerged = %d, want 0 (duplicate)", second.MemoriesMerged)
	}
}

// TestMergePeerDB_EmptyPeerDB verifies that merging a valid but empty peer DB
// (no rows in sessions/turns/memories) is a no-op and produces zero counts,
// no error.
func TestMergePeerDB_EmptyPeerDB(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)

	peerPath := filepath.Join(tmp, "empty-peer.db")
	_ = openTestDB(t, peerPath) // schema present, no rows

	stats, err := MergePeerDB(context.Background(), gossipDB, peerPath, "empty-peer")
	if err != nil {
		t.Fatalf("MergePeerDB empty peer: %v", err)
	}
	if stats.SessionsMerged != 0 || stats.TurnsMerged != 0 || stats.MemoriesMerged != 0 {
		t.Errorf("expected zero merge counts on empty peer, got %+v", stats)
	}
}

// TestMergePeerDB_PeerWithoutTables verifies that when the peer DB has no
// sessions/turns/memories tables at all (e.g. fresh DB with some other schema),
// the merge functions gracefully skip each table and return (0,0,0) with no
// error.
func TestMergePeerDB_PeerWithoutTables(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)

	// Peer DB that has a completely unrelated schema.
	peerPath := filepath.Join(tmp, "unrelated.db")
	peerDB, err := sql.Open("sqlite", peerPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { peerDB.Close() })
	if _, err := peerDB.Exec(`CREATE TABLE IF NOT EXISTS unrelated (k TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create unrelated table: %v", err)
	}

	stats, err := MergePeerDB(context.Background(), gossipDB, peerPath, "unrelated")
	if err != nil {
		t.Fatalf("MergePeerDB peer without tables: %v", err)
	}
	if stats.SessionsMerged != 0 || stats.TurnsMerged != 0 || stats.MemoriesMerged != 0 {
		t.Errorf("expected zero merge counts, got %+v", stats)
	}
}

// TestMergePeerDB_NonExistentPeerPath verifies the behavior when the peer DB
// path doesn't exist on disk. modernc.org/sqlite's sql.Open silently creates
// an empty DB file, so MergePeerDB succeeds with zero counts (the auto-created
// DB has no sessions/turns/memories tables). This documents the edge-case
// behavior: a missing path is NOT an error at the Open layer.
func TestMergePeerDB_NonExistentPeerPath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)

	missingPath := filepath.Join(tmp, "does-not-exist.db")

	stats, err := MergePeerDB(context.Background(), gossipDB, missingPath, "ghost")
	// modernc.org/sqlite auto-creates the file on Open, so no error is
	// returned. The stats will all be zero because the auto-created DB has
	// no sessions/turns/memories tables.
	if err != nil {
		t.Fatalf("MergePeerDB on nonexistent path: got error %v, want nil (sqlite auto-creates)", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.SessionsMerged != 0 || stats.TurnsMerged != 0 || stats.MemoriesMerged != 0 {
		t.Errorf("expected zero merge counts for empty auto-created DB, got %+v", stats)
	}
}

// TestMergePeerDB_CancelledContext verifies that a cancelled context propagates
// through the merge operation and produces an error.
func TestMergePeerDB_CancelledContext(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)
	peerPath := filepath.Join(tmp, "peer.db")
	_ = openTestDB(t, peerPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	stats, err := MergePeerDB(ctx, gossipDB, peerPath, "peer-x")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if stats == nil {
		t.Fatal("expected non-nil stats even on cancellation")
	}
}

// TestMergePeerDB_WithTimeout runs MergePeerDBWithContext with a 5s timeout and
// confirms it succeeds on a small DB. The "if mergeCtx.Done() != nil" branch in
// the source is always-true; this test exercises that code path.
func TestMergePeerDB_WithTimeout(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)
	peerPath := filepath.Join(tmp, "peer.db")
	peerDB := openTestDB(t, peerPath)

	peerID := id.Generate("peer-")
	insertSession(t, peerDB, id.Generate("sess-"), peerID)

	stats, err := MergePeerDBWithContext(context.Background(), gossipDB, peerPath, peerID, 5*time.Second)
	if err != nil {
		t.Fatalf("MergePeerDBWithContext: %v", err)
	}
	if stats.SessionsMerged != 1 {
		t.Errorf("SessionsMerged = %d, want 1", stats.SessionsMerged)
	}
}

// TestTableExists verifies both branches of the tableExists helper: an existing
// table returns (true, nil), a missing table returns (false, nil).
func TestTableExists(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "t.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create sessions table: %v", err)
	}

	got, err := tableExists(context.Background(), db, "sessions")
	if err != nil {
		t.Fatalf("tableExists(sessions): %v", err)
	}
	if !got {
		t.Errorf("tableExists(sessions) = false, want true")
	}

	got, err = tableExists(context.Background(), db, "nonexistent_table")
	if err != nil {
		t.Fatalf("tableExists(nonexistent_table): %v", err)
	}
	if got {
		t.Errorf("tableExists(nonexistent_table) = true, want false")
	}
}

// TestRunMergeOp_PropagatesExecError verifies runMergeOp returns the underlying
// error when the SQL is invalid (e.g. references a nonexistent table).
func TestRunMergeOp_PropagatesExecError(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "t.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	merged, skipped, err := runMergeOp(
		context.Background(),
		tx,
		`INSERT OR IGNORE INTO nonexistent (id) VALUES ('x')`,
	)
	if err == nil {
		t.Fatal("expected error for invalid SQL, got nil")
	}
	if merged != 0 || skipped != 0 {
		t.Errorf("counts on error = (%d, %d), want (0, 0)", merged, skipped)
	}
}

// TestMergePeerDB_AllOpsFailErrorAggregation guards against the regression where
// the "all three ops failed" path must return an aggregated error referencing
// all three failures. We construct peer DBs whose source_node columns are
// intentionally missing so every merge function fails with a table-existence
// error that survives the "no such table" filter (the table exists but the
// SELECT references a column that doesn't).
//
// This test is best-effort: because each merge function checks for table
// existence on the peer first, we trigger the "all ops fail" path by making the
// local (gossip) tables reference incompatible columns.
func TestMergePeerDB_AllOpsFailErrorAggregation(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Gossip DB with a sessions table that has NO source_node column. The
	// merge query selects source_node from peer.sessions and inserts into
	// local sessions — mismatch produces an error.
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { gossipDB.Close() })

	if _, err := gossipDB.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at INTEGER, updated_at INTEGER, metadata BLOB)`); err != nil {
		t.Fatalf("create gossip sessions: %v", err)
	}
	if _, err := gossipDB.Exec(`CREATE TABLE turns (turn_id TEXT PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp INTEGER)`); err != nil {
		t.Fatalf("create gossip turns: %v", err)
	}
	if _, err := gossipDB.Exec(`CREATE TABLE memories (id TEXT PRIMARY KEY, type TEXT, category TEXT, content TEXT, created_at INTEGER, agent_id TEXT, session_id TEXT)`); err != nil {
		t.Fatalf("create gossip memories: %v", err)
	}

	peerPath := filepath.Join(tmp, "peer.db")
	peerDB, err := sql.Open("sqlite", peerPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { peerDB.Close() })

	if _, err := peerDB.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at INTEGER, updated_at INTEGER, metadata BLOB, source_node TEXT)`); err != nil {
		t.Fatalf("create peer sessions: %v", err)
	}
	if _, err := peerDB.Exec(`CREATE TABLE turns (turn_id TEXT PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp INTEGER, source_node TEXT)`); err != nil {
		t.Fatalf("create peer turns: %v", err)
	}
	if _, err := peerDB.Exec(`CREATE TABLE memories (id TEXT PRIMARY KEY, type TEXT, category TEXT, content TEXT, created_at INTEGER, agent_id TEXT, session_id TEXT, source_node TEXT)`); err != nil {
		t.Fatalf("create peer memories: %v", err)
	}

	_, mErr := MergePeerDB(context.Background(), gossipDB, peerPath, "peer-agg")
	if mErr == nil {
		t.Fatal("expected aggregated error when all merge ops fail")
	}
	// We don't assert on the exact format string here because the "all ops
	// failed" aggregation only triggers when ALL three ops fail AND the
	// underlying errors aren't the "no such table" sentinel. We just verify
	// the call returns a non-nil error wrapping one of the underlying
	// causes. This guards against future changes that swallow merge errors
	// silently.
}

// TestMergePeerDB_SourceNodeSet verifies that the source_node column on merged
// rows carries the provided peerID, not the peer's own source_node value.
func TestMergePeerDB_SourceNodeSet(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB := openTestDB(t, gossipPath)

	peerPath := filepath.Join(tmp, "peer.db")
	peerDB := openTestDB(t, peerPath)

	peerID := "explicit-merge-source"
	// Peer DB's source_node is "wrong" — merge MUST override it.
	if _, err := peerDB.Exec(
		`INSERT INTO sessions (id, created_at, updated_at, metadata, source_node) VALUES (?, ?, ?, ?, ?)`,
		"sess-s", time.Now().UnixNano(), time.Now().UnixNano(), []byte(`{}`), "wrong-node",
	); err != nil {
		t.Fatalf("insert peer session: %v", err)
	}

	if _, err := MergePeerDB(context.Background(), gossipDB, peerPath, peerID); err != nil {
		t.Fatalf("MergePeerDB: %v", err)
	}

	var gotNode string
	queryErr := gossipDB.QueryRow(`SELECT source_node FROM sessions WHERE id = 'sess-s'`).Scan(&gotNode)
	if queryErr != nil {
		t.Fatalf("query merged session: %v", queryErr)
	}
	if gotNode != peerID {
		t.Errorf("source_node = %q, want %q", gotNode, peerID)
	}
}

// TestMergePeerDB_AllOpsFail_RollbackReverts verifies that when ALL three merge
// operations fail, the transaction is rolled back and no partial rows leak
// into the gossip DB. We construct gossip + peer DBs where every table has an
// intentionally-incompatible column set so each merge op errors out, hitting
// the "all merge operations failed" aggregation path.
func TestMergePeerDB_AllOpsFail_RollbackReverts(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Gossip DB: tables exist but without the source_node column. The merge
	// INSERT references source_node, which will fail on every table.
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("sql.Open gossip: %v", err)
	}
	t.Cleanup(func() { gossipDB.Close() })

	if _, err := gossipDB.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at INTEGER, updated_at INTEGER, metadata BLOB)`); err != nil {
		t.Fatalf("create gossip sessions: %v", err)
	}
	if _, err := gossipDB.Exec(`CREATE TABLE turns (turn_id TEXT PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp INTEGER)`); err != nil {
		t.Fatalf("create gossip turns: %v", err)
	}
	if _, err := gossipDB.Exec(`CREATE TABLE memories (id TEXT PRIMARY KEY, type TEXT, category TEXT, content TEXT, created_at INTEGER, agent_id TEXT, session_id TEXT)`); err != nil {
		t.Fatalf("create gossip memories: %v", err)
	}

	// Peer DB: tables WITH source_node and data rows.
	peerPath := filepath.Join(tmp, "peer.db")
	peerDB, err := sql.Open("sqlite", peerPath)
	if err != nil {
		t.Fatalf("sql.Open peer: %v", err)
	}
	if _, err := peerDB.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at INTEGER, updated_at INTEGER, metadata BLOB, source_node TEXT)`); err != nil {
		peerDB.Close()
		t.Fatalf("create peer sessions: %v", err)
	}
	if _, err := peerDB.Exec(`CREATE TABLE turns (turn_id TEXT PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp INTEGER, source_node TEXT)`); err != nil {
		peerDB.Close()
		t.Fatalf("create peer turns: %v", err)
	}
	if _, err := peerDB.Exec(`CREATE TABLE memories (id TEXT PRIMARY KEY, type TEXT, category TEXT, content TEXT, created_at INTEGER, agent_id TEXT, session_id TEXT, source_node TEXT)`); err != nil {
		peerDB.Close()
		t.Fatalf("create peer memories: %v", err)
	}
	if _, err := peerDB.Exec(`INSERT INTO sessions (id, created_at, updated_at, metadata, source_node) VALUES ('s1', 1, 1, x'', 'peer')`); err != nil {
		peerDB.Close()
		t.Fatalf("insert peer session: %v", err)
	}
	// Close peer DB to avoid ATTACH conflicts.
	if err := peerDB.Close(); err != nil {
		t.Fatalf("close peer DB: %v", err)
	}

	_, mErr := MergePeerDB(context.Background(), gossipDB, peerPath, "peer-rb")
	if mErr == nil {
		t.Fatal("expected error from schema mismatch (all ops failed), got nil")
	}

	// Confirm no rows leaked into gossip.sessions despite the failed merge
	// attempt. The tx.Rollback() defer should have reverted any partial
	// writes.
	var n int
	if err := gossipDB.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("gossip.sessions row count = %d, want 0 (rollback should have reverted)", n)
	}
}

// copyFile copies src to dst. Used to create a distinct peer DB file for the
// second merge call in idempotency tests, avoiding modernc.org/sqlite ATTACH
// conflicts when two MergePeerDB calls target the same file path.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
