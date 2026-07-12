package taint

import (
	"sync"
	"testing"
)

func TestParallelTaintTracker_RecordAndGet(t *testing.T) {
	tracker := NewParallelTaintTracker()
	tracker.RecordTaint("call_1", TaintExternal)

	got := tracker.GetCombinedTaint([]string{"call_1"})
	if got != TaintExternal {
		t.Errorf("GetCombinedTaint = %q, want %q", got, TaintExternal)
	}
}

func TestParallelTaintTracker_CombinedTaint_MostRestrictive(t *testing.T) {
	tracker := NewParallelTaintTracker()
	tracker.RecordTaint("call_a", TaintUserInput)
	tracker.RecordTaint("call_b", TaintSecret)
	tracker.RecordTaint("call_c", TaintExternal)

	got := tracker.GetCombinedTaint([]string{"call_a", "call_b", "call_c"})
	if got != TaintSecret {
		t.Errorf("GetCombinedTaint = %q, want %q (most restrictive)", got, TaintSecret)
	}
}

func TestParallelTaintTracker_CombinedTaint_AllClean(t *testing.T) {
	tracker := NewParallelTaintTracker()
	// Record nothing for any call — all clean.

	got := tracker.GetCombinedTaint([]string{"call_x", "call_y"})
	if got != TaintNone {
		t.Errorf("GetCombinedTaint = %q, want %q", got, TaintNone)
	}
}

func TestParallelTaintTracker_CombinedTaint_PartialClean(t *testing.T) {
	// One tainted, one clean — combined should be the tainted one.
	tracker := NewParallelTaintTracker()
	tracker.RecordTaint("call_tainted", TaintShell)

	got := tracker.GetCombinedTaint([]string{"call_tainted", "call_clean"})
	if got != TaintShell {
		t.Errorf("GetCombinedTaint = %q, want %q", got, TaintShell)
	}
}

func TestParallelTaintTracker_IsTainted_True(t *testing.T) {
	tracker := NewParallelTaintTracker()
	tracker.RecordTaint("call_1", TaintUntrusted)

	if !tracker.IsTainted([]string{"call_1", "call_2"}) {
		t.Error("IsTainted = false, want true (call_1 is tainted)")
	}
}

func TestParallelTaintTracker_IsTainted_False(t *testing.T) {
	tracker := NewParallelTaintTracker()

	if tracker.IsTainted([]string{"call_1", "call_2"}) {
		t.Error("IsTainted = true, want false (no taints recorded)")
	}
}

func TestParallelTaintTracker_Concurrent(t *testing.T) {
	// Verify that concurrent RecordTaint calls don't race (-race flag).
	tracker := NewParallelTaintTracker()
	labels := []TaintLabel{TaintSecret, TaintUntrusted, TaintShell, TaintExternal, TaintUserInput}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			callID := labelForIdx(idx)
			label := labels[idx%len(labels)]
			tracker.RecordTaint(callID, label)
			_ = tracker.GetCombinedTaint([]string{callID})
			_ = tracker.IsTainted([]string{callID})
		}(i)
	}
	wg.Wait()
}

func labelForIdx(i int) string {
	return "call_" + string(rune('A'+i%26)) + string(rune('0'+i/26+1))
}
