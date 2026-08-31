package agent

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestChatHandler_StopStopsQuotaResumeWatcher verifies that ChatHandler.Stop
// stops the quota resume watcher (quota-reset-resilience leaf 06). Without
// the Stop call, the watcher's polling goroutine leaks on daemon shutdown
// and parked-turn resumes can fire mid-shutdown.
func TestChatHandler_StopStopsQuotaResumeWatcher(t *testing.T) {
	h := &ChatHandler{logger: testLogger()}
	h.SetQuotaResumeConfig(time.Hour)
	if h.quotaResumeWatcher == nil {
		t.Fatal("SetQuotaResumeConfig did not wire the watcher")
	}

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.quotaResumeWatcher.Start(ctx)
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	// The watcher's Stop waits for its goroutine to exit; allow a small
	// settle window for scheduler lag before comparing counts.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Fatalf("goroutine leak after ChatHandler.Stop: before=%d after=%d", before, after)
	}

	// Double-stop must be safe (idempotent watcher Stop).
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
}

// TestChatHandler_StopNilWatchers verifies Stop is safe when no resume
// watcher was ever configured (multi-user disabled path and unit setups).
func TestChatHandler_StopNilWatchers(t *testing.T) {
	h := &ChatHandler{logger: testLogger()}
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("Stop with nil watchers returned error: %v", err)
	}
}
