package daemon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// newWiringTestComponents builds a minimal *Components suitable for
// wireAgentLoopManager tests. It mirrors the construction pattern in
// components_test.go without requiring the full NewComponents stack.
func newWiringTestComponents(t *testing.T) *Components {
	t.Helper()
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c
}

// TestWireAgentLoopManager_ConstructsManagerAndPool verifies that
// wiring with WorkerPool.Enabled=true sets both Components fields.
func TestWireAgentLoopManager_ConstructsManagerAndPool(t *testing.T) {
	c := newWiringTestComponents(t)

	cfg := &config.Config{
		Agent: config.AgentConfig{
			WorkerPool: config.AgentWorkerPoolConfig{
				Enabled: true,
			},
		},
	}

	wireAgentLoopManager(c, cfg, slog.Default())
	defer func() {
		if c.AgentWorkerPool != nil {
			c.AgentWorkerPool.Stop()
		}
	}()

	if c.AgentLoopManager == nil {
		t.Fatal("expected AgentLoopManager to be non-nil after wiring")
	}
	if c.AgentWorkerPool == nil {
		t.Fatal("expected AgentWorkerPool to be non-nil after wiring")
	}
}

// TestWireAgentLoopManager_DisabledByConfig verifies that wiring is a
// no-op when WorkerPool.Enabled=false.
func TestWireAgentLoopManager_DisabledByConfig(t *testing.T) {
	c := newWiringTestComponents(t)

	cfg := &config.Config{
		Agent: config.AgentConfig{
			WorkerPool: config.AgentWorkerPoolConfig{
				Enabled: false,
			},
		},
	}

	wireAgentLoopManager(c, cfg, slog.Default())

	// Manager should still be created for per-session project isolation,
	// even when the worker pool is disabled.
	if c.AgentLoopManager == nil {
		t.Error("expected AgentLoopManager to be created even when worker pool disabled")
	}
	if c.AgentWorkerPool != nil {
		t.Error("expected AgentWorkerPool to remain nil when disabled")
	}
}

// TestWireAgentLoopManager_GetOrCreateRoundtrip verifies the full
// manager roundtrip: create, identity, sessionID/workingDir getters,
// and List count.
func TestWireAgentLoopManager_GetOrCreateRoundtrip(t *testing.T) {
	c := newWiringTestComponents(t)

	cfg := &config.Config{
		Agent: config.AgentConfig{
			WorkerPool: config.AgentWorkerPoolConfig{
				Enabled: true,
			},
		},
	}

	wireAgentLoopManager(c, cfg, slog.Default())
	defer func() {
		if c.AgentWorkerPool != nil {
			c.AgentWorkerPool.Stop()
		}
	}()

	loop, err := c.AgentLoopManager.GetOrCreate("s1", "/tmp")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop")
	}
	if got := loop.GetSessionID(); got != "s1" {
		t.Errorf("GetSessionID() = %q, want %q", got, "s1")
	}
	if got := loop.GetWorkingDir(); got != "/tmp" {
		t.Errorf("GetWorkingDir() = %q, want %q", got, "/tmp")
	}

	// Second call with same sessionID returns the same pointer.
	loop2, err := c.AgentLoopManager.GetOrCreate("s1", "/tmp")
	if err != nil {
		t.Fatalf("second GetOrCreate failed: %v", err)
	}
	if loop2 != loop {
		t.Error("expected second GetOrCreate to return the same loop pointer")
	}

	// List should have exactly 1 entry.
	loops := c.AgentLoopManager.List()
	if len(loops) != 1 {
		t.Errorf("expected List() to have 1 entry, got %d", len(loops))
	}
}

// TestWireAgentLoopManager_GetOrCreateValidation verifies that empty
// sessionID or workingDir produce errors.
func TestWireAgentLoopManager_GetOrCreateValidation(t *testing.T) {
	c := newWiringTestComponents(t)

	cfg := &config.Config{
		Agent: config.AgentConfig{
			WorkerPool: config.AgentWorkerPoolConfig{
				Enabled: true,
			},
		},
	}

	wireAgentLoopManager(c, cfg, slog.Default())
	defer func() {
		if c.AgentWorkerPool != nil {
			c.AgentWorkerPool.Stop()
		}
	}()

	if _, err := c.AgentLoopManager.GetOrCreate("", "/tmp"); err == nil {
		t.Error("expected error for empty sessionID, got nil")
	}
	if _, err := c.AgentLoopManager.GetOrCreate("x", ""); err == nil {
		t.Error("expected error for empty workingDir, got nil")
	}
}

// TestWireAgentLoopManager_DefaultsFromZeroConfig verifies that wiring
// with Enabled=true but all other fields zero still produces a
// functional pool (defaults applied). We confirm by submitting a work
// item without error.
func TestWireAgentLoopManager_DefaultsFromZeroConfig(t *testing.T) {
	c := newWiringTestComponents(t)

	cfg := &config.Config{
		Agent: config.AgentConfig{
			WorkerPool: config.AgentWorkerPoolConfig{
				Enabled: true,
				// MaxWorkers, MaxLoopsPerWorker, IdleTimeout all zero
			},
		},
	}

	wireAgentLoopManager(c, cfg, slog.Default())
	defer func() {
		if c.AgentWorkerPool != nil {
			c.AgentWorkerPool.Stop()
		}
	}()

	if c.AgentWorkerPool == nil {
		t.Fatal("expected AgentWorkerPool to be non-nil")
	}

	// Create a loop via the manager and submit work — should not error.
	loop, err := c.AgentLoopManager.GetOrCreate("defaults-sess", "/tmp")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	item := WorkItem{
		Loop:           loop,
		Trigger:        TriggerUserMessage,
		Message:        "ping",
		ConversationID: "conv-defaults",
	}
	if err := c.AgentWorkerPool.Submit(item); err != nil {
		t.Fatalf("Submit failed on default-configured pool: %v", err)
	}
}
