package voice

// Bughunt 2026-11-25 A5-6 pins: OutcomeRegistry.outcomes grew one entry per
// phone call for the daemon's lifetime (Record only writes; nothing evicted).
// Record now caps the map (default 1024) with oldest-first FIFO eviction over
// an insertion-order ring. The two pre-existing A5-1 Telnyx call-cap pins
// that used to live in this file moved verbatim to
// bughunt_1126_voiceattempt_existing_pins_test.go.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// A5-6 pins below: OutcomeRegistry.outcomes used to grow one entry per phone
// call for the daemon's lifetime with no eviction path.

// TestOutcomeRegistry_CapEvictsOldest: recording cap+1 distinct keys must
// bound the map at cap and drop the OLDEST entry, keeping the newest.
func TestOutcomeRegistry_CapEvictsOldest(t *testing.T) {
	reg := NewOutcomeRegistry()
	reg.cap = 4

	for i := 0; i < 5; i++ {
		reg.Record(fmt.Sprintf("ccid-%d", i), "answered")
	}

	reg.mu.Lock()
	got := len(reg.outcomes)
	orderLen := len(reg.order)
	reg.mu.Unlock()
	if got != 4 {
		t.Errorf("outcomes len = %d, want cap 4 (A5-6: unbounded growth)", got)
	}
	if orderLen != 4 {
		t.Errorf("order len = %d, want 4 (eviction must shrink the ring too)", orderLen)
	}

	lookupCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// ccid-0 (oldest) evicted: Lookup must time out false, not block forever.
	if _, ok := reg.Lookup(lookupCtx, "ccid-0"); ok {
		t.Errorf("ccid-0 survived eviction; oldest must be dropped first")
	}
	// ccid-4 (newest) must still resolve immediately.
	out, ok := reg.Lookup(lookupCtx, "ccid-4")
	if !ok || out != "answered" {
		t.Errorf("ccid-4 = %q/%v, want answered/true", out, ok)
	}
}

// TestOutcomeRegistry_ReRecordDoesNotGrowOrder: re-Recording the same key
// must update the outcome without appending a second order entry (a double
// append would evict live entries early).
func TestOutcomeRegistry_ReRecordDoesNotGrowOrder(t *testing.T) {
	reg := NewOutcomeRegistry()
	reg.cap = 8

	for i := 0; i < 5; i++ {
		reg.Record("same-ccid", "answered")
	}

	reg.mu.Lock()
	orderLen := len(reg.order)
	mapLen := len(reg.outcomes)
	reg.mu.Unlock()
	if orderLen != 1 || mapLen != 1 {
		t.Errorf("re-Record grew state: order=%d map=%d, want 1/1", orderLen, mapLen)
	}

	// The survivor must be the LATEST outcome, not the first write.
	reg.Record("same-ccid", "no_answer")
	lookupCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if out, ok := reg.Lookup(lookupCtx, "same-ccid"); !ok || out != "no_answer" {
		t.Errorf("re-Record lost the update: %q/%v, want no_answer/true", out, ok)
	}
}

// TestOutcomeRegistry_EvictionSafeUnderConcurrentWaiters: eviction while a
// concurrent Record+Lookup churn runs must not panic or corrupt state, and
// the cap must hold at the end.
func TestOutcomeRegistry_EvictionSafeUnderConcurrentWaiters(t *testing.T) {
	reg := NewOutcomeRegistry()
	reg.cap = 32

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				ccid := fmt.Sprintf("ccid-%d-%d", base, i)
				reg.Record(ccid, "answered")
				lookupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
				_, _ = reg.Lookup(lookupCtx, ccid)
				cancel()
			}
		}(w)
	}
	wg.Wait()

	reg.mu.Lock()
	got := len(reg.outcomes)
	orderLen := len(reg.order)
	reg.mu.Unlock()
	if got > reg.cap || orderLen > reg.cap {
		t.Errorf("post-churn len map=%d order=%d, want <= cap %d", got, orderLen, reg.cap)
	}
}
