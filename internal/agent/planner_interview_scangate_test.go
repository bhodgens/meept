package agent

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
	"modernc.org/sqlite"
)

// Fixwave 2026-11-27 bughunt A-6 (L2) pins: DeliverInterviewAnswer runs on
// EVERY chat turn before classification (handler.go), the waiter miss is
// the common case, and the ungated cold path paid a planning-task table
// scan plus a GetByID per row per ordinary turn. The fix gates the scan on
// coldInterviewArmed — armed by StoreInterviewState's successful persist,
// disarmed by ClearInterviewState's successful delete — and prunes the
// coldInterviewClaims entry when the resume is delivered.
//
// Store traffic is counted at the SQL DRIVER level: StrategicPlanner holds
// a concrete *task.Store (a struct double cannot intercept), so the pins
// register a wrapping driver whose connections forward to the production
// sqlite driver and count every tasks-table SELECT the real store issues.
// Production seams untouched; every counted query lands on production
// storage.

// gateCountDriver wraps the production sqlite driver, counting tasks-table
// SELECTs (the cold scan's List lane and the GetByID reads it fans out to).
type gateCountDriver struct {
	count *atomic.Int64
}

func (d *gateCountDriver) Open(name string) (driver.Conn, error) {
	inner, err := new(sqlite.Driver).Open(name)
	if err != nil {
		return nil, err
	}
	return &gateCountConn{inner: inner, count: d.count}, nil
}

// gateCountConn forwards to the real sqlite conn and counts queries via
// the legacy driver.Queryer seam modernc implements (Query carries the
// SQL text inline, so no Prepare interception is needed).
type gateCountConn struct {
	inner driver.Conn
	count *atomic.Int64
}

func (c *gateCountConn) Prepare(q string) (driver.Stmt, error) {
	return c.inner.Prepare(q)
}

func (c *gateCountConn) Close() error { return c.inner.Close() }

// Begin satisfies the legacy driver.Conn interface; database/sql routes
// through it only for transactions (the task store never opens one).
func (c *gateCountConn) Begin() (driver.Tx, error) {
	return c.inner.Begin()
}

// Query intercepts the legacy Queryer lane: database/sql prefers
// driver.Queryer over Prepare+Query, and this is where the task store's
// List/GetByID SELECTs flow. Count anything reading the tasks table.
func (c *gateCountConn) Query(q string, args []driver.Value) (driver.Rows, error) {
	if c.count != nil && strings.Contains(strings.ToUpper(q), "FROM TASKS") {
		c.count.Add(1)
	}
	qr, ok := c.inner.(driver.Queryer)
	if !ok {
		return nil, fmt.Errorf("scangate: sqlite conn is not a Queryer")
	}
	return qr.Query(q, args)
}

func (c *gateCountConn) Exec(cmd string, args []driver.Value) (driver.Result, error) {
	er, ok := c.inner.(driver.Execer)
	if !ok {
		return nil, fmt.Errorf("scangate: sqlite conn is not an Execer")
	}
	return er.Exec(cmd, args)
}

// newScanGatePlanner builds a StrategicPlanner over a REAL temp task store
// whose SQL passes through the counting driver, plus the SELECT counter.
func newScanGatePlanner(t *testing.T) (*StrategicPlanner, *atomic.Int64) {
	t.Helper()
	// coldInterviewArmed is package-level process state; sibling interview
	// pins also touch it. Reset after every pin in this file.
	t.Cleanup(func() { coldInterviewArmed.Store(0) })

	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"

	var count atomic.Int64
	drvName := "scangate-" + t.Name()
	sql.Register(drvName, &gateCountDriver{count: &count})
	db, err := sql.Open(drvName, dsn)
	if err != nil {
		t.Fatalf("open counted db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping counted db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := task.NewStoreFromDB(db, slogDiscardLogger())
	if err != nil {
		t.Fatalf("store from counted db: %v", err)
	}
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore: store,
		StepStore: store.StepStore(),
		Logger:    slogDiscardLogger(),
	})
	return sp, &count
}

// TestDeliverInterviewAnswer_NoInterviewSkipsStoreScan pins the gate's
// steady state: a turn with no pending interview performs NO store scan.
func TestDeliverInterviewAnswer_NoInterviewSkipsStoreScan(t *testing.T) {
	// Sibling interview pins arm the shared counter with ORPHANED
	// interviews that never resolve (the stale-fallthrough leg is one):
	// reset before asserting this pin's own steady state.
	coldInterviewArmed.Store(0)
	sp, count := newScanGatePlanner(t)
	tsk := newTestTask("no interview", "just chat with me")
	tsk.SetState(task.StatePlanning)
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if count.Load() != 0 {
		t.Fatalf("precondition broken: %d tasks-table SELECTs before the turn", count.Load())
	}
	for i := 0; i < 5; i++ {
		if _, delivered := sp.DeliverInterviewAnswer("conv-scan-none", "ordinary turn"); delivered {
			t.Fatal("ordinary turn routed to the interview seam")
		}
	}
	if got := count.Load(); got != 0 {
		t.Fatalf("no-interview turns made %d tasks-table SELECTs, want 0 (cold-scan gate disarmed)", got)
	}
}

// TestDeliverInterviewAnswer_ArmedInterviewStillScans pins the gate's
// other edge: once an interview wait is PERSISTED, the scan runs again —
// the fix must not break the L8 restart-miss recovery.
func TestDeliverInterviewAnswer_ArmedInterviewStillScans(t *testing.T) {
	sp, count := newScanGatePlanner(t)
	tsk := newTestTask("armed interview", "design and implement the archive service")
	tsk.SetState(task.StatePlanning)
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	sp.StoreInterviewState(tsk, PlanRequest{TaskID: tsk.ID, SessionID: "conv-scan-armed"},
		&plannerInterviewState{TaskID: tsk.ID, SessionID: "conv-scan-armed", Questions: []string{"which backend?"}})
	if got := count.Load(); got != 0 {
		t.Fatalf("StoreInterviewState must not itself scan (got %d tasks-table SELECTs)", got)
	}
	if got := coldInterviewArmed.Load(); got != 1 {
		t.Fatalf("successful persist left coldInterviewArmed = %d, want 1", got)
	}
	if _, delivered := sp.DeliverInterviewAnswer("conv-scan-armed", "sqlite"); !delivered {
		t.Fatal("armed interview answer fell through; gate broke the L8 recovery")
	}
	if got := count.Load(); got == 0 {
		t.Fatal("armed interview turn made 0 tasks-table SELECTs; the cold scan never ran")
	}
}

// TestDeliverInterviewAnswer_AfterClearResumesSkippingScan pins the
// disarm leg: after the interview resolves (ClearInterviewState via the
// hot consume), later no-interview turns are scan-free again.
func TestDeliverInterviewAnswer_AfterClearResumesSkippingScan(t *testing.T) {
	sp, count := newScanGatePlanner(t)
	tsk := newTestTask("resolved interview", "design the archive service")
	tsk.SetState(task.StatePlanning)
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	sp.StoreInterviewState(tsk, PlanRequest{TaskID: tsk.ID, SessionID: "conv-scan-clear"},
		&plannerInterviewState{TaskID: tsk.ID, SessionID: "conv-scan-clear", Questions: []string{"which backend?"}})
	// Hot waiter resolution: park then answer.
	sp.beginInterviewWait(tsk.ID, "conv-scan-clear", func([]string) {})
	if _, delivered := sp.DeliverInterviewAnswer("conv-scan-clear", "sqlite"); !delivered {
		t.Fatal("hot-path answer not accepted")
	}
	before := count.Load()
	for i := 0; i < 5; i++ {
		sp.DeliverInterviewAnswer("conv-scan-clear", "follow-up chat")
	}
	if got := count.Load(); got != before {
		t.Fatalf("post-resolution turns made %d additional tasks-table SELECTs, want 0 (gate disarmed after clear)", got-before)
	}
}

// TestColdInterviewClaimsPrunedAfterDelivery pins the pruning leg: the
// claim entry taken on a cold delivery is deleted once the resume is
// dispatched, instead of leaking for the process lifetime.
func TestColdInterviewClaimsPrunedAfterDelivery(t *testing.T) {
	sp, _ := newScanGatePlanner(t)
	tsk := newTestTask("claimed interview", "design the archive service")
	tsk.SetState(task.StatePlanning)
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	sp.StoreInterviewState(tsk, PlanRequest{TaskID: tsk.ID, SessionID: "conv-scan-claim"},
		&plannerInterviewState{TaskID: tsk.ID, SessionID: "conv-scan-claim", Questions: []string{"which backend?"}})
	if _, delivered := sp.DeliverInterviewAnswer("conv-scan-claim", "sqlite"); !delivered {
		t.Fatal("cold delivery not accepted")
	}
	if _, loaded := coldInterviewClaims.Load(tsk.ID); loaded {
		t.Fatal("coldInterviewClaims entry still present after delivery; claim leaked")
	}
}

// TestColdInterviewArmedCountsConcurrentInterviews pins the counter (not
// bool) shape: two concurrently parked interviews stay armed until BOTH
// blocks are cleared.
func TestColdInterviewArmedCountsConcurrentInterviews(t *testing.T) {
	sp, _ := newScanGatePlanner(t)
	pctx := &plan.PlanningContext{InterviewQuestions: []string{"which backend?"}}
	for _, sess := range []string{"conv-conc-a", "conv-conc-b"} {
		tsk := newTestTask("interview "+sess, "design the archive service")
		tsk.SetState(task.StatePlanning)
		if err := sp.taskStore.Create(tsk); err != nil {
			t.Fatalf("create task: %v", err)
		}
		if err := sp.awaitInterviewAnswers(context.Background(), tsk, PlanRequest{TaskID: tsk.ID, SessionID: sess}, pctx); err != nil {
			t.Fatalf("awaitInterviewAnswers(%s): %v", sess, err)
		}
	}
	if got := coldInterviewArmed.Load(); got != 2 {
		t.Fatalf("coldInterviewArmed = %d, want 2 (one per persisted interview)", got)
	}
	// Resolve the first: the gate stays armed for the second.
	if _, delivered := sp.DeliverInterviewAnswer("conv-conc-a", "sqlite"); !delivered {
		t.Fatal("first interview answer not accepted")
	}
	if got := coldInterviewArmed.Load(); got != 1 {
		t.Fatalf("after one resolve coldInterviewArmed = %d, want 1 (the other interview still waits)", got)
	}
	if _, delivered := sp.DeliverInterviewAnswer("conv-conc-b", "sqlite"); !delivered {
		t.Fatal("second interview answer not accepted")
	}
	if got := coldInterviewArmed.Load(); got != 0 {
		t.Fatalf("after both resolves coldInterviewArmed = %d, want 0", got)
	}
}
