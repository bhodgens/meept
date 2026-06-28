package backup

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newMetaStore creates a fresh SyncMetadataStore backed by an on-disk SQLite
// DB in the test's TempDir. The table is pre-created.
func newMetaStore(t *testing.T) (*SyncMetadataStore, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "meta.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := NewSyncMetadataStore(db)
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	return store, db
}

// TestSyncMetadataStore_EnsureTableIdempotent verifies that EnsureTable can be
// called multiple times without error. The IF NOT EXISTS clause must make this
// safe.
func TestSyncMetadataStore_EnsureTableIdempotent(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	// Second call on a DB that already has the table — must succeed.
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("second EnsureTable: %v", err)
	}
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("third EnsureTable: %v", err)
	}

	// Verify table actually exists.
	var n int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sync_metadata'`,
	).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 1 {
		t.Errorf("sync_metadata table count = %d, want 1", n)
	}
}

// TestSyncMetadataStore_GetAllSyncStatus_FreshDB verifies that querying a fresh
// DB with no metadata rows returns an empty (non-nil) map and no error. This
// guards against the regression where an empty DB returns nil and panics
// callers.
func TestSyncMetadataStore_GetAllSyncStatus_FreshDB(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus on fresh DB: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map on fresh DB")
	}
	if len(result) != 0 {
		t.Errorf("fresh DB returned %d entries, want 0", len(result))
	}
}

// TestSyncMetadataStore_GetLastSync_UnknownPeer returns zero time + no error
// when the peer has never been synced.
func TestSyncMetadataStore_GetLastSync_UnknownPeer(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	ts, err := store.GetLastSync("never-synced-peer")
	if err != nil {
		t.Fatalf("GetLastSync unknown peer: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for unknown peer, got %v", ts)
	}
}

// TestSyncMetadataStore_SetLastSync_Persists verifies round-trip
// SetLastSync -> GetLastSync preserves the timestamp (at millisecond
// precision).
func TestSyncMetadataStore_SetLastSync_Persists(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	peerID := "peer-persist"
	want := time.Date(2026, 6, 26, 12, 30, 0, 0, time.UTC)

	if err := store.SetLastSync(peerID, want); err != nil {
		t.Fatalf("SetLastSync: %v", err)
	}

	got, err := store.GetLastSync(peerID)
	if err != nil {
		t.Fatalf("GetLastSync: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("GetLastSync = %v, want %v", got, want)
	}
	// Verify UTC normalization.
	if got.Location() != time.UTC {
		t.Errorf("GetLastSync location = %v, want UTC", got.Location())
	}
}

// TestSyncMetadataStore_SetLastSync_Overwrites verifies that calling SetLastSync
// twice for the same peer replaces the previous value (INSERT OR REPLACE).
func TestSyncMetadataStore_SetLastSync_Overwrites(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	peerID := "peer-overwrite"
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)

	if err := store.SetLastSync(peerID, first); err != nil {
		t.Fatalf("first SetLastSync: %v", err)
	}
	if err := store.SetLastSync(peerID, second); err != nil {
		t.Fatalf("second SetLastSync: %v", err)
	}
	got, err := store.GetLastSync(peerID)
	if err != nil {
		t.Fatalf("GetLastSync: %v", err)
	}
	if !got.Equal(second) {
		t.Errorf("GetLastSync = %v, want %v (latest value)", got, second)
	}
}

// TestSyncMetadataStore_SetLastMergeStats_Persistence verifies that merge stats
// round-trip through SetLastMergeStats + GetAllSyncStatus. The MergeStats struct
// uses json tags ("sessions", "turns", "memories") matching the keys written by
// SetLastMergeStats, so all fields round-trip correctly.
func TestSyncMetadataStore_SetLastMergeStats_Persistence(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	peerID := "peer-stats"
	want := &MergeStats{
		SessionsMerged: 7,
		TurnsMerged:    42,
		MemoriesMerged: 3,
		Skipped:        9,
		Errors:         1,
	}
	if err := store.SetLastMergeStats(peerID, want); err != nil {
		t.Fatalf("SetLastMergeStats: %v", err)
	}

	if err := store.SetLastSync(peerID, time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync: %v", err)
	}

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}

	st, ok := result[peerID]
	if !ok {
		t.Fatalf("peer %q not in result", peerID)
	}
	if st.LastMergeStats == nil {
		t.Fatal("LastMergeStats is nil in returned status")
	}
	if st.LastMergeStats.Skipped != 9 {
		t.Errorf("Skipped = %d, want 9", st.LastMergeStats.Skipped)
	}
	if st.LastMergeStats.Errors != 1 {
		t.Errorf("Errors = %d, want 1", st.LastMergeStats.Errors)
	}
	if st.LastMergeStats.SessionsMerged != 7 {
		t.Errorf("SessionsMerged = %d, want 7", st.LastMergeStats.SessionsMerged)
	}
	if st.LastMergeStats.TurnsMerged != 42 {
		t.Errorf("TurnsMerged = %d, want 42", st.LastMergeStats.TurnsMerged)
	}
	if st.LastMergeStats.MemoriesMerged != 3 {
		t.Errorf("MemoriesMerged = %d, want 3", st.LastMergeStats.MemoriesMerged)
	}
}

// TestSyncMetadataStore_SetLastMergeStats_JSONKeyTags verifies that the JSON
// produced by SetLastMergeStats uses the short keys ("sessions", "turns",
// "memories") that match the MergeStats struct's json tags, ensuring all
// fields round-trip through json.Marshal/Unmarshal.
func TestSyncMetadataStore_SetLastMergeStats_JSONKeyTags(t *testing.T) {
	t.Parallel()

	store, db := newMetaStore(t)
	peerID := "peer-keytags"
	want := &MergeStats{SessionsMerged: 5, TurnsMerged: 10, MemoriesMerged: 2, Skipped: 3, Errors: 0}
	if err := store.SetLastMergeStats(peerID, want); err != nil {
		t.Fatalf("SetLastMergeStats: %v", err)
	}

	var raw string
	err := db.QueryRow("SELECT value FROM sync_metadata WHERE key = ?", "last_merge_stats:"+peerID).Scan(&raw)
	if err != nil {
		t.Fatalf("query raw value: %v", err)
	}

	// Verify all five keys present in the serialized JSON.
	for _, key := range []string{`"sessions":5`, `"turns":10`, `"memories":2`, `"skipped":3`, `"errors":0`} {
		if !strings.Contains(raw, key) {
			t.Errorf("raw JSON %q missing key %q", raw, key)
		}
	}

	// Round-trip through json.Unmarshal to confirm struct tags match.
	var got MergeStats
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SessionsMerged != 5 || got.TurnsMerged != 10 || got.MemoriesMerged != 2 ||
		got.Skipped != 3 || got.Errors != 0 {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, *want)
	}
}

// TestSyncMetadataStore_SetLastError verifies error storage and retrieval via
// GetAllSyncStatus.
func TestSyncMetadataStore_SetLastError(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	peerID := "peer-err"
	wantMsg := "git pull timed out after 30s"
	if err := store.SetLastError(peerID, wantMsg); err != nil {
		t.Fatalf("SetLastError: %v", err)
	}

	// Set LastSync so the peer shows up in the result map.
	if err := store.SetLastSync(peerID, time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync: %v", err)
	}

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}

	st, ok := result[peerID]
	if !ok {
		t.Fatalf("peer %q not in result", peerID)
	}
	if st.Error != wantMsg {
		t.Errorf("Error = %q, want %q", st.Error, wantMsg)
	}
}

// TestSyncMetadataStore_SetLastError_EmptyClears verifies that setting an empty
// error string is stored (the mergePeer code uses "" to clear errors). We
// don't delete the row — we store empty — which GetAllSyncStatus surfaces as
// Error == "".
func TestSyncMetadataStore_SetLastError_EmptyClears(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	peerID := "peer-clear"
	if err := store.SetLastError(peerID, "first error"); err != nil {
		t.Fatalf("first SetLastError: %v", err)
	}
	if err := store.SetLastError(peerID, ""); err != nil {
		t.Fatalf("second SetLastError (empty): %v", err)
	}
	if err := store.SetLastSync(peerID, time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync: %v", err)
	}

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}
	st, ok := result[peerID]
	if !ok {
		t.Fatalf("peer %q not in result", peerID)
	}
	if st.Error != "" {
		t.Errorf("Error = %q, want empty after clear", st.Error)
	}
}

// TestSyncMetadataStore_ClearPeerStatus removes every key for a peer:
// last_sync, last_merge_stats, and last_error. A subsequent GetAllSyncStatus
// must not include the peer.
func TestSyncMetadataStore_ClearPeerStatus(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	peerID := "peer-clear-all"
	otherPeer := "peer-other"

	// Populate all three keys for the target peer, plus one for another peer.
	if err := store.SetLastSync(peerID, time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync target: %v", err)
	}
	if err := store.SetLastMergeStats(peerID, &MergeStats{SessionsMerged: 1}); err != nil {
		t.Fatalf("SetLastMergeStats target: %v", err)
	}
	if err := store.SetLastError(peerID, "boom"); err != nil {
		t.Fatalf("SetLastError target: %v", err)
	}
	if err := store.SetLastSync(otherPeer, time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync other: %v", err)
	}

	// Clear target.
	if err := store.ClearPeerStatus(peerID); err != nil {
		t.Fatalf("ClearPeerStatus: %v", err)
	}

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}
	if _, stillThere := result[peerID]; stillThere {
		t.Errorf("cleared peer %q still in result", peerID)
	}
	if _, ok := result[otherPeer]; !ok {
		t.Errorf("other peer %q missing from result after clearing target", otherPeer)
	}
}

// TestSyncMetadataStore_GetAllSyncStatus_MultiPeer verifies that multiple peers
// with partial metadata (some with only last_sync, some with errors) all appear
// in the result map.
func TestSyncMetadataStore_GetAllSyncStatus_MultiPeer(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	if err := store.SetLastSync("peer-A", time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync A: %v", err)
	}
	if err := store.SetLastSync("peer-B", time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync B: %v", err)
	}
	if err := store.SetLastError("peer-B", "timeout"); err != nil {
		t.Fatalf("SetLastError B: %v", err)
	}
	if err := store.SetLastError("peer-C", "never synced but errored"); err != nil {
		t.Fatalf("SetLastError C: %v", err)
	}

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("result has %d peers, want 3", len(result))
	}
	if _, ok := result["peer-A"]; !ok {
		t.Error("peer-A missing")
	}
	if _, ok := result["peer-B"]; !ok {
		t.Error("peer-B missing")
	}
	if _, ok := result["peer-C"]; !ok {
		t.Error("peer-C missing (should appear via last_error key)")
	}
	if result["peer-C"].Error == "" {
		t.Error("peer-C Error is empty, expected 'never synced but errored'")
	}
}

// TestSyncMetadataStore_GetAllSyncStatus_EmptyPeerIDKey verifies the guard
// against empty peer IDs (keys like "last_sync:" with empty peer). These
// rows are filtered out and do not appear in the result.
func TestSyncMetadataStore_GetAllSyncStatus_EmptyPeerIDKey(t *testing.T) {
	t.Parallel()

	store, _ := newMetaStore(t)

	// Manually insert a malformed row that would result from an empty peer ID.
	if _, err := store.db.Exec(
		`INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)`,
		"last_sync:", "12345",
	); err != nil {
		t.Fatalf("insert empty-peer last_sync: %v", err)
	}
	// Also insert a valid row.
	if err := store.SetLastSync("real-peer", time.Now().UTC()); err != nil {
		t.Fatalf("SetLastSync real-peer: %v", err)
	}

	result, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus: %v", err)
	}
	if _, ok := result[""]; ok {
		t.Error("empty peer ID should be filtered out")
	}
	if _, ok := result["real-peer"]; !ok {
		t.Error("real-peer should be present")
	}
}
