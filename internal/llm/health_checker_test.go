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
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = hc.IsHealthy()
		}()
	}

	wg.Wait()
	hc.Stop()
}
