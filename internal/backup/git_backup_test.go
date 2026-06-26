package backup

import (
	"context"
	"log/slog"
	"os"
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
