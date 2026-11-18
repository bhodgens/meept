package agent

// Dual-backend restart parity + flag-off identity (tree 20261110-parked-turns-as-jobs,
// leaf 03). TESTS ONLY.
//
// These ports pin that the tree-20261109 C1 guarantee (restart re-arm is
// owner-partitioned, at-most-once per owner, delete-before-callback) holds
// on the QUEUE backend, and that the flag-off path (no SetBackend) never
// touches the jobs table at all:
//
//   1. TestRestartRearmSingleOwnerQueueBackend — the C1-killer restart test
//      (TestRestartRearmSingleOwner, park_owner_gate_test.go) ported to
//      queueParkBackend: quota-watcher + throttle owners persist one chat
//      row each over the SAME queue.Store file; a simulated restart (close
//      the store, fresh parkers + fresh backends over the reopened file)
//      re-arms each owner exactly its own row, each resume fires exactly
//      once, and the parked_turn row is gone from jobs BEFORE the resume
//      callback runs (at-most-once delete-before-callback).
//   2. TestQueueBackendDisabledByteIdentical — a parker WITHOUT SetBackend
//      never creates a 'parked_turn' job: after a full park cycle the jobs
//      table is empty and the sqlite park store holds the rows (default
//      path untouched).
//   3. TestQueueBackendNoWorkerClaim — a parked_turn row saved through the
//      backend is NEVER returned by ClaimNextForAgent even when its
//      next_retry_at is in the past: the far-future due_at sentinel keeps
//      parks out of worker hands (every claim path filters
//      due_at IS NULL OR due_at <= now).
//
// Rig reuse (no forks): quotaGateRecord/throttleGateRecord
// (park_owner_gate_test.go), newTestParkStore/newParkStoreTestLogger/
// persistedRowCount (park_store_sqlite_test.go), gateQueueDB
// (park_queue_gate_test.go), ParkOwner strings via SetPersistenceOwner.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/queue"
)

// parityRecorder counts resume callbacks per owner and captures the number
// of that owner's live parked_turn rows AT CALLBACK TIME (delete-before-
// callback: at-most-once resume means the row must already be gone).
type parityRecorder struct {
	mu         sync.Mutex
	count      int
	last       ParkedTurnRecord
	rowsAtCall int
}

func (r *parityRecorder) callback(live *atomic.Pointer[queue.Store], claimedBy string) func(context.Context, ParkedTurnRecord) {
	return func(ctx context.Context, rec ParkedTurnRecord) {
		rows := -1
		if q := live.Load(); q != nil {
			var n int
			if err := q.DB().QueryRowContext(ctx,
				`SELECT COUNT(*) FROM jobs WHERE type = 'parked_turn' AND claimed_by = ?`, claimedBy,
			).Scan(&n); err == nil {
				rows = n
			}
		}
		r.mu.Lock()
		r.count++
		r.last = rec
		r.rowsAtCall = rows
		r.mu.Unlock()
	}
}

func (r *parityRecorder) snapshot() (count, rowsAtCall int, last ParkedTurnRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count, r.rowsAtCall, r.last
}

// --- 1. C1 killer, queue-backend restart shape ------------------------------

func TestRestartRearmSingleOwnerQueueBackend(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	qPath := filepath.Join(dir, "parkqueue.db")

	base := time.Now()
	// Wake 2s after the run-1 clock base: re-arm keeps the rows (the run-2
	// parkers' backend nowFn stays pinned at base, so resume_at > now), and
	// the backend-mode drain fires the resume once the REAL clock passes
	// the wake (C-A1 pin: backend drains read time.Now, never the injected
	// fake). The 2s margin keeps the post-Start Pending() re-arm
	// assertions deterministic even under -race run overhead.
	resumeAt := base.Add(2 * time.Second)

	// A recording backend must be a *QueueParkBackend to reach the nowFn
	// knob; the constructor returns the interface (gate-pinned shape), so
	// assert down for the test-seam field.
	quotaLive := atomic.Pointer[queue.Store]{}
	throttleLive := atomic.Pointer[queue.Store]{}
	var quotaRec, throttleRec parityRecorder

	// Run 1: two parkers with distinct owners persist one row each through
	// their queueParkBackend over the shared queue file. Parkers are not
	// started — the rows must survive on disk untouched.
	q1, err := queue.NewStore(qPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("queue.NewStore (run 1): %v", err)
	}
	store1 := newTestParkStore(t) // key-derivation plumbing; must stay empty
	defer store1.Close()

	pq1 := NewTurnParker(newParkStoreTestLogger(), quotaRec.callback(&quotaLive, "park:quota-watcher"), 2*time.Hour)
	pq1.SetParkPersistence(store1, ParkKindChat)
	pq1.SetPersistenceOwner("quota-watcher")
	pq1.nowFunc = func() time.Time { return base }
	pq1.SetBackend(newQueueParkBackend(q1, ParkKindChat, "quota-watcher"))
	if !pq1.Park(quotaGateRecord("s-q1", resumeAt)) {
		t.Fatal("run 1 quota park refused")
	}

	pt1 := NewTurnParker(newParkStoreTestLogger(), throttleRec.callback(&throttleLive, "park:throttle"), 2*time.Hour)
	pt1.SetParkPersistence(store1, ParkKindChat)
	pt1.SetPersistenceOwner("throttle")
	pt1.nowFunc = func() time.Time { return base }
	pt1.SetBackend(newQueueParkBackend(q1, ParkKindChat, "throttle"))
	if !pt1.Park(throttleGateRecord("s-t1", resumeAt)) {
		t.Fatal("run 1 throttle park refused")
	}

	var persisted int
	if err := q1.DB().QueryRow(`SELECT COUNT(*) FROM jobs WHERE type = 'parked_turn'`).Scan(&persisted); err != nil {
		t.Fatalf("count parked_turn rows (run 1): %v", err)
	}
	if persisted != 2 {
		t.Fatalf("parked_turn jobs after run 1 = %d, want 2", persisted)
	}
	if n := store1.persistedRowCount(t, ParkKindChat); n != 0 {
		t.Fatalf("sqlite park store rows under the queue backend = %d, want 0 (backend replaces the store path)", n)
	}

	// Simulated restart: close the queue store, reopen the same file,
	// build FRESH parkers + FRESH backends over it.
	if err := q1.Close(); err != nil {
		t.Fatalf("close queue store (restart): %v", err)
	}

	q2, err := queue.NewStore(qPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("queue.NewStore (run 2): %v", err)
	}
	defer q2.Close()
	quotaLive.Store(q2)
	throttleLive.Store(q2)
	store2 := newTestParkStore(t) // fresh plumbing store; stays empty
	defer store2.Close()

	dueQ := NewTurnParker(newParkStoreTestLogger(), quotaRec.callback(&quotaLive, "park:quota-watcher"), 2*time.Hour)
	dueQ.SetPollInterval(10 * time.Millisecond)
	dueQ.SetParkPersistence(store2, ParkKindChat)
	dueQ.SetPersistenceOwner("quota-watcher")
	dueQ.nowFunc = func() time.Time { return base }
	bq := newQueueParkBackend(q2, ParkKindChat, "quota-watcher")
	bq.(*QueueParkBackend).nowFn = func() time.Time { return base } // re-arm clock knob
	dueQ.SetBackend(bq)

	dueT := NewTurnParker(newParkStoreTestLogger(), throttleRec.callback(&throttleLive, "park:throttle"), 2*time.Hour)
	dueT.SetPollInterval(10 * time.Millisecond)
	dueT.SetParkPersistence(store2, ParkKindChat)
	dueT.SetPersistenceOwner("throttle")
	dueT.nowFunc = func() time.Time { return base }
	bt := newQueueParkBackend(q2, ParkKindChat, "throttle")
	bt.(*QueueParkBackend).nowFn = func() time.Time { return base }
	dueT.SetBackend(bt)

	dueQ.Start(ctx)
	defer dueQ.Stop()
	dueT.Start(ctx)
	defer dueT.Stop()

	// Restart re-arm partitions by owner: each parker holds exactly its
	// own row (the C1 cross-feed would give each parker both rows).
	if got := dueQ.Pending(); got != 1 {
		t.Errorf("restart re-arm: quota parker holds %d rows, want 1 (its own only)", got)
	}
	if got := dueT.Pending(); got != 1 {
		t.Errorf("restart re-arm: throttle parker holds %d rows, want 1 (its own only)", got)
	}

	// Backend-mode drain runs on the REAL clock (C-A1 pin), so the rows go
	// due ~2s after base without any fake-clock advance.
	waitUntil(t, 6*time.Second, func() bool {
		qc, _, _ := quotaRec.snapshot()
		tc, _, _ := throttleRec.snapshot()
		return qc >= 1 && tc >= 1
	})
	time.Sleep(150 * time.Millisecond) // let any (wrong) cross- or double-resume surface

	qc, qRows, qLast := quotaRec.snapshot()
	tc, tRows, tLast := throttleRec.snapshot()
	if qc != 1 {
		t.Errorf("quota-watcher resume callbacks = %d, want exactly 1 (its own row; C1 cross-feed = 2)", qc)
	}
	if tc != 1 {
		t.Errorf("throttle resume callbacks = %d, want exactly 1 (its own row; C1 cross-feed = 2)", tc)
	}
	if qLast.SessionID != "s-q1" {
		t.Errorf("quota-watcher resumed %+v, want the s-q1 record", qLast)
	}
	if tLast.SessionID != "s-t1" {
		t.Errorf("throttle resumed %+v, want the s-t1 record", tLast)
	}

	// Delete-before-callback: the owner's parked_turn row must already be
	// GONE at the instant its resume callback ran.
	if qRows != 0 {
		t.Errorf("quota parked_turn rows at callback time = %d, want 0 (delete-before-callback / at-most-once resume)", qRows)
	}
	if tRows != 0 {
		t.Errorf("throttle parked_turn rows at callback time = %d, want 0 (delete-before-callback / at-most-once resume)", tRows)
	}

	// No resurrection: both rows terminal after their drains, and the
	// plumbing store never saw a write.
	var left int
	if err := q2.DB().QueryRow(`SELECT COUNT(*) FROM jobs WHERE type = 'parked_turn'`).Scan(&left); err != nil {
		t.Fatalf("count parked_turn rows after drains: %v", err)
	}
	if left != 0 {
		t.Errorf("parked_turn rows after both drains = %d, want 0", left)
	}
	if n := store2.persistedRowCount(t, ParkKindChat); n != 0 {
		t.Errorf("sqlite park store rows under the queue backend (run 2) = %d, want 0", n)
	}
}

// --- 2. Flag-off identity: no backend wired => zero parked_turn jobs --------

// A parker WITHOUT SetBackend keeps the pre-seam sqlite path byte-
// identically: parks persist to the park store and NEVER touch the jobs
// table (no 'parked_turn' rows exist at all). Proves the default path is
// untouched by the queue backend landing.
func TestQueueBackendDisabledByteIdentical(t *testing.T) {
	q := gateQueueDB(t)
	store := newTestParkStore(t)
	defer store.Close()

	var resumed parityRecorder
	p := NewTurnParker(newParkStoreTestLogger(), resumed.callback(&atomic.Pointer[queue.Store]{}, ""), 2*time.Hour)
	p.SetParkPersistence(store, ParkKindChat)
	p.SetPersistenceOwner("parity-disabled")
	// Deliberately NO SetBackend call — the flag-off shape.

	if !p.Park(quotaGateRecord("s-disabled-q", time.Now().Add(time.Hour))) {
		t.Fatal("quota park refused (default path)")
	}
	if !p.Park(throttleGateRecord("s-disabled-t", time.Now().Add(time.Hour))) {
		t.Fatal("throttle park refused (default path)")
	}

	// Default path ran: rows landed in the sqlite park store...
	if n := store.persistedRowCount(t, ParkKindChat); n != 2 {
		t.Fatalf("store rows after default-path parks = %d, want 2", n)
	}

	// ...and the queue saw NOTHING at all.
	var parks int
	if err := q.DB().QueryRow(`SELECT COUNT(*) FROM jobs WHERE type = 'parked_turn'`).Scan(&parks); err != nil {
		t.Fatalf("count parked_turn jobs: %v", err)
	}
	if parks != 0 {
		t.Errorf("parked_turn jobs after flag-off park cycle = %d, want 0 (default path must not create queue rows)", parks)
	}
	var total int
	if err := q.DB().QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&total); err != nil {
		t.Fatalf("count all jobs: %v", err)
	}
	if total != 0 {
		t.Errorf("total jobs rows after flag-off park cycle = %d, want 0 (jobs table untouched)", total)
	}
}

// --- 3. Worker claim never sees a parked_turn row ----------------------------

// The due_at sentinel (parkedTurnSentinelDue, year 2999) keeps parked rows
// structurally unclaimable: every claim path filters
// `due_at IS NULL OR due_at <= now`, so a parked_turn job with next_retry_at
// long past is still NEVER returned by ClaimNextForAgent and stays pending
// under its park owner.
func TestQueueBackendNoWorkerClaim(t *testing.T) {
	ctx := context.Background()
	q := gateQueueDB(t)
	b := NewQueueParkBackend(q, ParkKindChat, "quota-watcher")

	// Save through the backend with a resume time in the PAST — the wake
	// has passed (next_retry_at <= now), so the ONLY thing keeping this
	// row out of worker hands is the due_at sentinel.
	rec := quotaGateRecord("s-noclaim", time.Now().Add(-time.Hour))
	rec.Owner = "quota-watcher"
	if err := b.Save(ctx, rec); err != nil {
		t.Fatalf("backend Save: %v", err)
	}

	// Row is shaped as expected: pending, owner-stamped, retry time past.
	var (
		state     string
		claimedBy string
		dueAt     string
		nextRetry string
	)
	if err := q.DB().QueryRow(
		`SELECT state, claimed_by, due_at, next_retry_at FROM jobs WHERE type = 'parked_turn'`,
	).Scan(&state, &claimedBy, &dueAt, &nextRetry); err != nil {
		t.Fatalf("query parked_turn row: %v", err)
	}
	if state != "pending" {
		t.Errorf("job state = %q, want pending", state)
	}
	if claimedBy != "park:quota-watcher" {
		t.Errorf("claimed_by = %q, want park:quota-watcher (owner stamp)", claimedBy)
	}
	if dueAt != parkedTurnSentinelDue {
		t.Errorf("due_at = %q, want the unclaimable sentinel %q", dueAt, parkedTurnSentinelDue)
	}
	wake, perr := time.Parse(time.RFC3339, nextRetry)
	if perr != nil {
		t.Fatalf("parse next_retry_at %q: %v", nextRetry, perr)
	}
	if !wake.Before(time.Now()) {
		t.Fatalf("test setup: next_retry_at %v must be in the past (due-looking to a worker)", wake)
	}

	// Worker claim (empty agentID = the pool's generic worker, matches the
	// most permissive claim filter) must NOT return the parked row.
	if job, err := q.ClaimNextForAgent("worker-1", nil, ""); err == nil {
		t.Fatalf("ClaimNextForAgent returned job %+v, want ErrNoJobAvailable (parked_turn rows are unclaimable)", job)
	} else if !errors.Is(err, queue.ErrNoJobAvailable) {
		t.Errorf("ClaimNextForAgent err = %v, want %v", err, queue.ErrNoJobAvailable)
	}

	// The row is untouched by the claim attempt: still pending, still
	// owned by the park owner, never claimed by the worker.
	var state2, claimedBy2 string
	if err := q.DB().QueryRow(
		`SELECT state, claimed_by FROM jobs WHERE type = 'parked_turn'`,
	).Scan(&state2, &claimedBy2); err != nil {
		t.Fatalf("re-query parked_turn row: %v", err)
	}
	if state2 != "pending" || claimedBy2 != "park:quota-watcher" {
		t.Errorf("after worker claim: state=%q claimed_by=%q, want pending/park:quota-watcher (sentinel keeps parks out of worker hands)", state2, claimedBy2)
	}
}
