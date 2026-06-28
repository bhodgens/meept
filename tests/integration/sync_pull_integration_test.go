package integration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite"
)

// gossipSchemaForSync is the canonical gossip DB schema. It must match what
// the DualStore and merge functions expect.
const gossipSchemaForSync = `
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

// TestSyncPull_MergePeerIntoGossipAndPersistMetadata is the focused Phase 7
// integration test. End-to-end flow:
//  1. Construct a peer DB with sessions/turns/memories rows.
//  2. Open a fresh gossip DB (no rows yet).
//  3. Call backup.MergePeerDB to merge peer DB into gossip DB.
//  4. Construct a SyncMetadataStore on the gossip DB and record the result.
//  5. Query the metadata store and verify the recorded stats match the merge
//     output and that gossip DB now contains the peer rows.
//
// This bypasses the git layer (which is exercised by unit tests for
// findPeerBackup) and focuses on the data-flow contract between MergePeerDB
// and SyncMetadataStore that the daemon wiring depends on.
func TestSyncPull_MergePeerIntoGossipAndPersistMetadata(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// --- 1. Build peer DB with real data. ---
	peerPath := filepath.Join(tmp, "peer.db")
	peerDB, err := sql.Open("sqlite", peerPath)
	if err != nil {
		t.Fatalf("open peer DB: %v", err)
	}
	defer peerDB.Close()

	if _, err := peerDB.Exec(gossipSchemaForSync); err != nil {
		t.Fatalf("apply peer schema: %v", err)
	}

	peerID := id.Generate("peer-")
	sessionRows := 3
	turnRows := 8
	memoryRows := 2

	for i := 0; i < sessionRows; i++ {
		sid := id.Generate("sess-")
		_, err = peerDB.Exec(
			`INSERT INTO sessions (id, created_at, updated_at, metadata, source_node) VALUES (?, ?, ?, ?, ?)`,
			sid, time.Now().UnixNano(), time.Now().UnixNano(), []byte(`{}`), peerID,
		)
		if err != nil {
			t.Fatalf("insert peer session %d: %v", i, err)
		}
	}
	for i := 0; i < turnRows; i++ {
		tid := id.Generate("turn-")
		_, err = peerDB.Exec(
			`INSERT INTO turns (turn_id, session_id, role, content, timestamp, source_node) VALUES (?, ?, ?, ?, ?, ?)`,
			tid, "any-sess", "user", "msg", time.Now().UnixNano(), peerID,
		)
		if err != nil {
			t.Fatalf("insert peer turn %d: %v", i, err)
		}
	}
	for i := 0; i < memoryRows; i++ {
		mid := id.Generate("mem-")
		_, err = peerDB.Exec(
			`INSERT INTO memories (id, type, category, content, created_at, agent_id, session_id, source_node) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			mid, "episodic", "test", "body", time.Now().UnixNano(), "agent-1", "sess-x", peerID,
		)
		if err != nil {
			t.Fatalf("insert peer memory %d: %v", i, err)
		}
	}
	// Close the peer DB so MergePeerDB can ATTACH it without lock conflicts.
	if err := peerDB.Close(); err != nil {
		t.Fatalf("close peer DB: %v", err)
	}

	// --- 2. Open fresh gossip DB. ---
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("open gossip DB: %v", err)
	}
	defer gossipDB.Close()

	if _, err := gossipDB.Exec(gossipSchemaForSync); err != nil {
		t.Fatalf("apply gossip schema: %v", err)
	}

	// --- 3. Merge peer DB into gossip DB. ---
	stats, err := backup.MergePeerDB(context.Background(), gossipDB, peerPath, peerID)
	if err != nil {
		t.Fatalf("MergePeerDB: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.SessionsMerged != sessionRows {
		t.Errorf("SessionsMerged = %d, want %d", stats.SessionsMerged, sessionRows)
	}
	if stats.TurnsMerged != turnRows {
		t.Errorf("TurnsMerged = %d, want %d", stats.TurnsMerged, turnRows)
	}
	if stats.MemoriesMerged != memoryRows {
		t.Errorf("MemoriesMerged = %d, want %d", stats.MemoriesMerged, memoryRows)
	}

	// --- 4. Record stats in SyncMetadataStore. ---
	store := backup.NewSyncMetadataStore(gossipDB)
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SetLastSync(peerID, now); err != nil {
		t.Fatalf("SetLastSync: %v", err)
	}
	if err := store.SetLastMergeStats(peerID, stats); err != nil {
		t.Fatalf("SetLastMergeStats: %v", err)
	}
	if err := store.SetLastError(peerID, ""); err != nil {
		t.Fatalf("SetLastError: %v", err)
	}

	// --- 5. Verify metadata persisted. ---
	// MergeStats now carries JSON tags matching the keys written by
	// SetLastMergeStats, so all fields round-trip correctly.
	statusMap, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}
	st, ok := statusMap[peerID]
	if !ok {
		t.Fatalf("peer %q not in status map", peerID)
	}
	if st.LastSync.IsZero() {
		t.Error("LastSync is zero in metadata store")
	}
	if st.LastMergeStats == nil {
		t.Fatal("LastMergeStats is nil in metadata store")
	}
	if st.LastMergeStats.Skipped != 0 {
		t.Errorf("stored Skipped = %d, want 0", st.LastMergeStats.Skipped)
	}
	if st.LastMergeStats.Errors != 0 {
		t.Errorf("stored Errors = %d, want 0", st.LastMergeStats.Errors)
	}
	if st.LastMergeStats.SessionsMerged != sessionRows {
		t.Errorf("stored SessionsMerged = %d, want %d", st.LastMergeStats.SessionsMerged, sessionRows)
	}
	if st.LastMergeStats.TurnsMerged != turnRows {
		t.Errorf("stored TurnsMerged = %d, want %d", st.LastMergeStats.TurnsMerged, turnRows)
	}
	if st.LastMergeStats.MemoriesMerged != memoryRows {
		t.Errorf("stored MemoriesMerged = %d, want %d", st.LastMergeStats.MemoriesMerged, memoryRows)
	}
	if st.Error != "" {
		t.Errorf("stored Error = %q, want empty", st.Error)
	}

	// --- 6. Verify the gossip DB actually contains the merged rows. ---
	var gotSessions, gotTurns, gotMemories int
	if err := gossipDB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE source_node = ?`, peerID).Scan(&gotSessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := gossipDB.QueryRow(`SELECT COUNT(*) FROM turns WHERE source_node = ?`, peerID).Scan(&gotTurns); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if err := gossipDB.QueryRow(`SELECT COUNT(*) FROM memories WHERE source_node = ?`, peerID).Scan(&gotMemories); err != nil {
		t.Fatalf("count memories: %v", err)
	}
	if gotSessions != sessionRows {
		t.Errorf("gossip sessions count = %d, want %d", gotSessions, sessionRows)
	}
	if gotTurns != turnRows {
		t.Errorf("gossip turns count = %d, want %d", gotTurns, turnRows)
	}
	if gotMemories != memoryRows {
		t.Errorf("gossip memories count = %d, want %d", gotMemories, memoryRows)
	}

	// Remove temp DB files explicitly to ensure cleanup (the deferred Close
	// happens before TempDir auto-cleanup, but this is belt-and-suspenders).
	_ = os.Remove(peerPath)
	_ = os.Remove(gossipPath)
}

// TestSyncPull_MergeIsIdempotent_AcrossTwoCycles verifies that running two
// merge cycles against the same peer does not duplicate rows in the gossip DB
// (INSERT OR IGNORE contract) and the second cycle reports zero newly-merged
// rows.
//
// Each cycle uses a fresh gossip DB connection to avoid modernc.org/sqlite's
// ATTACH alias persistence issue across transactions on the same connection
// pool.
func TestSyncPull_MergeIsIdempotent_AcrossTwoCycles(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Build peer DB with stable IDs.
	peerPath := filepath.Join(tmp, "peer.db")
	peerDB, err := sql.Open("sqlite", peerPath)
	if err != nil {
		t.Fatalf("open peer DB: %v", err)
	}
	if _, err := peerDB.Exec(gossipSchemaForSync); err != nil {
		t.Fatalf("apply peer schema: %v", err)
	}
	peerID := id.Generate("peer-")
	stableSessionID := "sess-stable-id"
	_, err = peerDB.Exec(
		`INSERT INTO sessions (id, created_at, updated_at, metadata, source_node) VALUES (?, ?, ?, ?, ?)`,
		stableSessionID, 1, 1, []byte(`{}`), peerID,
	)
	if err != nil {
		t.Fatalf("insert stable session: %v", err)
	}
	if err := peerDB.Close(); err != nil {
		t.Fatalf("close peer DB: %v", err)
	}

	// Open gossip DB for cycle 1.
	gossipPath := filepath.Join(tmp, "gossip.db")
	gossip1, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("open gossip DB cycle 1: %v", err)
	}
	if _, err := gossip1.Exec(gossipSchemaForSync); err != nil {
		gossip1.Close()
		t.Fatalf("apply gossip schema cycle 1: %v", err)
	}

	// Cycle 1: merges the session.
	first, err := backup.MergePeerDB(context.Background(), gossip1, peerPath, peerID)
	if err != nil {
		gossip1.Close()
		t.Fatalf("first MergePeerDB: %v", err)
	}
	if first.SessionsMerged != 1 {
		t.Errorf("first cycle SessionsMerged = %d, want 1", first.SessionsMerged)
	}
	gossip1.Close()

	// Cycle 2: open the same gossip DB file (now containing the stable row)
	// via a fresh connection to avoid the ATTACH-alias issue.
	gossip2, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("open gossip DB cycle 2: %v", err)
	}
	defer gossip2.Close()

	second, err := backup.MergePeerDB(context.Background(), gossip2, peerPath, peerID)
	if err != nil {
		t.Fatalf("second MergePeerDB: %v", err)
	}
	if second.SessionsMerged != 0 {
		t.Errorf("second cycle SessionsMerged = %d, want 0 (idempotent)", second.SessionsMerged)
	}

	// Verify only one row exists in gossip.sessions for the stable ID.
	var n int
	if err := gossip2.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE id = ?`, stableSessionID,
	).Scan(&n); err != nil {
		t.Fatalf("count stable session: %v", err)
	}
	if n != 1 {
		t.Errorf("stable session row count = %d, want 1 (idempotent merge)", n)
	}
}
