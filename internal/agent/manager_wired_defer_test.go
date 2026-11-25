package agent

import (
	"testing"
	"time"
)

// TestManager_GetOrCreateWired_ActiveStaleCloseDeferred pins bughunt
// 2026-11-24 H8: Manager.GetOrCreateWired closed an evicted stale loop
// INLINE even when a turn was executing on it — the production chat path
// (ChatHandler:4142 uses GetOrCreateWired) kept the mid-turn Close hazard
// that 8af54fe5 fixed only in the two fallback scoped caches. The evicted
// loop with an active turn must be closed DEFERRED (after the turn ends
// or at the bounded cap), and the replacement loop must be live.
func TestManager_GetOrCreateWired_ActiveStaleCloseDeferred(t *testing.T) {
	mgr := NewManager(managerConfig())
	template := NewAgentLoop("singleton", "/tmp")

	first, err := mgr.GetOrCreateWired("conv-defer", "/repos/a", template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	if first == nil {
		t.Fatal("nil loop")
	}

	// Simulate a turn in flight on the cached loop.
	first.SetActive(true)

	// Project change evicts it.
	second, err := mgr.GetOrCreateWired("conv-defer", "/repos/b", template)
	if err != nil {
		t.Fatalf("GetOrCreateWired (project change): %v", err)
	}
	if second == first {
		t.Fatal("expected a NEW loop after workingDir change")
	}

	// The active stale loop must NOT be closed yet: give the deferred
	// closer's 100ms poll a moment and confirm it is still open while
	// the turn is active.
	time.Sleep(300 * time.Millisecond)
	if first.closed.Load() {
		t.Fatal("H8: active stale loop was closed INLINE despite a turn in flight")
	}

	// Turn ends -> the deferred closer fires within its poll interval.
	first.SetActive(false)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if first.closed.Load() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("stale loop was never closed after its turn ended")
}
