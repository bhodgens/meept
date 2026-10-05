# Worker Wake-Up - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Event-driven worker wake-up: queue notifies waiting workers on Enqueue; workers stop relying on the 1s poll.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Workers in internal/worker/worker.go poll `queue.Claim` on a 1s timer,
backing off to 15s when idle. Every step-to-step hop pays up to 1s of pure
polling latency. The bus already publishes `queue.enqueue` but workers
ignore it. This leaf adds a direct wake channel from `PersistentQueue.Enqueue`
to each worker's run loop: `Enqueue` sends a non-blocking signal to every
registered waiter; the worker's `select` gains the wake channel so it
claims immediately instead of waiting out the timer.

## Context

- `PersistentQueue` (internal/queue/queue.go) wraps a SQLite store. `Enqueue`
  (queue.go:147-168) inserts and publishes a bus event. `Claim`
  (queue.go:182-267) is called by workers.
- `Worker.run` (internal/worker/worker.go:165-215) is a loop with
  `time.After(waitTime)`; `tryProcessJob` (worker.go:217+) claims and
  processes one job.
- `Pool.AddWorker` (internal/worker/pool.go:169-194) constructs workers;
  the pool holds `p.queue queue.Queue` (the small interface).
- IMPORTANT: do NOT touch the Claim slow-path window logic (queue.go:204-241)
  — a parallel fixer owns it.

Key files:
- internal/queue/queue.go — add wake plumbing to PersistentQueue
- internal/worker/worker.go — add WakeCh to Config, select on it in run()
- internal/worker/pool.go — create + register the channel in AddWorker

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/queue/queue.go
// Optional interface the pool type-asserts on:
type WakeNotifier interface {
    WakeWaiter(ch chan<- struct{}) (unregister func())
}

// On *PersistentQueue:
//   unexported: wakeMu sync.Mutex; wakeWaiters map[chan<- struct{}]struct{}
//   func (q *PersistentQueue) WakeWaiter(ch chan<- struct{}) (unregister func())
//   Enqueue: after store.Insert succeeds, non-blocking send to all waiters.
//   Close: clears waiters.

// internal/worker/worker.go
// Config gains: WakeCh <-chan struct{} (nil = poll-only legacy).
// run()'s select gains: case <-w.wakeCh (guard nil channel — a nil channel
// in a select blocks forever, which is exactly the legacy behavior, so a
// nil WakeCh needs NO special-casing IF you store it as a nil channel field;
// verify this reasoning in a test).

// internal/worker/pool.go
// AddWorker: if wn, ok := p.queue.(queue.WakeNotifier); ok — create
// ch := make(chan struct{}, 1), unregister := wn.WakeWaiter(ch), pass ch
// into the worker Config, and arrange RemoveWorker/Stop to call unregister.
```

### What This Leaf Consumes

Nothing new; existing `queue.Queue` interface stays untouched (WakeNotifier
is a separate optional interface — do NOT add methods to `Queue`).

## Tasks

### Task 1: WakeNotifier on PersistentQueue

**Objective:** Enqueue signals registered waiters without blocking.

**Files:**
- Modify: internal/queue/queue.go (PersistentQueue struct, NewPersistentQueue, Enqueue, Close)
- Test: internal/queue/wakeup_test.go (NEW)

**Step 1: Write failing test**

```go
func TestPersistentQueue_WakeWaiter(t *testing.T) {
    // NewPersistentQueue on t.TempDir() db.
    ch := make(chan struct{}, 1)
    unregister := q.WakeWaiter(ch)
    job := mustJob(t)
    if err := q.Enqueue(context.Background(), job); err != nil { t.Fatal(err) }
    select {
    case <-ch:
    case <-time.After(100 * time.Millisecond):
        t.Fatal("Enqueue did not signal waiter")
    }
    unregister()
    _ = q.Enqueue(context.Background(), mustJob(t))
    select {
    case <-ch:
        t.Fatal("waiter signaled after unregister")
    default:
    }
}
```

**Step 2: Run** `go test ./internal/queue/ -run TestPersistentQueue_WakeWaiter -v` — expect FAIL (no WakeWaiter).

**Step 3: Implement** per the contract. The send loop must hold wakeMu only
to snapshot the waiter set, then send OUTSIDE the lock (mutexio analyzer:
no channel ops under mutex). Non-blocking send: `select { case ch <- struct{}{}: default: }`.

**Step 4: Run** — expect PASS.

### Task 2: Worker consumes the wake channel

**Objective:** An idle worker claims a job within milliseconds of Enqueue.

**Files:**
- Modify: internal/worker/worker.go (Config, Worker struct, NewWorker, run)
- Test: internal/worker/worker_wakeup_test.go (NEW)

**Step 1: Write failing test**

```go
func TestWorker_WakeClaimsImmediately(t *testing.T) {
    // Fake queue whose Claim returns ErrNoJobAvailable until armed.
    // Worker with pollInterval... construct via NewWorker(Config{WakeCh: ch, ...}).
    // Start worker. Arm the fake queue, THEN Enqueue-equivalent: send on ch.
    // Assert (via fake queue's claim log + timestamp) that Claim ran within
    // 50ms of the wake signal — far below the 1s poll floor.
}
func TestWorker_NilWakeChIsLegacy(t *testing.T) {
    // Worker with WakeCh nil runs its normal loop (select on nil channel
    // blocks forever = pure timer behavior). Assert Start/Stop cycle works
    // and no panic.
}
```

**Step 2: Run** — expect FAIL.

**Step 3: Implement.** Store `wakeCh <-chan struct{}` on Worker. In run(),
replace the fixed `time.After(waitTime)` select with:

```go
timer := time.NewTimer(waitTime)
select {
case <-ctx.Done():
    timer.Stop()
    return
case <-w.wakeCh: // nil channel: blocks forever — legacy timer path
    timer.Stop()
case <-timer.C:
}
```

On wake: skip the sleep and go straight to the next tryProcessJob.

**Step 4: Run** — expect PASS.

### Task 3: Pool wiring + unregister lifecycle

**Objective:** Pool-created workers get a registered channel; RemoveWorker and Stop unregister it (no goroutine/channel leak).

**Files:**
- Modify: internal/worker/pool.go (AddWorker, RemoveWorker, Stop)
- Test: internal/worker/pool_wakeup_test.go (NEW)

**Step 1: Write failing test** — pool over a fake queue implementing
WakeNotifier; AddWorker returns worker; assert fake queue has 1 registered
waiter; RemoveWorker asserts 0. A queue NOT implementing WakeNotifier must
also construct workers fine (type assert fails → nil channel).

**Step 2: Run** — expect FAIL. **Step 3: Implement.** Track the unregister
func per worker ID in a pool map guarded by p.mu; call it in RemoveWorker
and in Stop's cleanup. **Step 4: Run** — expect PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] WakeNotifier is a separate optional interface (Queue untouched)
- [ ] No channel send under mutex
- [ ] Nil WakeCh worker behaves byte-identically to legacy
- [ ] No unregister leak (test proves count returns to 0)
- [ ] No edits to Claim slow-path logic (queue.go:204-241)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task implemented, tests present and passing
- [ ] Contracts match exactly
- [ ] Project conventions followed (gofmt, no ignored errors)
- [ ] No scope creep

## Notes

- Existing worker tests use fake queues; follow their construction pattern
  (see internal/worker/worker_test.go) rather than inventing new fakes.
- The wake channel is size-1 and signals are dropped when full: a worker
  mid-job does not need signals; it will claim on its next loop anyway.
- `go test ./internal/worker/ -short` must stay green; do not weaken
  existing backoff tests — a wake must NOT reset idleBackoff growth
  (backoff still grows when genuinely idle with no enqueues).
