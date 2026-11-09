package agent

import (
	"sync"
	"testing"
)

// Tests for the turnCounter type's WIRED surface: Increment/Remaining (the
// subagent_pool RLM runtime consumes these via rlm_analyzer's Nudge chain).
// The nudge-policy remainder (GetRemainingTurns, GetNudgeThreshold, RoundTo,
// thresholdFor standalone use, String) was deleted with the HALO Phase 7
// skip decision — these tests cover only what production calls.

func TestTurnCounter_IncrementAndRemaining(t *testing.T) {
	tc := newTurnCounter(10)
	if tc.Remaining() != 10 {
		t.Errorf("Remaining = %d, want 10", tc.Remaining())
	}
	n, exhausted := tc.Increment()
	if exhausted {
		t.Fatal("first increment must not exhaust")
	}
	if n != 1 {
		t.Errorf("count after 1 increment = %d, want 1", n)
	}
	if tc.Remaining() != 9 {
		t.Errorf("Remaining after 1 increment = %d, want 9", tc.Remaining())
	}
}

func TestTurnCounter_ZeroLimit(t *testing.T) {
	tc := newTurnCounter(0)
	_, exhausted := tc.Increment()
	if !exhausted {
		t.Fatal("zero limit must exhaust on first increment")
	}
}

func TestTurnCounter_MultiIncrement(t *testing.T) {
	tc := newTurnCounter(3)
	for i := 0; i < 2; i++ {
		_, exhausted := tc.Increment()
		if exhausted {
			t.Fatalf("increment %d must not exhaust (limit 3)", i+1)
		}
	}
	_, exhausted := tc.Increment()
	if !exhausted {
		t.Fatal("3rd increment on limit 3 must exhaust")
	}
}

func TestTurnCounter_Concurrent(t *testing.T) {
	tc := newTurnCounter(1000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tc.Increment()
		}()
	}
	wg.Wait()
	if got := tc.GetCount(); got != 50 {
		t.Errorf("GetCount after 50 concurrent increments = %d, want 50", got)
	}
}

func TestTurnCounter_Reset(t *testing.T) {
	tc := newTurnCounter(5)
	tc.Increment()
	tc.Reset()
	if tc.GetCount() != 0 {
		t.Errorf("GetCount after reset = %d, want 0", tc.GetCount())
	}
}
