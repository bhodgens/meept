package daemon

// Gate checkpoint wiring tests (hitl-notifications leaf 02): the daemon
// seam parks on plan.submitting, resumes exactly once on plan.approved,
// and tolerates nil wiring. All through the REAL TurnParker + REAL
// SQLite park store -- no hand-built doubles (leaf review checklist).

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// newGateWiringHarness builds components with a real bus, real park
// store, and the gate parker wired, plus the subscriptions armed.
func newGateWiringHarness(t *testing.T) (*Components, *bus.MessageBus, func()) {
	t.Helper()
	logger := slog.Default()
	mb := bus.New(nil, logger)
	store, err := agent.NewSQLiteParkStore(filepath.Join(t.TempDir(), "parks.db"), logger)
	if err != nil {
		t.Fatalf("park store: %v", err)
	}
	c := &Components{
		Logger:    logger,
		msgBus:    mb,
		ParkStore: store,
	}
	c.wireGateParker()
	if c.GateParker == nil {
		t.Fatalf("gate parker not wired")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.StartGateParkPump(ctx)
	cleanup := func() {
		cancel()
		c.stopGateParker()
		store.Close()
	}
	return c, mb, cleanup
}

func gateBusMessage(topic string, payload map[string]any) *models.BusMessage {
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
	if err != nil {
		panic(err)
	}
	msg.Topic = topic
	return msg
}

// Park on plan.submitting (session-bearing plan), resume exactly once on
// plan.approved, second approval is a no-op.
func TestGateWiringParkOnSubmitResumeOnApprove(t *testing.T) {
	c, mb, cleanup := newGateWiringHarness(t)
	defer cleanup()

	mb.Publish("plan.submitting", gateBusMessage("plan.submitting", map[string]any{
		"plan_id":    "plan-w1",
		"title":      "ship it",
		"session_id": "session-w1",
	}))
	// Give the pump a moment to process.
	waitForGateRows(t, c, 1)

	if n := c.GateParker.PendingGates(); n != 1 {
		t.Fatalf("pending gates = %d, want 1 after submit", n)
	}

	mb.Publish("plan.approved", gateBusMessage("plan.approved", map[string]any{
		"plan_id": "plan-w1",
	}))
	waitForGateRows(t, c, 0)
	time.Sleep(50 * time.Millisecond)

	// Resume drained the row; the pump log is the observable. Assert via
	// the parker: nothing pending, and a second approval is a no-op.
	if c.GateParker.ResumeByKey(context.Background(), "plan", "plan-w1") {
		t.Fatalf("second approval resumed again")
	}
}

// Session-less sink plans (evolver) never park a gate.
func TestGateWiringSkipsSessionlessPlans(t *testing.T) {
	c, mb, cleanup := newGateWiringHarness(t)
	defer cleanup()

	mb.Publish("plan.submitting", gateBusMessage("plan.submitting", map[string]any{
		"plan_id":    "plan-sink",
		"title":      "evolver plan",
		"session_id": "",
	}))
	time.Sleep(100 * time.Millisecond)
	if n := c.GateParker.PendingGates(); n != 0 {
		t.Fatalf("session-less plan parked a gate (%d pending)", n)
	}
}

// Restart re-arm: park, stop, rewire (fresh parker over the same store,
// Start), approve late -- resumes exactly once.
func TestGateWiringRestartReArmLateApproval(t *testing.T) {
	logger := slog.Default()
	mb := bus.New(nil, logger)
	dbPath := filepath.Join(t.TempDir(), "parks.db")
	store, err := agent.NewSQLiteParkStore(dbPath, logger)
	if err != nil {
		t.Fatalf("park store: %v", err)
	}
	defer store.Close()

	c1 := &Components{Logger: logger, msgBus: mb, ParkStore: store}
	c1.wireGateParker()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	c1.StartGateParkPump(ctx1)

	mb.Publish("plan.submitting", gateBusMessage("plan.submitting", map[string]any{
		"plan_id":    "plan-r1",
		"title":      "restart me",
		"session_id": "session-r1",
	}))
	waitForGateRows(t, c1, 1)
	// "Restart": stop the first parker; the row survives in the store.
	c1.stopGateParker()

	// Fresh components over the same store: re-arm via Start.
	c2 := &Components{Logger: logger, msgBus: mb, ParkStore: store}
	c2.wireGateParker()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	c2.StartGateParkPump(ctx2)
	if n := c2.GateParker.PendingGates(); n != 1 {
		t.Fatalf("re-armed gates = %d, want 1", n)
	}

	// Late approval after the restart.
	if !c2.GateParker.ResumeByKey(context.Background(), "plan", "plan-r1") {
		t.Fatalf("late approval did not resume after restart")
	}
	if c2.GateParker.ResumeByKey(context.Background(), "plan", "plan-r1") {
		t.Fatalf("duplicate approval resumed again")
	}
}

// Nil wiring degrades silently: no parker, no panic.
func TestGateWiringNilParkerNoop(t *testing.T) {
	logger := slog.Default()
	mb := bus.New(nil, logger)
	c := &Components{Logger: logger, msgBus: mb, ParkStore: nil}
	c.wireGateParker()
	if c.GateParker != nil {
		t.Fatalf("gate parker wired without a park store")
	}
	// Both handlers + pump are no-ops on nil wiring.
	c.handleGateSubmit(gateBusMessage("plan.submitting", map[string]any{
		"plan_id": "p", "session_id": "s",
	}))
	c.handleGateApproved(gateBusMessage("plan.approved", map[string]any{
		"plan_id": "p",
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.StartGateParkPump(ctx) // must not panic
}

func waitForGateRows(t *testing.T, c *Components, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.GateParker.PendingGates() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("gate rows did not reach %d within 2s", want)
}
