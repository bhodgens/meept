package daemon

// Goal-gate human-approval wiring tests (audit HITL-1): the goal-kind
// branch of the checkpoint surface parks through the REAL GateParker +
// REAL SQLite park store (no doubles) and the manager seam drains it
// exactly once, mirroring the plan-gate tests in gate_park_wiring_test.go.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
)

// newGoalGateHarness builds components with a real bus, real park store,
// the gate parker wired (including the goal-kind seams), and a real
// employee manager carrying one goal loop.
func newGoalGateHarness(t *testing.T) (*Components, *bus.MessageBus, *employee.GoalLoop, *employee.GoalStore, func()) {
	t.Helper()
	logger := slog.Default()
	mb := bus.New(nil, logger)
	store, err := agent.NewSQLiteParkStore(filepath.Join(t.TempDir(), "parks.db"), logger)
	if err != nil {
		t.Fatalf("park store: %v", err)
	}
	goalStore, err := employee.NewGoalStore(filepath.Join(t.TempDir(), "goals.db"), logger)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	c := &Components{
		Logger:            logger,
		msgBus:            mb,
		ParkStore:         store,
		EmployeeGoalStore: goalStore,
	}
	c.wireGateParker()
	if c.GateParker == nil {
		t.Fatalf("gate parker not wired")
	}
	c.EmployeeManager = employee.NewManagerWithStores(nil, nil, nil, goalStore, nil, logger)

	ctx, cancel := context.WithCancel(context.Background())
	c.StartGateParkPump(ctx)

	loop := employee.NewGoalLoop("emp-gate-daemon", nil, goalStore, logger)
	c.EmployeeManager.RegisterGoalLoop("emp-gate-daemon", loop)

	cleanup := func() {
		cancel()
		c.stopGateParker()
		store.Close()
		goalStore.Close()
	}
	return c, mb, loop, goalStore, cleanup
}

// Park a goal gate through the loop's seam (as Reflect would), then drain
// it via the manager resume seam: exactly once, restart re-arm honored.
func TestGoalGateParkAndResumeThroughSharedParker(t *testing.T) {
	c, _, _, _, cleanup := newGoalGateHarness(t)
	defer cleanup()

	ctx := context.Background()
	// The manager-level resume path keys on the goal id + employee id
	// (the park row identity), so a synthetic goal id exercises the flow
	// without a goal-store row (no FK seed needed).
	const goalID = "goal-d1"

	// Park via the loop's seam (the path Reflect uses): a non-nil park
	// result means the goal kind landed in the shared parker. The reentry
	// payload mirrors parkGateApproval's shape (plan_id + employee_id).
	loop := c.EmployeeManager.GetGoalLoop("emp-gate-daemon")
	parked := loop.GoalGateParkCheckpoint(agent.GateParkData{
		GateKind: "goal",
		GateID:   goalID,
		TurnReentry: map[string]any{
			"plan_id":     "plan-d1",
			"employee_id": "emp-gate-daemon",
		},
		RequestedAt: timeNowUTC(),
	})
	if !parked {
		t.Fatalf("goal gate park refused")
	}
	if n := c.GateParker.PendingGates(); n != 1 {
		t.Fatalf("pending gates = %d, want 1 after goal park", n)
	}

	// Drain via the manager resume seam (the agents.goals.approve_gate
	// path). Exactly once.
	resumed, err := c.EmployeeManager.ResumeGoalGateParked(ctx, "emp-gate-daemon", goalID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resumed {
		t.Fatalf("goal gate did not resume")
	}
	if n := c.GateParker.PendingGates(); n != 0 {
		t.Fatalf("pending gates = %d, want 0 after approval", n)
	}
	// Duplicate approval is a no-op.
	resumed, err = c.EmployeeManager.ResumeGoalGateParked(ctx, "emp-gate-daemon", goalID)
	if err != nil {
		t.Fatalf("second resume: %v", err)
	}
	if resumed {
		t.Fatalf("duplicate approval resumed again")
	}
}

// Restart re-arm: park, stop the parker, rewire over the same store, drain
// late — the goal-kind row survives like a plan-kind row (T3 symmetry).
func TestGoalGateRestartReArm(t *testing.T) {
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
	if !c1.GateParker.ParkGate(agent.GateParkData{
		GateKind: "goal",
		GateID:   "goal-r1",
	}) {
		t.Fatalf("park refused")
	}
	c1.stopGateParker()

	c2 := &Components{Logger: logger, msgBus: mb, ParkStore: store}
	c2.wireGateParker()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	c2.StartGateParkPump(ctx2)
	if n := c2.GateParker.PendingGates(); n != 1 {
		t.Fatalf("re-armed gates = %d, want 1", n)
	}
	if !c2.GateParker.ResumeByKey(context.Background(), "goal", "goal-r1") {
		t.Fatalf("late goal-gate approval did not resume after restart")
	}
	if c2.GateParker.ResumeByKey(context.Background(), "goal", "goal-r1") {
		t.Fatalf("duplicate goal-gate approval resumed again")
	}
}

// Nil wiring degrades silently: no manager, no parker — the approval pump
// must not panic.
func TestGoalGateNilWiringNoop(t *testing.T) {
	logger := slog.Default()
	mb := bus.New(nil, logger)
	c := &Components{Logger: logger, msgBus: mb, ParkStore: nil}
	c.wireGateParker()
	ctx := t.Context()
	c.StartGateParkPump(ctx) // must not panic (gate parker nil → pump no-op)

	// Manager set but no park store: the resume seam reports an error
	// rather than a silent false.
	c2 := &Components{
		Logger:            logger,
		msgBus:            mb,
		ParkStore:         nil,
		EmployeeManager:   employee.NewManagerWithStores(nil, nil, nil, nil, nil, logger),
		EmployeeGoalStore: nil,
	}
	c2.StartGateParkPump(ctx)
	resumed, err := c2.EmployeeManager.ResumeGoalGateParked(ctx, "emp", "goal-x")
	if err == nil {
		t.Fatalf("resume without a wired probe must error")
	}
	if resumed {
		t.Fatalf("resume reported success without a parker")
	}
	_ = time.Now // keep time import if assertions change
}
