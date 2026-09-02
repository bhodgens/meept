package llm

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestSlotGateGrantUpToCap verifies that cap concurrent acquires are
// granted immediately (leaf Task 2: "grant up to cap").
func TestSlotGateGrantUpToCap(t *testing.T) {
	g := newSlotGate(3)
	for range 3 {
		if err := g.acquire(context.Background(), false); err != nil {
			t.Fatalf("acquire: %v", err)
		}
	}
	if held := g.heldCount(); held != 3 {
		t.Fatalf("held = %d, want 3", held)
	}
}

// TestSlotGateBlocksAtCap verifies the cap N+1 blocks until a release.
func TestSlotGateBlocksAtCap(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	blocked := make(chan error, 1)
	go func() { blocked <- g.acquire(context.Background(), false) }()

	select {
	case err := <-blocked:
		t.Fatalf("second acquire granted at cap, err=%v", err)
	case <-time.After(50 * time.Millisecond):
		// Still blocked — correct.
	}

	g.release()
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("waiter acquire: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter not granted after release")
	}
}

// TestSlotGateReleaseWakesOne verifies release wakes exactly ONE waiter,
// leaving the gate at cap (the grant is consumed by the woken waiter).
func TestSlotGateReleaseWakesOne(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	const waiters = 3
	granted := make(chan struct{}, waiters)
	for range waiters {
		go func() {
			if g.acquire(context.Background(), false) == nil {
				granted <- struct{}{}
			}
		}()
	}
	time.Sleep(50 * time.Millisecond) // let all waiters enqueue

	g.release()
	time.Sleep(50 * time.Millisecond)

	select {
	case <-granted:
	default:
		t.Fatal("no waiter woken by release")
	}
	if held := g.heldCount(); held != 1 {
		t.Fatalf("held = %d, want 1 (exactly one grant outstanding)", held)
	}
	if n := g.waiterCount(); n != waiters-1 {
		t.Fatalf("waiters = %d, want %d", n, waiters-1)
	}
}

// TestSlotGateInteractiveJumpsQueue is the core fairness test: an
// interactive waiter arriving AFTER background waiters is granted first.
func TestSlotGateInteractiveJumpsQueue(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	order := make(chan string, 3)
	wg := sync.WaitGroup{}
	// Background waiters first, enqueued STRICTLY in order (serialize
	// with sleeps — goroutine start order is not FIFO).
	wg.Add(1)
	go func() {
		defer wg.Done()
		if g.acquire(context.Background(), false) == nil {
			order <- "bg0"
			g.release()
		}
	}()
	time.Sleep(30 * time.Millisecond)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if g.acquire(context.Background(), false) == nil {
			order <- "bg1"
			g.release()
		}
	}()
	time.Sleep(30 * time.Millisecond)
	// ...then the interactive one jumps them all.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if g.acquire(context.Background(), true) == nil {
			order <- "interactive"
			g.release()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	g.release()

	first := <-order
	if first != "interactive" {
		t.Fatalf("first grant after release = %q, want %q", first, "interactive")
	}
	// Remaining two are background FIFO.
	second := <-order
	third := <-order
	if second != "bg0" || third != "bg1" {
		t.Fatalf("background order = %q, %q; want bg0, bg1 (FIFO)", second, third)
	}
	wg.Wait()
}

// TestSlotGateStarvationGuard verifies the bounded jump: after 3
// consecutive interactive grants, 1 background grant releases before
// further interactive grants.
func TestSlotGateStarvationGuard(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	order := make(chan string, 8)

	// Enqueue a stable set with STRICT ordering: one background, then
	// four interactives (each fully parked before the next starts —
	// verify via waiterCount so ordering is load-bearing, not
	// sleep-luck). Grants are observed through the single `order`
	// channel, preserving grant order in the stream.
	record := func(name string, prio bool) {
		before := g.waiterCount()
		go func() {
			if g.acquire(context.Background(), prio) == nil {
				order <- name
				g.release()
			}
		}()
		// Wait until this waiter is parked on the lane.
		for g.waiterCount() <= before {
			time.Sleep(5 * time.Millisecond)
		}
	}

	record("bg", false)
	record("i1", true)
	record("i2", true)
	record("i3", true)
	record("i4", true)

	g.release()

	got := make([]string, 0, 5)
	for range 5 {
		select {
		case name := <-order:
			got = append(got, name)
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for grants; got %v", got)
		}
	}

	// Expected grant ORDER (lane preference is interactive-first; the
	// guard INSERTS one background grant after every 3 consecutive
	// interactive grants — the bounded jump):
	// i1, i2, i3 (guard counts 1, 2, 3), then bg (guard engaged →
	// background forced through, counter reset), then i4 (lane
	// preference resumes).
	want := []string{"i1", "i2", "i3", "bg", "i4"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("grant order = %v, want %v (guard forces background after 3 interactive)", got, want)
		}
	}
}

// TestSlotGateStarvationGuardBackgroundInserted is the sharper guard
// test: with a steady stream of interactive waiters, a background
// waiter that arrived FIRST must be granted after every 3rd
// interactive grant.
func TestSlotGateStarvationGuardBackgroundInserted(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	order := make(chan string, 8)
	record := func(name string, prio bool) {
		go func() {
			if g.acquire(context.Background(), prio) == nil {
				order <- name
				g.release()
			}
		}()
	}

	record("bg-first", false)
	time.Sleep(20 * time.Millisecond)
	record("i1", true)
	time.Sleep(20 * time.Millisecond)
	record("i2", true)
	time.Sleep(20 * time.Millisecond)
	record("i3", true)
	time.Sleep(20 * time.Millisecond)
	record("i4", true)
	time.Sleep(50 * time.Millisecond)

	g.release()

	// Expected order (interactive-first lanes; guard inserts the
	// earlier-arrived background waiter after every 3rd consecutive
	// interactive grant — bounded jump, never first):
	// i1, i2, i3 (guard 1, 2, 3) → bg-first (guard forced, reset) → i4.
	got := []string{
		<-order, <-order, <-order, <-order, <-order,
	}
	expected := []string{"i1", "i2", "i3", "bg-first", "i4"}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("grant %d = %q, want %q (full order %v)", i, got[i], expected[i], got)
		}
	}
}

// TestSlotGateCtxCancelDequeues verifies that a cancelled waiter is
// dequeued (no leaked waiter, no phantom grant later).
func TestSlotGateCtxCancelDequeues(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- g.acquire(ctx, false) }()
	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("cancelled waiter got a nil error (grant while held)")
		}
		if err != context.Canceled {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter not unblocked")
	}

	if n := g.waiterCount(); n != 0 {
		t.Fatalf("waiters after cancel = %d, want 0", n)
	}

	// The released slot still goes to a fresh waiter, not the ghost.
	got := make(chan error, 1)
	go func() { got <- g.acquire(context.Background(), true) }()
	g.release()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("fresh waiter: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fresh interactive waiter not granted")
	}
}

// TestSlotGateCtxCancelInteractive verifies cancellation from the
// interactive lane too.
func TestSlotGateCtxCancelInteractive(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), true); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- g.acquire(ctx, true) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("interactive waiter not unblocked on cancel")
	}
	if n := g.waiterCount(); n != 0 {
		t.Fatalf("waiters after cancel = %d, want 0", n)
	}
}

// TestSlotGateUncontendedNoAlloc verifies the leaf's "zero overhead when
// uncontended" requirement: acquire+release with no contention performs
// no heap allocation.
func TestSlotGateUncontendedNoAlloc(t *testing.T) {
	g := newSlotGate(1)
	if err := g.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	g.release()

	// Warm up any lazy allocations.
	for range 100 {
		_ = g.acquire(context.Background(), true)
		g.release()
	}
	runtime.GC()

	allocs := testing.AllocsPerRun(200, func() {
		_ = g.acquire(context.Background(), true)
		g.release()
	})
	if allocs > 0 {
		t.Fatalf("uncontended acquire+release allocated %v times", allocs)
	}
}

// TestSlotGateConcurrentStress hammers the gate from both lanes to give
// the race detector real contention; every acquirer must eventually be
// granted exactly once and every release balanced.
func TestSlotGateConcurrentStress(t *testing.T) {
	g := newSlotGate(4)
	const workers = 64
	const iters = 25
	var wg sync.WaitGroup
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0

	for w := range workers {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := range iters {
				prio := (seed+i)%2 == 0
				if err := g.acquire(context.Background(), prio); err != nil {
					t.Errorf("acquire: %v", err)
					return
				}
				mu.Lock()
				inFlight++
				if inFlight > maxInFlight {
					maxInFlight = inFlight
				}
				mu.Unlock()

				mu.Lock()
				inFlight--
				mu.Unlock()
				g.release()
			}
		}(w)
	}
	wg.Wait()

	if maxInFlight > 4 {
		t.Fatalf("max in-flight = %d, want <= cap 4", maxInFlight)
	}
	if held := g.heldCount(); held != 0 {
		t.Fatalf("held after stress = %d, want 0", held)
	}
	if n := g.waiterCount(); n != 0 {
		t.Fatalf("waiters after stress = %d, want 0", n)
	}
}

// TestSlotGateInteractiveNoWaitersGrantsImmediately verifies an
// interactive acquire takes the fast path when lanes are empty.
func TestSlotGateInteractiveNoWaitersGrantsImmediately(t *testing.T) {
	g := newSlotGate(2)
	done := make(chan error, 1)
	go func() { done <- g.acquire(context.Background(), true) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("uncontended interactive acquire blocked")
	}
}
