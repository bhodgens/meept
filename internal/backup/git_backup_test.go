package backup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/go-git/go-git/v5"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewGitBackupScheduler(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      24 * time.Hour,
		RetentionDays: 12,
		NodeID:        "test-node",
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
	if s.nodeID != "test-node" {
		t.Errorf("nodeID: got %q, want %q", s.nodeID, "test-node")
	}
	if s.cfg.Schedule != 24*time.Hour {
		t.Errorf("schedule: got %v, want %v", s.cfg.Schedule, 24*time.Hour)
	}
}

func TestNewGitBackupScheduler_InvalidConfig(t *testing.T) {
	// Enabled but no repo URL
	cfg := config.BackupConfig{
		Enabled:       true,
		RetentionDays: 12,
	}

	_, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	if !IsBackupError(err) {
		t.Errorf("expected BackupError, got %T", err)
	}
}

func TestNewGitBackupScheduler_DefaultNodeID(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 7,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}
	if s.nodeID == "" {
		t.Error("expected non-empty nodeID (default)")
	}
}

func TestNewGitBackupScheduler_Disabled(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled: false,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil scheduler for disabled config")
	}
}

func TestGitBackupScheduler_Stop(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 7,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}

	// Stop should be safe to call even without Start
	s.Stop() // should not panic
}

func TestGitBackupScheduler_Callback(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 7,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}

	var mu sync.Mutex
	var callbackFired bool

	s.SetOnBackupDone(func(_ *BackupManifest, err error) {
		mu.Lock()
		defer mu.Unlock()
		callbackFired = true
		_ = err
	})

	_ = s.RunNow() // may fail without real git repo; callback should fire regardless
	mu.Lock()
	fired := callbackFired
	mu.Unlock()
	t.Log("callback fired:", fired)
}

func TestDefaultNodeID(t *testing.T) {
	// defaultNodeID should return a reasonable hostname
	hostname, _ := os.Hostname()
	if hostname != "" {
		id := defaultNodeID()
		if len(id) > 20 {
			t.Errorf("nodeID should be <= 20 chars, got %d: %s", len(id), id)
		}
		if strings.Contains(id, ".") {
			t.Error("nodeID should not contain dots")
		}
	}
}

func Test_pruneOldBackups(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 30,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}

	// pruneOldBackups with nil repo should not panic
	err = s.pruneOldBackups()
	if err != nil {
		t.Logf("pruneOldBackups: %v (may be non-fatal with no repo)", err)
	}
}

func TestScheduler_StartAndStop(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      500 * time.Millisecond,
		RetentionDays: 7,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start in goroutine
	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop should work
	s.Stop()

	// Give it time to exit
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop in time")
	}
}

func TestIsGitConflict(t *testing.T) {
	_ = strings.NewReplacer() // verify NewReplacer compiles

	if isGitConflict(nil) {
		t.Error("isGitConflict(nil) should return false")
	}
	if isGitConflict(git.NoErrAlreadyUpToDate) {
		t.Error("NoErrAlreadyUpToDate should not be a conflict")
	}

	// Test text matching
	conflictErr := &BackupError{Op: "git_push", Message: "non-fast-forward update"}
	if !isGitConflict(conflictErr) {
		t.Error("non-fast-forward error should be a conflict")
	}
}

func TestInvokeCallback_NilCallback(t *testing.T) {
	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 7,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}

	// Invoke with nil callback should not panic
	s.invokeCallback(nil, nil)
	s.invokeCallback(&BackupManifest{}, nil)
}

// TestRunBackup_SHA256MatchesCompressedFile verifies that runBackup computes
// the SHA256 hash on the actual compressed file path (regression test for the
// path-suffix mismatch bug where CompressFile used to append ".zst" internally).
//
// The test exercises the compress + SHA256 portion of runBackup. The final git
// commit step may fail in this minimal test environment (no remote configured,
// absolute-path worktree edge cases) but the callback fires with the manifest
// before that matters. The assertion is that the manifest's SHA256 matches a
// fresh ComputeSHA256 on the CompressedPath.
func TestRunBackup_SHA256MatchesCompressedFile(t *testing.T) {
	tmp := t.TempDir()

	cfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       tmp,
		Schedule:      time.Hour,
		RetentionDays: 7,
		NodeID:        "test-node",
		CheckoutDir:   tmp,
	}

	s, err := NewGitBackupScheduler(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewGitBackupScheduler: %v", err)
	}

	// Create a local.db so GetLocalDBPaths finds something to back up.
	dbPath := filepath.Join(s.dataDir, "local.db")
	dbContent := []byte("SQLite format 3\x00test backup sha verification")
	if err := os.WriteFile(dbPath, dbContent, 0o644); err != nil {
		t.Fatalf("WriteFile local.db: %v", err)
	}

	// Capture the manifest via callback.
	var mu sync.Mutex
	var gotManifest *BackupManifest
	s.SetOnBackupDone(func(m *BackupManifest, e error) {
		mu.Lock()
		defer mu.Unlock()
		gotManifest = m
	})

	if err := s.initRepo(); err != nil {
		t.Fatalf("initRepo: %v", err)
	}

	// runBackup may fail at the git-commit step; that's OK — the compress and
	// SHA256 steps run before git and the manifest is delivered via callback.
	_ = s.runBackup(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if gotManifest == nil || len(gotManifest.Databases) == 0 {
		t.Fatal("expected non-empty manifest with databases from callback")
	}

	dbInfo := gotManifest.Databases[0]

	// The CompressedPath must exist on disk.
	if _, err := os.Stat(dbInfo.CompressedPath); err != nil {
		t.Fatalf("compressed file does not exist at %s: %v", dbInfo.CompressedPath, err)
	}

	// The SHA256 must match a fresh ComputeSHA256 on the CompressedPath.
	actualSHA, err := ComputeSHA256(dbInfo.CompressedPath)
	if err != nil {
		t.Fatalf("ComputeSHA256(%s): %v", dbInfo.CompressedPath, err)
	}
	if actualSHA != dbInfo.SHA256 {
		t.Errorf("SHA256 mismatch: manifest=%s, actual=%s (path=%s)",
			dbInfo.SHA256, actualSHA, dbInfo.CompressedPath)
	}

	// No double-suffixed file should exist.
	if _, err := os.Stat(dbInfo.CompressedPath + ".zst"); err == nil {
		t.Errorf("unexpected double-suffixed file exists at %s.zst", dbInfo.CompressedPath)
	}
}
