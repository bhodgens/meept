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

func (h *HealthChecker) checkOnce() {
	h.mu.RLock()
	wasHealthy := h.healthy
	h.mu.RUnlock()

	// Construct health URL from server root, not API base URL.
	// This handles baseURL like "http://host:port/v1" where health is at "/health".
	healthURL := h.baseURL + h.config.HealthEndpoint
	if parsed, err := url.Parse(h.baseURL); err == nil && parsed.Path != "" {
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

	h.mu.Lock()
	defer h.mu.Unlock()

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
