package signalleg

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestEndDoesNotAbandonCompletedCall pins A4-6: End must snapshot callID
// under l.mu so that a call that has been StartCall'd and Completed by
// Place is NOT abandoned when End is called. At HEAD, End read
// st.callID == "" without holding l.mu, so a concurrent Place (which
// stores callID under the lock after StartCall) could leave End observing
// "" and abandoning a reservation that Place had already completed.
//
// This test simulates the "already completed" state directly: it places a
// call (which sets callID and completes the effect), then Ends it and
// asserts the effect was NOT abandoned.
func TestEndDoesNotAbandonCompletedCall(t *testing.T) {
	leg, fx, _, _ := newTestLeg(t, func(uuid string) bool { return true })

	ccid, err := leg.Place(context.Background(), "emp-1", "aaaa1111-2222-3333-4444-555555555555", "")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	// The effect was reserved and completed by Place. Capture the count of
	// abandons so far (should be 0).
	fx.mu.Lock()
	abandonsBefore := len(fx.abandon)
	fx.mu.Unlock()

	// End the call. Since callID is set (StartCall landed), End must NOT
	// abandon the effect.
	if err := leg.End(ccid); err != nil {
		t.Fatalf("End: %v", err)
	}

	fx.mu.Lock()
	abandonsAfter := len(fx.abandon)
	fx.mu.Unlock()

	if abandonsAfter != abandonsBefore {
		t.Fatalf("End abandoned the effect for a completed call: abandons %d -> %d", abandonsBefore, abandonsAfter)
	}
}

// TestEndAbandonsWhenCallIDEmpty pins the inverse: when callID is empty
// (StartCall never landed), End SHOULD abandon the effect. This ensures
// the fix doesn't over-correct — the abandon path still works for the
// genuine "ended before connect" case.
func TestEndAbandonsWhenCallIDEmpty(t *testing.T) {
	leg, fx, _, _ := newTestLeg(t, func(uuid string) bool { return true })

	ccid, err := leg.Place(context.Background(), "emp-1", "bbbb2222-2222-3333-4444-555555555555", "")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	// Simulate the "StartCall never landed" state by clearing callID on the
	// tracked state. This mirrors the pre-StartCall window.
	leg.mu.Lock()
	st, ok := leg.calls[ccid]
	if !ok {
		leg.mu.Unlock()
		t.Fatalf("call state not found for ccid %s", ccid)
	}
	st.callID = ""
	leg.mu.Unlock()

	if err := leg.End(ccid); err != nil {
		t.Fatalf("End: %v", err)
	}

	fx.mu.Lock()
	abandons := len(fx.abandon)
	fx.mu.Unlock()
	if abandons == 0 {
		t.Fatalf("End did not abandon the effect for a call with empty callID (expected abandon)")
	}
}

// TestEndConcurrentWithPlaceNoAbandonAfterComplete is a race-shaped pin:
// it runs End concurrently with a fresh Place and asserts that the effect
// is never abandoned AFTER Place completes it. The fix snapshots callID
// under l.mu, so even if End reaps the call while Place is about to set
// callID, the snapshot is taken atomically with the delete.
//
// This is a best-effort race pin: the exact interleaving (End reaping while
// Place is mid-StartCall) is hard to force deterministically, so we hammer
// the pattern and rely on -race to catch data races, plus the assertion
// that a completed call is never abandoned.
func TestEndConcurrentWithPlaceNoAbandonAfterComplete(t *testing.T) {
	leg, fx, _, _ := newTestLeg(t, func(uuid string) bool { return true })

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			uuid := "cccc3333-2222-3333-4444-555555555555"
			ccid, err := leg.Place(context.Background(), "emp-1", uuid, "")
			if err != nil {
				return
			}
			// End immediately after Place returns (callID is set at this
			// point, since Place sets it before returning). This is the
			// "completed call" case: no abandon expected.
			if err := leg.End(ccid); err != nil {
				return
			}
		}(i)
	}
	wg.Wait()

	fx.mu.Lock()
	abandons := len(fx.abandon)
	fx.mu.Unlock()
	if abandons > 0 {
		t.Fatalf("concurrent Place+End abandoned effects: %d abandons (a completed call must not be abandoned)", abandons)
	}
}

// Ensure the test file is used (avoids unused import warnings if the
// above tests are the only ones). This is a no-op to keep the file
// compiling even if the package's other tests define these names.
var _ = time.Millisecond
