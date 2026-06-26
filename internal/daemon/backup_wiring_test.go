package daemon

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	bkpkg "github.com/caimlas/meept/internal/backup"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// TestBackupSchedulerNilNoConfig verifies that NewComponents does not
// create a scheduler when backup config is disabled (default).
func TestBackupSchedulerNilNoConfig(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	cfg := &config.Config{
		Backup: config.DefaultBackupConfig(), // disabled
		Security: config.SecurityConfig{
			AllowedPaths: []string{t.TempDir()},
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}

	if comps.BackupScheduler != nil {
		t.Error("expected nil BackupScheduler when backup is disabled")
	}
}

// TestBackupSchedulerConstructedWhenEnabled verifies that when
// BackupConfig is valid, NewComponents produces a non-nil scheduler.
func TestBackupSchedulerConstructedWhenEnabled(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmpDir := t.TempDir()

	cfg := &config.Config{
		Backup: config.BackupConfig{
			Enabled:       true,
			RepoURL:       "git@github.com:test/backups.git",
			Schedule:      24 * time.Hour,
			RetentionDays: 7,
			CheckoutDir:   tmpDir,
		},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}

	if comps.BackupScheduler == nil {
		t.Fatal("expected non-nil BackupScheduler when backup is enabled and configured")
	}
}

// TestBackupSchedulerStartDisabled verifies the nil-scheduler path in
// Components.Start does not panic.
func TestBackupSchedulerStartDisabled(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	cfg := &config.Config{
		Backup: config.DefaultBackupConfig(),
		Security: config.SecurityConfig{
			AllowedPaths: []string{t.TempDir()},
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}

	if comps.BackupScheduler != nil {
		t.Fatal("backup scheduler should be nil in default config")
	}
}

// TestBackupSchedulerStopAfterStart verifies that stopping a started
// backup scheduler completes without hanging.
func TestBackupSchedulerStopAfterStart(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmpDir := t.TempDir()

	cfg := &config.Config{
		Backup: config.BackupConfig{
			Enabled:       true,
			RepoURL:       "git@github.com:test/backups.git",
			Schedule:      24 * time.Hour,
			RetentionDays: 7,
			CheckoutDir:   tmpDir,
		},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}

	sched := comps.BackupScheduler
	if sched == nil {
		t.Fatal("expected non-nil scheduler for stop test")
	}

	// Call Start() in a goroutine because it blocks on the ticker loop.
	// Then Stop() should unblock it by closing stopCh.
	startDone := make(chan struct{})
	go func() {
		sched.Start(context.Background())
		close(startDone)
	}()

	stopDone := make(chan struct{})
	go func() {
		sched.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// Stop returned
	case <-time.After(5 * time.Second):
		t.Fatal("BackupScheduler.Stop() did not return within 5s")
	}

	// Ensure the Start goroutine has exited by now.
	select {
	case <-startDone:
		// Start returned cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("BackupScheduler.Start() did not return within 5s after Stop")
	}
}

// ensure bkpkg.GitBackupScheduler is accessible in tests
var _ *bkpkg.GitBackupScheduler
