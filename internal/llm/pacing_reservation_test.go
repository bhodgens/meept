package llm

// Concurrent-Wait ticket-reservation tests: with N agents waiting on ONE
// provider, each Wait must own a DISTINCT slot on the provider's outbound
// timeline (spaced one enforced gap apart). Before the reservation fix every
// concurrent Wait read the same lastClaim, computed the same wait against
// now, and all woke together — zero enforced gap.
//
// Determinism: a STATIC injected clock (the fake sleep does not advance it),
// so every herd member reads the same `now` regardless of goroutine
// scheduling — the exact thundering-herd shape the fix targets, with zero
// real-clock dependence (SHARED-CONVENTIONS §5). Reservation order across
// goroutines is nondeterministic, so slot assertions compare the SORTED set.

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

// herdSleeper is a concurrency-safe fake sleep: it records the requested
// duration and does NOT advance the clock, keeping every concurrent Wait's
// `now` identical.
type herdSleeper struct {
	mu    sync.Mutex
	slots []time.Duration
}

func (h *herdSleeper) sleep(ctx context.Context, d time.Duration) error {
	h.mu.Lock()
	h.slots = append(h.slots, d)
	h.mu.Unlock()
	return nil
}

// count returns the number of recorded slots.
func (h *herdSleeper) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.slots)
}

// sortedSince returns a sorted copy of the slots recorded after n entries.
func (h *herdSleeper) sortedSince(n int) []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := append([]time.Duration(nil), h.slots[n:]...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// newHerdPacer builds an enabled pacer over a static clock with the
// concurrency-safe sleeper.
func newHerdPacer(t *testing.T) (*AdaptivePacer, *pacingClock, *herdSleeper) {
	t.Helper()
	clock := &pacingClock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	p := NewAdaptivePacer(nil, testPacingCfg())
	p.now = clock.Now
	sleeper := &herdSleeper{}
	p.sleepFn = sleeper.sleep
	return p, clock, sleeper
}

// TestAdaptivePacer_ConcurrentWaitsReserveDistinctSlots is the core ticket
// reservation: N concurrent Waits arriving at the same clock instant must
// each be handed a DIFFERENT wake slot, spaced one enforced gap (40ms) apart.
func TestAdaptivePacer_ConcurrentWaitsReserveDistinctSlots(t *testing.T) {
	p, clock, sleeper := newHerdPacer(t)

	// Learn a 40ms interval and seed the timeline: the first Wait claims
	// without waiting; the second pays the gap from the first claim.
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("seed Wait 1 = %v, want nil", err)
	}
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("seed Wait 2 = %v, want nil", err)
	}

	// Park the clock at the last claim boundary, then release the herd:
	// all N goroutines reserve at the SAME instant.
	const n = 5
	clock.Advance(40 * time.Millisecond)
	before := sleeper.count()

	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Wait(context.Background(), "prov"); err != nil {
				t.Errorf("concurrent Wait = %v, want nil", err)
			}
		}()
	}
	wg.Wait()

	got := sleeper.sortedSince(before)
	if len(got) != n {
		t.Fatalf("herd sleeps = %v (%d), want %d reserved slots", got, len(got), n)
	}
	want := []time.Duration{
		40 * time.Millisecond,
		80 * time.Millisecond,
		120 * time.Millisecond,
		160 * time.Millisecond,
		200 * time.Millisecond,
	}
	for i, d := range want {
		if got[i] != d {
			t.Errorf("sorted herd slot[%d] = %v, want %v (tickets one 40ms gap apart)", i, got[i], d)
		}
	}
}

// TestAdaptivePacer_ConcurrentWaitsNotAllWakeTogether pins the OLD bug
// directly: under the pre-fix arithmetic, five Waits at the same instant all
// computed the SAME wait (the herd). The reservation must yield distinct
// waits without depending on exact slot values.
func TestAdaptivePacer_ConcurrentWaitsNotAllWakeTogether(t *testing.T) {
	p, clock, sleeper := newHerdPacer(t)

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	_ = p.Wait(context.Background(), "prov")
	_ = p.Wait(context.Background(), "prov")
	clock.Advance(40 * time.Millisecond)
	before := sleeper.count()

	const n = 5
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Wait(context.Background(), "prov"); err != nil {
				t.Errorf("concurrent Wait = %v, want nil", err)
			}
		}()
	}
	wg.Wait()

	got := sleeper.sortedSince(before)
	if len(got) != n {
		t.Fatalf("herd sleeps = %v (%d), want %d", got, len(got), n)
	}
	distinct := make(map[time.Duration]bool, n)
	for _, d := range got {
		distinct[d] = true
	}
	if len(distinct) == 1 {
		t.Fatalf("all %d concurrent Waits computed the SAME wait %v — zero enforced gap (pre-fix herd behavior)", n, got[0])
	}
}

// TestAdaptivePacer_ReservationClaimsThenSubsequentWaitsQueue pins the
// timeline after a herd: the NEXT sequential Wait (same clock instant as the
// herd's reservations) queues behind the last ticket — it waits for the
// remaining tail rather than passing immediately.
func TestAdaptivePacer_ReservationClaimsThenSubsequentWaitsQueue(t *testing.T) {
	p, clock, sleeper := newHerdPacer(t)

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	_ = p.Wait(context.Background(), "prov") // first claim
	_ = p.Wait(context.Background(), "prov") // pays 40ms
	clock.Advance(40 * time.Millisecond)

	const n = 3
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Wait(context.Background(), "prov")
		}()
	}
	wg.Wait()

	// Herd reserved slots ending at start+40/80/120/160ms from the clock
	// instant T0; the last ticket's END is T0+160. A further Wait at the
	// same instant queues behind the LAST ticket: it waits from now (T0)
	// until that end — 160ms.
	before := sleeper.count()
	if err := p.Wait(context.Background(), "prov"); err != nil {
		t.Fatalf("queued Wait = %v, want nil", err)
	}
	got := sleeper.sortedSince(before)
	if len(got) != 1 {
		t.Fatalf("post-herd sleeps = %v, want exactly the queued Wait", got)
	}
	if got[0] != 160*time.Millisecond {
		t.Errorf("queued Wait slept %v, want 160ms (from now to the last ticket's end)", got[0])
	}
}

// TestAdaptivePacer_ReservationAcrossProvidersIndependent pins provider
// scoping: a herd on one provider must not consume another provider's
// timeline (tickets are per-provider).
func TestAdaptivePacer_ReservationAcrossProvidersIndependent(t *testing.T) {
	p, clock, sleeper := newHerdPacer(t)

	// provA learns its 40ms interval and seeds its timeline.
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "provA")
	_ = p.Wait(context.Background(), "provA")
	_ = p.Wait(context.Background(), "provA")
	// provB learns its own interval; its timeline stays untouched.
	p.Observe(PolicyVerdict{Class: FailureThrottle}, "provB")
	clock.Advance(40 * time.Millisecond)

	// provB herd at the same instant: independent timeline — first
	// claim is free, second takes the 40ms slot.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Wait(context.Background(), "provB")
		}()
	}
	wg.Wait()

	// A further provB Wait queues behind its own herd's last ticket
	// (ends now+80ms), unperturbed by provA's timeline.
	before := sleeper.count()
	if err := p.Wait(context.Background(), "provB"); err != nil {
		t.Fatalf("provB queued Wait = %v, want nil", err)
	}
	got := sleeper.sortedSince(before)
	if len(got) != 1 || got[0] != 80*time.Millisecond {
		t.Errorf("provB queued Wait = %v, want [80ms] (own timeline, herd claims free/40)", got)
	}
}

// TestAdaptivePacer_CtxCancelAbandonsWaitSlot pins the cancellation
// contract: a Wait whose sleep is canceled returns the sleep error (existing
// seam behavior). The reservation itself already consumed the timeline
// position — documented canceled-after-admit semantics, mirroring a request
// canceled after pacing admitted it.
func TestAdaptivePacer_CtxCancelAbandonsWaitSlot(t *testing.T) {
	p, _, _ := newHerdPacer(t)
	p.sleepFn = func(ctx context.Context, d time.Duration) error {
		return context.Canceled
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p.Observe(PolicyVerdict{Class: FailureThrottle}, "prov")
	_ = p.Wait(context.Background(), "prov")
	_ = p.Wait(context.Background(), "prov")

	if err := p.Wait(ctx, "prov"); err == nil {
		t.Fatal("canceled Wait = nil, want the sleep error propagated (slot abandoned)")
	}
}
