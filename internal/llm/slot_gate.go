package llm

import (
	"context"
	"sync"
)

// interactiveGrantsBeforeBackground is the starvation-guard budget:
// after this many consecutive interactive grants, one background waiter
// releases before further interactive grants (leaf Task 2: "3
// interactive → 1 background" bounded jump).
const interactiveGrantsBeforeBackground = 3

// slotGate is a two-lane counting semaphore for model-concurrency slots.
// It replaces the raw buffered-channel semaphore (tree 04 leaf 03,
// Outcome 2): a channel semaphore has no addressable wait list, so an
// interactive turn cannot jump waiters parked in the runtime's channel
// queue. slotGate keeps TWO FIFO lanes — background (lane 0) and
// interactive (lane 1) — and grants interactive waiters first, with a
// starvation guard that forces one background grant after every 3
// consecutive interactive grants.
//
// The per-waiter buffered grant channel is the condition broadcast: a
// release delivers exactly one token to the selected waiter's channel,
// which that waiter's select is parked on. No polling, no missed-wake
// window (deliveries happen under mu before the releasing goroutine
// unlocks).
//
// Semantics match the old channel semaphore:
//   - acquire blocks until a slot is granted or ctx is done (ctx-cancel
//     dequeues the waiter — it can never receive a later grant).
//   - release frees one slot (panics on unbalanced release, mirroring
//     the unbalanced-receive panic of the channel it replaces).
//   - uncontended acquire/release takes the lock once each and performs
//     zero heap allocations.
//
// Priority input is the CALLING TURN (chat=true, queue work=false), per
// the audit decision — DECISIONS.md D11's two-tier rule; the queue job's
// Interactive flag does not influence slot acquisition (intentional
// cross-layer divergence, documented in docs/workflows/llm-management.md).
type slotGate struct {
	mu    sync.Mutex
	lanes [2][]chan struct{} // lanes[0]=background FIFO, lanes[1]=interactive FIFO
	held  int                // currently granted slots
	cap   int                // total slots; normalized > 0
	guard int                // consecutive interactive grants since the last background grant
}

// newSlotGate builds a gate with the given slot count. A non-positive
// capacity is normalized to 1 defensively; production callers construct
// only when MaxConcurrency > 0, exactly as they gated make(chan, n).
func newSlotGate(capacity int) *slotGate {
	if capacity <= 0 {
		capacity = 1
	}
	return &slotGate{cap: capacity}
}

// acquire takes one slot, blocking until granted or ctx is done. Returns
// nil on grant; ctx.Err() on cancel (the waiter is dequeued and can
// never be woken by a later release).
func (g *slotGate) acquire(ctx context.Context, priority bool) error {
	// Fast path: free slot AND no waiters on either lane. Uncontended
	// acquire is a single lock round-trip and zero allocations.
	g.mu.Lock()
	if g.held < g.cap && len(g.lanes[0]) == 0 && len(g.lanes[1]) == 0 {
		g.held++
		g.mu.Unlock()
		return nil
	}

	// Slow path: queue on the priority lane, then park on our grant
	// token.
	grant := make(chan struct{}, 1)
	lane := 0 // background
	if priority {
		lane = 1
	}
	g.lanes[lane] = append(g.lanes[lane], grant)
	g.mu.Unlock()

	// context.Background().Done() returns nil; a nil done channel blocks
	// forever, which is exactly the no-cancel semantics.
	done := ctx.Done()
	select {
	case <-grant:
		return nil
	case <-done:
		// A release may have raced the cancel and already delivered our
		// token. Decide under the lock: token present → take the slot
		// (held was already incremented by the granter); still queued →
		// dequeue and fail.
		g.mu.Lock()
		select {
		case <-grant:
			g.mu.Unlock()
			return nil
		default:
			g.removeWaiterLocked(grant, lane)
			g.mu.Unlock()
			return ctx.Err()
		}
	}
}

// release returns one slot to the gate and wakes the next eligible
// waiter (interactive first; a background waiter is forced through after
// every 3 consecutive interactive grants). Panics on unbalanced release,
// mirroring the unbalanced-receive panic of the channel semaphore it
// replaces.
func (g *slotGate) release() {
	g.mu.Lock()
	if g.held == 0 {
		g.mu.Unlock()
		panic("llm: slotGate release without acquire")
	}
	g.held--
	g.grantNextLocked()
	g.mu.Unlock()
}

// grantNextLocked hands the freed slot to the next eligible waiter.
// Caller must hold mu. Selection: the interactive lane first, EXCEPT
// when the starvation guard has engaged (>= 3 consecutive interactive
// grants) and the background lane is non-empty — then the background
// lane goes next and resets the guard.
func (g *slotGate) grantNextLocked() {
	if g.held >= g.cap {
		return // no free slot (defensive; release just decremented)
	}
	var lane int
	switch {
	case len(g.lanes[1]) > 0 && len(g.lanes[0]) > 0:
		if g.guard >= interactiveGrantsBeforeBackground {
			lane = 0
		} else {
			lane = 1
		}
	case len(g.lanes[1]) > 0:
		lane = 1
	case len(g.lanes[0]) > 0:
		lane = 0
	default:
		return // no waiters: the slot stays free for the next acquire
	}

	grant := g.lanes[lane][0]
	g.lanes[lane] = g.lanes[lane][1:]
	g.held++
	if lane == 1 {
		g.guard++
	} else {
		g.guard = 0
	}
	grant <- struct{}{} // buffered; never blocks
}

// removeWaiterLocked dequeues a cancelled waiter. Caller must hold mu.
func (g *slotGate) removeWaiterLocked(grant chan struct{}, lane int) {
	q := g.lanes[lane]
	for i, w := range q {
		if w == grant {
			g.lanes[lane] = append(q[:i], q[i+1:]...)
			return
		}
	}
}

// heldCount returns the number of currently granted slots (test hook).
func (g *slotGate) heldCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held
}

// waiterCount returns the number of queued waiters across both lanes
// (test hook).
func (g *slotGate) waiterCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.lanes[0]) + len(g.lanes[1])
}
