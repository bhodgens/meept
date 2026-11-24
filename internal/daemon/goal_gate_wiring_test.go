package daemon

// Goal-gate human-approval wiring tests (audit HITL-1): the goal-kind
// branch of the checkpoint surface parks through the REAL GateParker +
// REAL SQLite park store (no doubles) and the manager seam drains it
// exactly once, mirroring the plan-gate tests in gate_park_wiring_test.go.

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
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

// blockingReflector is a Reflector whose Chat blocks until released, so a
// test can observe WHEN the goal-gate resume re-entry actually runs.
type blockingReflector struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockingReflector() *blockingReflector {
	return &blockingReflector{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingReflector) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	b.calls.Add(1)
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return &llm.Response{Content: `{"health":"healthy","reasoning":"ok"}`}, nil
}

// TestGoalGatePumpDrainSerializesUnderInvokeMutex pins the bughunt
// serialization fix on the PUMP drain path: a goal-gate drain that does NOT
// ride Manager.ResumeGoalGateParked (a direct GateParker.ResumeByKey, the
// plan.approved pump / restart re-arm shape) must acquire the employee's
// per-employee invoke mutex before re-entering the loop — while a round
// holds the mutex, the drain BLOCKS instead of running a concurrent round.
//
// Failing revert: drop the AcquireInvokeMutexForResume lock in
// resumeGoalGateFromDrain — the re-entry runs immediately while the mutex
// is held and this test fails on the concurrent-entry check.
func TestGoalGatePumpDrainSerializesUnderInvokeMutex(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	mb := bus.New(nil, logger)
	store, err := agent.NewSQLiteParkStore(filepath.Join(t.TempDir(), "parks.db"), logger)
	if err != nil {
		t.Fatalf("park store: %v", err)
	}
	defer store.Close()
	goalStore, err := employee.NewGoalStore(filepath.Join(t.TempDir(), "goals.db"), logger)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	defer goalStore.Close()

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
	defer cancel()
	c.StartGateParkPump(ctx)

	const employeeID = "emp-pump-serial"
	reflector := newBlockingReflector()
	loop := employee.NewGoalLoop(employeeID, nil, goalStore, logger).
		WithReflector(reflector)
	c.EmployeeManager.RegisterGoalLoop(employeeID, loop)

	// Park a goal gate with a decodable re-entry (plan_id + employee_id +
	// a SUCCESSFUL result), mirroring parkGateApproval's TurnReentry shape:
	// the resumed Reflect reaches the reflector only on the success path,
	// which is what makes the blocking reflector observable.
	const goalID = "goal-pump-serial"
	parked := loop.GoalGateParkCheckpoint(agent.GateParkData{
		GateKind: "goal",
		GateID:   goalID,
		TurnReentry: map[string]any{
			"plan_id":     "plan-pump-serial",
			"employee_id": employeeID,
			"result":      map[string]any{"BotID": employeeID, "Success": true, "Output": "done"},
		},
		RequestedAt: timeNowUTC(),
	})
	if !parked {
		t.Fatalf("goal gate park refused")
	}

	// Hold the invoke mutex the way a trigger round does
	// (triggerViaGoalLoop holds it across Decide).
	invokeMu := c.EmployeeManager.AcquireInvokeMutexForResume(employeeID)
	invokeMu.Lock()

	drained := make(chan bool, 1)
	go func() {
		drained <- c.GateParker.ResumeByKey(context.Background(), "goal", goalID)
	}()

	// The parker invokes the resume callback synchronously (wg-wrapped
	// immediate call in agent resumeByKey), so the drain BLOCKS until the
	// queued re-entry finishes — which must not happen while the round
	// holds the mutex.
	select {
	case ok := <-drained:
		t.Fatalf("pump drain completed (resumed=%v) while the invoke mutex "+
			"was held — the re-entry ran concurrently with the round "+
			"(serialization regression)", ok)
	case <-reflector.entered:
		t.Fatal("resume re-entry ran while the invoke mutex was held " +
			"(concurrent rounds for one employee — serialization regression)")
	case <-time.After(200 * time.Millisecond):
		// Still waiting on the mutex: correct serialization.
	}

	// Release the round: the queued re-entry must now run (the callback is
	// still synchronous inside ResumeByKey, so the drain returns only once
	// the re-entry completes).
	invokeMu.Unlock()
	select {
	case <-reflector.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("queued resume re-entry never ran after the mutex released")
	}
	close(reflector.release)
	select {
	case ok := <-drained:
		if !ok {
			t.Fatalf("pump drain found no parked row")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drain never completed after the re-entry finished")
	}

	deadline := time.Now().Add(2 * time.Second)
	for reflector.calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := reflector.calls.Load(); n != 1 {
		t.Fatalf("resume re-entry ran %d times; want exactly 1 (at-most-once drain)", n)
	}
	if n := c.GateParker.PendingGates(); n != 0 {
		t.Fatalf("pending gates = %d, want 0 after the drain", n)
	}
}

// TestGoalGateManagerDrainNoSelfDeadlock pins the other half: the
// manager-routed RPC drain (ResumeGoalGateParked, which holds the invoke
// mutex across its resume probe) must NOT re-lock the same non-reentrant
// mutex inside the callback — the drain completes and the loop re-entry
// runs exactly once.
//
// Failing revert: remove the goalGateDrainLocked ctx marker (or the stamp
// in the resume probe) — the callback re-locks the held mutex and this
// test deadlocks/times out.
func TestGoalGateManagerDrainNoSelfDeadlock(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	mb := bus.New(nil, logger)
	store, err := agent.NewSQLiteParkStore(filepath.Join(t.TempDir(), "parks.db"), logger)
	if err != nil {
		t.Fatalf("park store: %v", err)
	}
	defer store.Close()
	goalStore, err := employee.NewGoalStore(filepath.Join(t.TempDir(), "goals.db"), logger)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	defer goalStore.Close()

	c := &Components{
		Logger:            logger,
		msgBus:            mb,
		ParkStore:         store,
		EmployeeGoalStore: goalStore,
	}
	c.wireGateParker()
	c.EmployeeManager = employee.NewManagerWithStores(nil, nil, nil, goalStore, nil, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.StartGateParkPump(ctx)

	const employeeID = "emp-mgr-nodeadlock"
	reflector := newBlockingReflector()
	close(reflector.release) // non-blocking: the re-entry may run immediately
	loop := employee.NewGoalLoop(employeeID, nil, goalStore, logger).
		WithReflector(reflector)
	c.EmployeeManager.RegisterGoalLoop(employeeID, loop)

	const goalID = "goal-mgr-nodeadlock"
	if !loop.GoalGateParkCheckpoint(agent.GateParkData{
		GateKind: "goal",
		GateID:   goalID,
		TurnReentry: map[string]any{
			"plan_id":     "plan-mgr-nodeadlock",
			"employee_id": employeeID,
			"result":      map[string]any{"BotID": employeeID, "Success": true, "Output": "done"},
		},
		RequestedAt: timeNowUTC(),
	}) {
		t.Fatalf("goal gate park refused")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resumed, err := c.EmployeeManager.ResumeGoalGateParked(ctx, employeeID, goalID)
		if err != nil {
			t.Errorf("manager drain: %v", err)
		}
		if !resumed {
			t.Error("manager drain reported resumed=false for a parked row")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("manager-routed drain self-deadlocked on the invoke mutex " +
			"(the callback re-locked a mutex the manager already holds)")
	}
	if n := reflector.calls.Load(); n != 1 {
		t.Fatalf("resume re-entry ran %d times; want exactly 1", n)
	}
}
