package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// HealthChangeCallback is invoked when health state changes.
type HealthChangeCallback func(healthy bool)

// HealthChecker performs periodic HTTP health checks on a runtime.
type HealthChecker struct {
	config         *RuntimeConfig
	client         *http.Client
	baseURL        string
	healthy        bool
	unhealthyCount int
	mu             sync.RWMutex
	stopCh         chan struct{}
	stopped        bool
	onHealthChange HealthChangeCallback
	logger         *slog.Logger
	// procAlive reports whether the process this checker was created for is
	// still running. nil = the checker is not bound to a spawn (standalone CLI
	// health waits): only the endpoint is then consulted.
	procAlive func() bool
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(cfg *RuntimeConfig, baseURL string) *HealthChecker {
	return &HealthChecker{
		config:  cfg,
		client:  &http.Client{Timeout: cfg.HealthTimeout},
		baseURL: baseURL,
		stopCh:  make(chan struct{}),
		logger:  slog.Default().With("component", "health-checker"),
	}
}

// Start begins periodic health checks in a background goroutine.
func (h *HealthChecker) Start(ctx context.Context) {
	go h.run(ctx)
}

func (h *HealthChecker) run(ctx context.Context) {
	ticker := time.NewTicker(h.config.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.checkOnce()
		}
	}
}

// SetProcessAliveProbe binds the checker to the process the endpoint belongs
// to. Without a probe, a foreign listener on the endpoint port answers /health
// with 200 and the runtime is reported healthy forever, even when the child we
// spawned died at bind time. With a probe, a dead child is unhealthy no matter
// what the endpoint says.
func (h *HealthChecker) SetProcessAliveProbe(fn func() bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.procAlive = fn
}

// checkOnce performs one health check.
func (h *HealthChecker) checkOnce() {
	h.mu.RLock()
	wasHealthy := h.healthy
	h.mu.RUnlock()

	// Perform HTTP check outside the lock to avoid blocking IsHealthy() calls.
	// Construct the health check URL from the server root, not the API base URL.
	// This handles cases where baseURL is "http://host:port/v1" but the health
	// endpoint is at "http://host:port/health".
	healthURL := h.baseURL + h.config.HealthEndpoint
	if parsed, err := url.Parse(h.baseURL); err == nil && parsed.Path != "" {
		// baseURL has a path component (e.g., /v1), strip it and use server root
		healthURL = fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, h.config.HealthEndpoint)
	}
	resp, err := h.client.Get(healthURL)

	// Read status code and close the body before acquiring the lock so that
	// no I/O happens while the mutex is held (CLAUDE.md mutex-scope rule).
	statusOK := false
	if err == nil {
		statusOK = resp.StatusCode == http.StatusOK
		_ = resp.Body.Close()
	}

	// Snapshot the process probe under the read lock and run it OUTSIDE the
	// lock: the probe takes the RuntimeProcess mutex, so calling it under h.mu
	// would invert the lock order.
	h.mu.RLock()
	probe := h.procAlive
	h.mu.RUnlock()
	procDead := probe != nil && !probe()

	h.mu.Lock()
	defer h.mu.Unlock()

	if procDead {
		h.unhealthyCount++
		if h.unhealthyCount >= h.config.HealthThreshold {
			h.healthy = false
		}
		h.notifyTransition(wasHealthy)
		return
	}

	if err != nil {
		h.unhealthyCount++
		if h.unhealthyCount >= h.config.HealthThreshold {
			h.healthy = false
		}
		h.notifyTransition(wasHealthy)
		return
	}

	if statusOK {
		h.unhealthyCount = 0
		h.healthy = true
	} else {
		h.unhealthyCount++
		if h.unhealthyCount >= h.config.HealthThreshold {
			h.healthy = false
		}
	}
	h.notifyTransition(wasHealthy)
}

func (h *HealthChecker) notifyTransition(wasHealthy bool) {
	if wasHealthy == h.healthy {
		return
	}
	if h.healthy {
		h.logger.Info("Runtime became healthy")
	} else {
		h.logger.Warn("Runtime became unhealthy", "consecutive_failures", h.unhealthyCount)
	}
	if h.onHealthChange != nil {
		cb := h.onHealthChange
		go cb(h.healthy)
	}
}

// Stop stops the health checker.
func (h *HealthChecker) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.stopped {
		close(h.stopCh)
		h.stopped = true
	}
}

// OnHealthChange sets a callback invoked on health state transitions.
func (h *HealthChecker) OnHealthChange(cb HealthChangeCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onHealthChange = cb
}

// IsHealthy returns true if the runtime is considered healthy based on recent checks.
func (h *HealthChecker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.healthy
}

// WaitForHealthy blocks until the runtime becomes healthy or the timeout is reached.
func (h *HealthChecker) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.IsHealthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Poll and retry
		}
	}
	return fmt.Errorf("timeout waiting for runtime to become healthy")
}
