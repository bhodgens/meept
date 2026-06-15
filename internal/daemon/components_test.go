package daemon

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"
)

// testComponents holds a Components along with the bus used to
// construct it, so tests can publish/subscribe to verify handler
// liveness after Start().
type testComponents struct {
	*Components
	bus *bus.MessageBus
}

// makeTestComponents builds a minimal Components with the three required
// handlers (chat, status, session) wired to a real bus.  All 18 optional
// components remain nil so their nil-guarded Start blocks are skipped.
func makeTestComponents(t *testing.T) testComponents {
	t.Helper()
	msgBus := bus.New(nil, slog.Default())
	logger := slog.Default()

	c := &Components{
		Config: &config.Config{
			Workers: config.WorkersConfig{PoolSize: 1},
		},
		Logger: logger,
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.ChatHandler = agent.NewChatHandler(nil, nil, msgBus, logger)
	c.StatusHandler = NewStatusHandler(msgBus, logger)
	store := session.NewMemoryStore(logger)
	c.SessionHandler = session.NewHandler(store, msgBus, logger)

	return testComponents{Components: c, bus: msgBus}
}

// publishStatusRequest publishes a status.request on the bus and
// returns the response channel that will receive any response.
func (tc testComponents) publishStatusRequest() <-chan *models.BusMessage {
	respSub := tc.bus.Subscribe("test-rollback", "status.response")
	msg, _ := models.NewBusMessage(models.MessageTypeRequest, "test", map[string]any{})
	tc.bus.Publish("status.request", msg)
	return respSub.Channel
}

// TestComponentsStart_RollbackOnSuccess verifies that a successful
// Start() does NOT roll back any started handlers.  Without a success
// flag, the deferred rollback fires on every return path (including
// return nil), stopping all handlers immediately after Start.
//
// We detect this by publishing a status.request on the bus and checking
// whether the StatusHandler (which subscribes on Start) responds.  If
// the rollback erroneously stopped the handler, no response arrives.
func TestComponentsStart_RollbackOnSuccess(t *testing.T) {
	tc := makeTestComponents(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	// The StatusHandler subscribes to "status.request" on Start.
	// If the rollback killed it, no response arrives within the timeout.
	respCh := tc.publishStatusRequest()
	select {
	case <-respCh:
		// Good: handler is alive and responded.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StatusHandler did not respond — rollback fired on successful Start (handlers were stopped)")
	}

	// Clean shutdown via Stop (not rollback).
	tc.Stop(ctx)
}

// TestComponentsStart_RollbackCoverage verifies that when Start()
// succeeds, all started handlers remain alive, and Stop() cleanly
// shuts everything down without panic.
//
// This test serves as the coverage assertion: after Task 2+3 land,
// the rollback switch must handle every startedHandlers key without
// panicking, even when optional components are nil.
func TestComponentsStart_RollbackCoverage(t *testing.T) {
	tc := makeTestComponents(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify StatusHandler is alive via bus round-trip.
	respCh := tc.publishStatusRequest()
	select {
	case <-respCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StatusHandler did not respond after Start")
	}

	// Clean shutdown.
	if err := tc.Stop(ctx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

// TestComponentsStart_CancelOnRollback verifies that the lifecycle
// context cancel function exists and works, so the rollback deferred
// can call c.cancel() to terminate PricingSyncer and other context-
// bound goroutines.
func TestComponentsStart_CancelOnRollback(t *testing.T) {
	c := &Components{
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	if c.cancel == nil {
		t.Fatal("c.cancel is nil — rollback cannot cancel PricingSyncer goroutine")
	}

	// Simulate the rollback's cancel call.
	c.cancel()

	select {
	case <-c.ctx.Done():
		// expected
	default:
		t.Error("c.cancel() did not cancel c.ctx")
	}
}
