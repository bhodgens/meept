package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func makeValidConfig(t *testing.T) llm.RuntimeConfig {
	t.Helper()
	modelPath := createTempModelFile(t)
	return llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       modelPath,
		HealthEndpoint:  "/health",
		HealthInterval:  500 * time.Millisecond,
		HealthTimeout:   2 * time.Second,
		HealthThreshold: 3,
	}
}

func TestNewHealthChecker(t *testing.T) {
	config := makeValidConfig(t)
	hc := llm.NewHealthChecker(&config, "http://localhost:8080")

	if hc == nil {
		t.Fatal("expected health checker, got nil")
	}
	if config.HealthTimeout != 2*time.Second {
		t.Errorf("expected health timeout %v, got %v", 2*time.Second, config.HealthTimeout)
	}
}

func TestHealthChecker_IsHealthy_Initial(t *testing.T) {
	config := makeValidConfig(t)
	config.HealthInterval = 10 * time.Second // long enough that first check won't fire during test
	hc := llm.NewHealthChecker(&config, "http://localhost:8080")

	// Initially the checker should report unhealthy until the first check runs
	if hc.IsHealthy() {
		t.Error("expected initial health state to be false")
	}
}

func TestHealthChecker_IsHealthy_AfterSuccessfulCheck(t *testing.T) {
	mu := sync.Mutex{}
	healthy := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	config := makeValidConfig(t)
	config.HealthEndpoint = "/health"
	config.HealthInterval = 100 * time.Millisecond
	config.HealthThreshold = 1

	hc := llm.NewHealthChecker(&config, server.URL)
	hc.Start(context.Background())
	defer hc.Stop()

	// Initially unhealthy (server returns 500)
	if hc.IsHealthy() {
		t.Error("expected unhealthy before server is stable")
	}

	// Wait a couple of intervals for unhealthy counting
	time.Sleep(300 * time.Millisecond)

	// Still unhealthy (server still returning 500)
	if hc.IsHealthy() {
		t.Error("expected unhealthy while server returns 500")
	}

	// Make server healthy
	mu.Lock()
	healthy = true
	mu.Unlock()

	// Wait for check to see healthy response
	time.Sleep(300 * time.Millisecond)

	if !hc.IsHealthy() {
		t.Error("expected healthy after server returns 200 and threshold exceeded")
	}
}

func TestHealthChecker_Stop(t *testing.T) {
	config := makeValidConfig(t)
	config.HealthInterval = 50 * time.Millisecond

	hc := llm.NewHealthChecker(&config, "http://localhost:12345")
	hc.Start(context.Background())

	// Stop once should work
	hc.Stop()

	// Stop again should be safe (idempotent)
	hc.Stop()

	// Should not panic or block
}

func TestHealthChecker_StopBeforeStart(t *testing.T) {
	config := makeValidConfig(t)
	hc := llm.NewHealthChecker(&config, "http://localhost:12345")

	// Should not panic when stopping before start
	hc.Stop()
}

func TestHealthChecker_WaitForHealthy_Timeout(t *testing.T) {
	config := makeValidConfig(t)
	config.HealthInterval = 50 * time.Millisecond
	config.HealthThreshold = 1

	hc := llm.NewHealthChecker(&config, "http://localhost:12345")
	hc.Start(context.Background())
	defer hc.Stop()

	// No server on 12345, so this should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := hc.WaitForHealthy(ctx, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHealthChecker_WaitForHealthy_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := makeValidConfig(t)
	config.HealthEndpoint = "/health"
	config.HealthInterval = 50 * time.Millisecond
	config.HealthThreshold = 1

	hc := llm.NewHealthChecker(&config, server.URL)
	hc.Start(context.Background())
	defer hc.Stop()

	// Wait a couple intervals for the checker to see healthy
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hc.WaitForHealthy(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestHealthChecker_Concurrent_Safety(t *testing.T) {
	config := makeValidConfig(t)
	config.HealthInterval = 100 * time.Millisecond
	config.HealthThreshold = 2

	hc := llm.NewHealthChecker(&config, "http://localhost:12345")
	hc.Start(context.Background())

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_ = hc.IsHealthy()
		})
	}

	wg.Wait()
	hc.Stop()
}

// assertEventuallyHealthy polls the checker until it reports healthy.
func assertEventuallyHealthy(t *testing.T, hc *llm.HealthChecker, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hc.IsHealthy() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never became healthy: %s", what)
}

// TestHealthChecker_DeadProcessIsUnhealthy pins the ownership rule for the
// health check: a 200 from the endpoint is not enough. A foreign listener on
// the same port answers /health for a child that died at bind time, so a dead
// process must read unhealthy no matter what the endpoint returns.
func TestHealthChecker_DeadProcessIsUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	config := makeValidConfig(t)
	config.HealthInterval = 20 * time.Millisecond
	config.HealthThreshold = 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Control: the same endpoint with a live process does become healthy.
	live := llm.NewHealthChecker(&config, srv.URL)
	live.SetProcessAliveProbe(func() bool { return true })
	live.Start(ctx)
	defer live.Stop()
	assertEventuallyHealthy(t, live, "live process behind a 200 endpoint")

	// Subject: endpoint still answers 200, the process probe says dead.
	dead := llm.NewHealthChecker(&config, srv.URL)
	dead.SetProcessAliveProbe(func() bool { return false })
	dead.Start(ctx)
	defer dead.Stop()

	// Several intervals must pass without the checker flipping to healthy.
	time.Sleep(150 * time.Millisecond)
	if dead.IsHealthy() {
		t.Error("a 200 endpoint with a dead process must not read as healthy")
	}
}
