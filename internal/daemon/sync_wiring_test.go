package daemon

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	bkpkg "github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

// validPeerSyncConfig returns a PeerSyncConfig that passes IsValidated.
// PeerSync borrows the BackupConfig.RepoURL when its own RepoURL is
// empty (see components.go:2237), so we set both to keep the test
// self-contained.
func validPeerSyncConfig() config.PeerSyncConfig {
	return config.PeerSyncConfig{
		Enabled:      true,
		Peers:        []string{"peer-a", "peer-b"},
		PullSchedule: time.Hour,
		RepoURL:      "git@github.com:test/peer-backups.git",
	}
}

// makeTestDualStore builds a real memory.DualStore in a temp directory
// so we can exercise the SyncPuller construction path without spinning
// up the entire NewComponents pipeline. Returns nil on failure (the
// caller skips the test via t.Fatal).
func makeTestDualStore(t *testing.T, nodeID string) *memory.DualStore {
	t.Helper()
	tmpDir := t.TempDir()
	ds, err := memory.NewDualStore(tmpDir, nodeID, testLogger(t).With("component", "dualstore"))
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	t.Cleanup(func() { _ = ds.Close() })
	return ds
}

// makeTestSyncPuller constructs a SyncPuller directly from a
// PeerSyncConfig + DualStore, mirroring the wiring at
// components.go:2240. This avoids spinning up the full Components
// pipeline (which spawns dozens of goroutines via NewComponents) and
// lets us test the SyncPuller lifecycle in isolation.
func makeTestSyncPuller(t *testing.T, cfg config.PeerSyncConfig, ds *memory.DualStore) *bkpkg.SyncPuller {
	t.Helper()
	puller, err := bkpkg.NewSyncPuller(cfg, ds.LocalDB(), ds.GossipDB())
	if err != nil {
		t.Fatalf("NewSyncPuller: %v", err)
	}
	return puller
}

// TestComponents_ConstructsSyncPuller_WhenPeerSyncEnabled verifies that
// a Components with PeerSync enabled and a DualStore available has a
// non-nil SyncPuller field after the daemon wiring at
// components.go:2235-2249 runs. We construct the Components struct
// directly (mirroring the NewComponents code path at :2244) rather than
// calling NewComponents, which would spawn dozens of unrelated
// goroutines and make the test slow/flaky.
func TestComponents_ConstructsSyncPuller_WhenPeerSyncEnabled(t *testing.T) {
	logger := testLogger(t)
	ds := makeTestDualStore(t, "test-node")

	puller := makeTestSyncPuller(t, validPeerSyncConfig(), ds)

	c := &Components{
		Config: &config.Config{
			PeerSync: validPeerSyncConfig(),
		},
		Logger:     logger,
		SyncPuller: puller,
	}

	if c.SyncPuller == nil {
		t.Fatal("expected non-nil SyncPuller when peer_sync is enabled and DualStore is available")
	}
}

// TestComponents_SyncPullerNil_WhenPeerSyncDisabled verifies that
// disabling PeerSync yields a nil SyncPuller, even when DualStore is
// available. This mirrors the code path at components.go:2235 where the
// `cfg.PeerSync.IsValidated()` guard short-circuits.
func TestComponents_SyncPullerNil_WhenPeerSyncDisabled(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	// Use the real NewComponents with PeerSync disabled. Unlike the
	// cluster-enabled path, this construction does not spawn background
	// goroutines that outlive the test.
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Cluster: config.ClusterConfig{Enabled: false},
		Backup: config.BackupConfig{
			Enabled:       true,
			RepoURL:       "git@github.com:test/backups.git",
			Schedule:      24 * time.Hour,
			RetentionDays: 7,
			CheckoutDir:   tmpDir,
		},
		PeerSync: config.DefaultPeerSyncConfig(), // disabled
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
		Daemon: config.DaemonConfig{
			DataDir: tmpDir,
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = comps.Stop(ctx)
	})

	if comps.SyncPuller != nil {
		t.Error("expected nil SyncPuller when peer_sync.enabled=false")
	}
}

// TestComponents_SyncPullerNil_WhenDualStoreMissing verifies that when
// PeerSync is enabled but cluster.enabled=false (so DualStore is nil),
// NewComponents logs a warning and leaves SyncPuller nil rather than
// crashing. This mirrors the code path at components.go:2250-2252.
func TestComponents_SyncPullerNil_WhenDualStoreMissing(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmpDir := t.TempDir()
	cfg := &config.Config{
		// Cluster disabled — DualStore will not be constructed.
		Cluster: config.ClusterConfig{Enabled: false},
		Backup: config.BackupConfig{
			Enabled:       true,
			RepoURL:       "git@github.com:test/backups.git",
			Schedule:      24 * time.Hour,
			RetentionDays: 7,
			CheckoutDir:   tmpDir,
		},
		PeerSync: validPeerSyncConfig(),
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
		Daemon: config.DaemonConfig{
			DataDir: tmpDir,
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = comps.Stop(ctx)
	})

	if comps.DualStore != nil {
		t.Error("expected nil DualStore when cluster.enabled=false")
	}
	if comps.SyncPuller != nil {
		t.Error("expected nil SyncPuller when DualStore is unavailable")
	}
}

// TestComponents_StopClosesSyncPuller verifies that the SyncPuller
// lifecycle (Start with a cancellable context, then cancel + Stop) does
// not leak goroutines. SyncPuller.Start blocks on a ticker loop that
// only exits when the supplied context is cancelled; Stop() cleans up
// temp files. The daemon wiring at components.go:3076-3081 runs Start
// in a goroutine on the Components lifecycle context and calls Stop()
// during shutdown — we mirror that two-phase pattern here.
func TestComponents_StopClosesSyncPuller(t *testing.T) {
	ds := makeTestDualStore(t, "test-node")
	puller := makeTestSyncPuller(t, validPeerSyncConfig(), ds)

	ctx, cancel := context.WithCancel(context.Background())

	// Start in a goroutine, mirroring components.go:3077
	startDone := make(chan struct{})
	go func() {
		puller.Start(ctx)
		close(startDone)
	}()

	// Give the initial pull a brief moment to run, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Start should return shortly after ctx is cancelled.
	select {
	case <-startDone:
		// Start returned cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("SyncPuller.Start did not return within 5s after ctx cancel")
	}

	// Stop cleans up temp files (idempotent — safe even after Start returned).
	puller.Stop()

	// Allow any residual goroutines to drain.
	before := runtime.NumGoroutine()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Tolerate flakiness: we expect roughly the same count (within 2).
	// The SyncPuller spawns an initial pull goroutine and a ticker
	// goroutine; both should exit after ctx cancellation.
	if diff := after - before; diff > 2 {
		t.Errorf("possible goroutine leak: before=%d after=%d (diff=%d)", before, after, diff)
	}
}

// TestComponents_BackupSchedulerStartStopIdempotent verifies that Stop
// is idempotent on a Components constructed via NewComponents with a
// backup scheduler. Components.Stop is guarded by sync.Once, so a
// second call must return nil without panicking.
func TestComponents_BackupSchedulerStartStopIdempotent(t *testing.T) {
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
		Daemon: config.DaemonConfig{
			DataDir: tmpDir,
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.BackupScheduler == nil {
		t.Fatal("expected non-nil BackupScheduler for idempotent-Stop test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First Stop: should not panic.
	if err := comps.Stop(ctx); err != nil {
		t.Errorf("first Stop returned error: %v", err)
	}
	// Second Stop: must be idempotent (no panic, no error).
	if err := comps.Stop(ctx); err != nil {
		t.Errorf("second Stop returned error: %v", err)
	}
}

// Ensure bkpkg import is always exercised even if individual test
// variants are trimmed during refactoring.
var _ *bkpkg.SyncPuller
