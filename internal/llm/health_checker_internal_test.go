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
