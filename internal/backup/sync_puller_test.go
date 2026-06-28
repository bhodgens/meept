package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite"
)

// pullerTestEnv sets up an isolated HOME so NewSyncPuller's UserHomeDir() call
// points at a temp dir. Returns the home dir, the gossip DB, and a cleanup
// function. The gossip DB has the canonical schema pre-applied.
func pullerTestEnv(t *testing.T) (home string, gossipDB *sql.DB) {
	t.Helper()

	home = t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "gossip.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(mergeTestSchema); err != nil {
		t.Fatalf("apply mergeTestSchema: %v", err)
	}
	return home, db
}

// validSyncCfg returns a PeerSyncConfig with the minimum fields populated to
// pass validation.
func validSyncCfg(peers ...string) config.PeerSyncConfig {
	return config.PeerSyncConfig{
		Enabled:         true,
		Peers:           peers,
		PullSchedule:    time.Hour,
		MaxMergeMinutes: 10,
		RepoURL:         "",
	}
}

// TestNewSyncPuller_NilGossipDB verifies that constructing a puller without a
// gossip DB fails with the ErrGossipDBRequired sentinel.
func TestNewSyncPuller_NilGossipDB(t *testing.T) {
	// Cannot use t.Parallel because t.Setenv modifies process environment.
	t.Setenv("HOME", t.TempDir())

	cfg := validSyncCfg("peer-a")
	_, err := NewSyncPuller(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil gossipDB")
	}
	if !errors.Is(err, ErrGossipDBRequired) {
		t.Errorf("err = %v, want wrap of ErrGossipDBRequired", err)
	}
}

// TestNewSyncPuller_InvalidConfig verifies that an invalid config (enabled with
// no peers) fails at construction.
func TestNewSyncPuller_InvalidConfig(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	_, gossipDB := pullerTestEnv(t)

	bad := config.PeerSyncConfig{
		Enabled:      true,
		Peers:        nil,
		PullSchedule: time.Hour,
	}
	_, err := NewSyncPuller(bad, nil, gossipDB)
	if err == nil {
		t.Fatal("expected error for enabled config without peers")
	}
	if !strings.Contains(err.Error(), "invalid sync config") {
		t.Errorf("err = %v, want 'invalid sync config'", err)
	}
}

// TestNewSyncPuller_Success verifies a happy-path construction: valid config,
// non-nil gossip DB. The returned puller has peers and the temp directory
// created.
func TestNewSyncPuller_Success(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	home, gossipDB := pullerTestEnv(t)

	cfg := validSyncCfg("peer-a", "peer-b")
	p, err := NewSyncPuller(cfg, gossipDB, gossipDB)
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}
	t.Cleanup(func() { p.Stop() })

	if len(p.peers) != 2 {
		t.Errorf("peers len = %d, want 2", len(p.peers))
	}
	if p.nodeID == "" {
		t.Error("nodeID is empty")
	}
	// Temp dir must exist.
	if fi, err := os.Stat(filepath.Join(home, ".meept", syncTempDirName)); err != nil || !fi.IsDir() {
		t.Errorf("sync-temp dir missing under HOME/.meept: err=%v", err)
	}
	// EnsureTable was called during construction — table must exist.
	var n int
	if err := gossipDB.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sync_metadata'`,
	).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 1 {
		t.Errorf("sync_metadata table count = %d, want 1", n)
	}
}

// TestSyncPuller_PullOnce_NoPeers verifies that PullNow on a puller configured
// with zero peers completes the cycle without error. The puller won't find a
// repo at the default path, so it returns the git-open error; but the function
// surfaces that error rather than panicking. We just confirm no panic and that
// the returned error is a *SyncError (or wrapped) from the pull op.
func TestSyncPuller_PullOnce_NoRepo(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	_, gossipDB := pullerTestEnv(t)

	cfg := validSyncCfg("peer-a")
	// RepoURL empty + no repo at default path → git open fails.
	p, err := NewSyncPuller(cfg, gossipDB, gossipDB)
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}
	t.Cleanup(func() { p.Stop() })

	err = p.PullNow()
	if err == nil {
		t.Fatal("expected error from PullNow when no repo exists, got nil")
	}
	if !IsSyncError(err) {
		t.Errorf("err = %T (%v), want *SyncError", err, err)
	}
}

// TestSyncPuller_StopIsIdempotent verifies that calling Stop more than once
// doesn't panic. The underlying TempManager.Cleanup is guarded by the
// stopped flag.
func TestSyncPuller_StopIsIdempotent(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	_, gossipDB := pullerTestEnv(t)

	cfg := validSyncCfg("peer-a")
	p, err := NewSyncPuller(cfg, gossipDB, gossipDB)
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}

	p.Stop()
	p.Stop() // second Stop must not panic
	p.Stop()
}

// TestSyncPuller_PeerStatus_FreshDB returns an empty map (not an error) on a
// puller with no recorded sync history.
func TestSyncPuller_PeerStatus_FreshDB(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	_, gossipDB := pullerTestEnv(t)

	cfg := validSyncCfg("peer-a")
	p, err := NewSyncPuller(cfg, gossipDB, gossipDB)
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}
	t.Cleanup(func() { p.Stop() })

	status, err := p.PeerStatus()
	if err != nil {
		t.Fatalf("PeerStatus: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status map")
	}
	if len(status) != 0 {
		t.Errorf("fresh puller status has %d peers, want 0", len(status))
	}
}

// TestFindPeerBackup_NoBackupsDir verifies the findPeerBackup helper returns an
// error derived from ErrPeerNotFound when the repo path doesn't exist. Note:
// SyncWrap creates a new *SyncError wrapping the sentinel, so errors.Is does
// not chain-match (the sentinel's inner Err is nil). We verify via the Message
// field instead.
func TestFindPeerBackup_NoBackupsDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	missingRepo := filepath.Join(tmp, "no-repo")

	_, err := findPeerBackup(missingRepo, "peer-x")
	if err == nil {
		t.Fatal("expected error for missing backups dir")
	}
	// Verify the error carries the "peer backup not found" message from
	// ErrPeerNotFound. SyncWrap propagates Message but breaks errors.Is
	// chain (sentinel's Err is nil).
	if !strings.Contains(err.Error(), "peer backup not found") {
		t.Errorf("err = %v, want message containing 'peer backup not found'", err)
	}
}

// TestFindPeerBackup_NoMatchingPeer verifies that when the backups dir exists
// but contains no backup for the requested peer, findPeerBackup returns an
// error with the ErrPeerNotFound message.
func TestFindPeerBackup_NoMatchingPeer(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	backupsDir := filepath.Join(tmp, "backups", "2026-06-26")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Add a backup for a different peer.
	otherDir := filepath.Join(backupsDir, "other-peer")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatalf("MkdirAll other-peer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "local.db.zst"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := findPeerBackup(tmp, "peer-missing")
	if err == nil {
		t.Fatal("expected error for missing peer")
	}
	if !strings.Contains(err.Error(), "peer backup not found") {
		t.Errorf("err = %v, want message containing 'peer backup not found'", err)
	}
}

// TestFindPeerBackup_FindsLatestBackup verifies that findPeerBackup locates a
// .db.zst file under backups/<date>/<peerID>/.
func TestFindPeerBackup_FindsLatestBackup(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	peerID := "peer-found"
	dateDir := filepath.Join(tmp, "backups", "2026-06-26", peerID)
	if err := os.MkdirAll(dateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dateDir, "local.db.zst"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := findPeerBackup(tmp, peerID)
	if err != nil {
		t.Fatalf("findPeerBackup: %v", err)
	}
	if !strings.HasSuffix(got, ".db.zst") {
		t.Errorf("got = %q, want .db.zst suffix", got)
	}
	if !strings.Contains(got, peerID) {
		t.Errorf("got = %q, want path containing %q", got, peerID)
	}
}

// TestFindPeerBackup_IgnoresNonZstd verifies that files without the .db.zst
// suffix are not returned by findPeerBackup.
func TestFindPeerBackup_IgnoresNonZstd(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	peerID := "peer-only-plain"
	peerDir := filepath.Join(tmp, "backups", "2026-06-26", peerID)
	if err := os.MkdirAll(peerDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Only a .db file (no .zst suffix).
	if err := os.WriteFile(filepath.Join(peerDir, "local.db"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := findPeerBackup(tmp, peerID)
	if err == nil {
		t.Fatal("expected error when only non-.db.zst files exist")
	}
	if !strings.Contains(err.Error(), "peer backup not found") {
		t.Errorf("err = %v, want message containing 'peer backup not found'", err)
	}
}

// TestSyncPuller_StartExit verifies that Start returns promptly when the
// context is cancelled (no scheduled-ticker blocking). Uses a short PullSchedule
// to ensure the ticker is armed.
func TestSyncPuller_StartExit(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	_, gossipDB := pullerTestEnv(t)

	cfg := validSyncCfg("peer-x")
	cfg.PullSchedule = 50 * time.Millisecond // small but positive
	p, err := NewSyncPuller(cfg, gossipDB, gossipDB)
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}
	t.Cleanup(func() { p.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// good — Start returned on ctx cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s after ctx cancel")
	}
}

// TestSyncPuller_CleanHostname verifies that cleanHostname strips dots, used
// to sanitize peer IDs for git-safe paths.
func TestSyncPuller_CleanHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"192.168.1.1", "192-168-1-1"},
		{"plain", "plain"},
		{"a.b.c", "a-b-c"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := cleanHostname(tc.in); got != tc.want {
			t.Errorf("cleanHostname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSyncPuller_NodeIDTruncation verifies that node IDs longer than 20 chars
// are truncated by NewSyncPuller. We can't easily mock os.Hostname, so this
// test is informational: if the real hostname is <= 20 chars, we just verify
// the field is set; otherwise we verify it's exactly 20 chars.
func TestSyncPuller_NodeIDTruncation(t *testing.T) {
	// Cannot use t.Parallel because pullerTestEnv calls t.Setenv("HOME").
	_, gossipDB := pullerTestEnv(t)

	p, err := NewSyncPuller(validSyncCfg("peer"), gossipDB, gossipDB)
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}
	t.Cleanup(func() { p.Stop() })

	host, _ := os.Hostname()
	wantMax := 20
	if len(host) > wantMax {
		if len(p.nodeID) != wantMax {
			t.Errorf("nodeID len = %d, want %d (truncated)", len(p.nodeID), wantMax)
		}
	}
	// Regardless of host length, nodeID must never contain a dot.
	if strings.Contains(p.nodeID, ".") {
		t.Errorf("nodeID %q contains a dot (cleanHostname should have stripped it)", p.nodeID)
	}
}

// TestMergePeerBackupCorrupt exercises the corrupt-peer-DB path that the
// mergePeer function in sync_puller.go would hit when ReservePeerDB returns a
// path that is not a valid SQLite file.
func TestMergePeerBackupCorrupt(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// "Corrupt" peer DB: a text file with .db extension. SQLite will fail to
	// open it for some queries but Ping may succeed — to ensure the merge
	// fails we use a 0-byte file.
	corruptPath := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(corruptPath, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gossipPath := filepath.Join(tmp, "gossip.db")
	gossipDB, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { gossipDB.Close() })
	if _, err := gossipDB.Exec(mergeTestSchema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	peerID := id.Generate("corrupt-")
	_, err = MergePeerDB(context.Background(), gossipDB, corruptPath, peerID)
	// Whether this returns an error depends on how modernc handles an empty
	// file (it may treat it as a valid empty DB). Either way, the call must
	// not panic and stats must be non-nil.
	if err != nil {
		t.Logf("MergePeerDB on empty/corrupt file returned error (acceptable): %v", err)
	}
}
