package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthChecker_StaleGenerationDoesNotWriteVerdict pins the generation guard.
// A check that began before Stop (or before a re-arm) measured the PREVIOUS
// process/endpoint; letting it write its verdict into the current generation is
// how a restarted runtime gets reported healthy before it binds. The endpoint
// here answers 200, so an unguarded stale write would set healthy=true.
func TestHealthChecker_StaleGenerationDoesNotWriteVerdict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &RuntimeConfig{
		HealthEndpoint:  "/health",
		HealthInterval:  time.Second,
		HealthTimeout:   time.Second,
		HealthThreshold: 1,
	}
	hc := NewHealthChecker(cfg, srv.URL)
	hc.Start(context.Background())
	hc.Stop()

	// A check from the stopped generation: the checker has no active run, so the
	// guard must drop the write.
	stale := make(chan struct{})
	hc.checkOnce(stale)

	if hc.IsHealthy() {
		t.Error("a check from a stopped generation wrote a healthy verdict")
	}
}

// TestHealthChecker_SupersededRunDoesNotClearActiveFlag pins audit finding F60:
// the deferred clear in a run goroutine must belong to ITS generation. A
// superseded goroutine (Stop→Start re-armed a new run before this one noticed)
// clearing the shared flag made Stop a no-op for the active run and let further
// Starts arm concurrent, unstoppable runs.
func TestHealthChecker_SupersededRunDoesNotClearActiveFlag(t *testing.T) {
	cfg := &RuntimeConfig{HealthEndpoint: "/health", HealthInterval: time.Hour, HealthTimeout: time.Second, HealthThreshold: 1}
	hc := NewHealthChecker(cfg, "http://127.0.0.1:1")

	stopped := make(chan struct{}) // the superseded run's generation
	active := make(chan struct{})  // the current run's generation
	hc.mu.Lock()
	hc.stopCh = active
	hc.running = true
	hc.mu.Unlock()

	// The superseded run exits.
	hc.finishRun(stopped)

	hc.mu.RLock()
	running, cur := hc.running, hc.stopCh
	hc.mu.RUnlock()
	if !running {
		t.Error("a superseded run cleared the ACTIVE run's running flag (F60)")
	}
	if cur != active {
		t.Error("a superseded run changed the active run's stop channel")
	}

	// The active run exiting DOES clear the flag.
	hc.finishRun(active)
	hc.mu.RLock()
	running = hc.running
	hc.mu.RUnlock()
	if running {
		t.Error("the active run's exit must clear the running flag")
	}
}

// TestHealthChecker_StopClosesActiveChannelDespiteClearedFlag pins the other
// half of F60: Stop must close the channel of the run it believes is active
// even when the running flag was cleared, or that run can never be stopped
// (StopAll at shutdown included).
func TestHealthChecker_StopClosesActiveChannelDespiteClearedFlag(t *testing.T) {
	cfg := &RuntimeConfig{HealthEndpoint: "/health", HealthInterval: time.Hour, HealthTimeout: time.Second, HealthThreshold: 1}
	hc := NewHealthChecker(cfg, "http://127.0.0.1:1")

	ch := make(chan struct{})
	hc.mu.Lock()
	hc.stopCh = ch
	hc.running = false // a stale/superseded clear left the flag false
	hc.mu.Unlock()

	hc.Stop()
	select {
	case <-ch:
		// closed: the active run was signalled to stop
	default:
		t.Error("Stop must close the active run's channel even when the flag was cleared (F60)")
	}
}
