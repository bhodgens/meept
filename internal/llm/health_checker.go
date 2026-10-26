package llm

import (
	"bytes"
	"context"
	"encoding/json"
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
	// Canary lane (llama-hang-watchdog Contract 5). canaryClient is a PLAIN
	// http.Client by design: no Recorder, no Resolver, no slot gate — canary
	// probes are infrastructure, not turns, and must NEVER write an
	// llm_calls row in metrics.db (gate F9). canaryArmed is set on the first
	// /health 200 after Start (cold-load guard: no probing before the model
	// has answered healthy once). canaryFails counts CONSECUTIVE canary
	// failures and is separate from unhealthyCount (a canary failure must
	// not flip the /health verdict). canaryAccum accumulates HealthInterval
	// ticks in the single run goroutine until CanaryInterval is due.
	canaryClient   *http.Client
	canaryArmed    bool
	canaryFails    int
	canaryAccum    time.Duration
	mu             sync.RWMutex
	stopCh         chan struct{}
	running        bool
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
		config: cfg,
		client: &http.Client{Timeout: cfg.HealthTimeout},
		// Plain client, deliberately NOT the daemon's LLM client: no
		// Recorder/Resolver/slot gate — see the canaryClient field comment
		// (gate F9, ledger bypass).
		canaryClient: &http.Client{Timeout: cfg.CanaryTimeout},
		baseURL:      baseURL,
		stopCh:       make(chan struct{}),
		logger:       slog.Default().With("component", "health-checker"),
	}
}

// Start begins periodic health checks in a background goroutine.
//
// The run belongs to the ENDPOINT, not to the caller's context. Every caller
// passes a short-lived context — the daemon cancels its boot context the moment
// StartAll returns (`defer cancelLlm`), the HTTP start path passes
// `r.Context()`, and RPC passes a connection-scoped context — so binding the
// run to it silently ended health monitoring seconds after boot. The run
// detaches from cancellation the same way RuntimeProcess.Start detaches the
// child; only Stop ends it (and StopAll calls Stop for every endpoint).
//
// Start is also the re-arm path. The manager stops the checker when a runtime
// stops and starts it again when the runtime restarts, and a run can end on its
// own (an older caller's context died). Any checker that is not running is
// armed fresh — verdict reset to unhealthy, failure count cleared — because
// serving the previous verdict would either fail a healthy restart (and Stop
// would then kill the process it just restarted) or report a runtime healthy
// before its socket exists.
func (h *HealthChecker) Start(ctx context.Context) {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.stopCh = make(chan struct{})
	h.running = true
	h.unhealthyCount = 0
	h.healthy = false
	// Canary re-arm: a (re-)started checker must re-prove /health before any
	// canary probe fires (cold-load guard across restarts).
	h.canaryArmed = false
	h.canaryFails = 0
	h.canaryAccum = 0
	stopCh := h.stopCh
	h.mu.Unlock()
	go h.run(context.WithoutCancel(ctx), stopCh)
}

func (h *HealthChecker) run(ctx context.Context, stopCh <-chan struct{}) {
	defer h.finishRun(stopCh)

	ticker := time.NewTicker(h.config.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			h.checkOnce(stopCh)
			// Canary cadence rides the SAME run goroutine (no second
			// goroutine, so the generation guard and Stop semantics stay
			// intact): accumulate health ticks and fire when the canary
			// interval is due.
			if h.config.CanaryEnabled {
				h.mu.RLock()
				armed := h.canaryArmed
				h.mu.RUnlock()
				if armed {
					h.canaryAccum += h.config.HealthInterval
					if h.canaryAccum >= h.config.CanaryInterval {
						h.canaryAccum = 0
						h.runCanary(stopCh)
					}
				}
			}
		}
	}
}

// runCanary issues one Contract-5 inference canary probe against
// {server-root}/v1/chat/completions for the run identified by gen. Success =
// HTTP 200 AND a parseable response with a NON-EMPTY choices array; any
// transport error, timeout, non-200, unparseable body, or empty choices is
// one failure. Failures accumulate in canaryFails (SEPARATE from
// unhealthyCount — a canary failure never flips the /health verdict); a
// success resets the counter. All HTTP I/O and body parsing happen OUTSIDE
// h.mu (mutexio rule); only the counter update takes the lock, under the
// generation guard.
func (h *HealthChecker) runCanary(gen <-chan struct{}) {
	// Server-root derivation identical to checkOnce's healthURL logic.
	root := h.baseURL
	if parsed, err := url.Parse(h.baseURL); err == nil && parsed.Path != "" {
		root = fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	}
	canaryURL := root + "/v1/chat/completions"

	// Model id: prefer the authoritative provider model IDs (ModelKeys,
	// populated by RegisterConfig — the same resolved ids the classifier
	// boot gate consults); fall back to the model path.
	model := h.config.ModelPath
	if len(h.config.ModelKeys) > 0 {
		model = h.config.ModelKeys[0]
	}
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": h.config.CanaryPrompt},
		},
		"max_tokens": 1,
		"stream":     false,
	})
	if err != nil {
		// Marshal of a static map cannot fail; treat defensively as a
		// failure without I/O.
		h.recordCanaryResult(gen, false)
		return
	}

	resp, err := h.canaryClient.Post(canaryURL, "application/json", bytes.NewReader(body))
	ok := false
	if err == nil {
		var parsed struct {
			Choices []json.RawMessage `json:"choices"`
		}
		decErr := json.NewDecoder(resp.Body).Decode(&parsed)
		_ = resp.Body.Close()
		ok = resp.StatusCode == http.StatusOK && decErr == nil && len(parsed.Choices) > 0
		if !ok && decErr != nil {
			h.logger.Debug("canary response unparseable", "err", decErr)
		}
	} else {
		h.logger.Debug("canary probe transport error", "err", err)
	}
	h.recordCanaryResult(gen, ok)
}

// recordCanaryResult updates the consecutive-failure counter under h.mu with
// the generation guard (a late canary from a superseded run must not write
// into the current generation).
func (h *HealthChecker) recordCanaryResult(gen <-chan struct{}, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopCh != gen {
		return
	}
	if ok {
		h.canaryFails = 0
		return
	}
	h.canaryFails++
	if h.canaryFails >= h.config.CanaryThreshold {
		h.logger.Warn("canary probe failing at threshold",
			"consecutive_failures", h.canaryFails,
			"threshold", h.config.CanaryThreshold)
	}
}

// finishRun is a run goroutine's exit path. It clears the running flag ONLY
// when the run it belongs to is still the active one — the generation guard,
// mirroring checkOnce. A SUPERSEDED goroutine (Stop→Start re-armed a new run
// before this one noticed) must not clear the active run's flag: doing so makes
// Stop a no-op for that run (so it can never be stopped, StopAll included) and
// lets further Starts arm concurrent, unstoppable runs (audit finding F60).
func (h *HealthChecker) finishRun(stopCh <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopCh == stopCh {
		h.running = false
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

// checkOnce performs one health check for the run identified by gen (its stop
// channel). A check that began before Stop or before a re-arm must not write its
// verdict into the current generation: the process it measured may already be
// gone, and a late "healthy" from the previous generation is exactly the
// false-healthy report the process probe exists to prevent.
func (h *HealthChecker) checkOnce(gen <-chan struct{}) {
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

	// Generation guard: see checkOnce's doc comment.
	if h.stopCh != gen {
		return
	}

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
		// Cold-load guard release: the first /health 200 since (re-)arm
		// arms the canary lane. The existing /health verdict logic above is
		// unchanged; canary failures count separately (canaryFails).
		if h.config.CanaryEnabled {
			h.canaryArmed = true
		}
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

// Stop stops the health checker. Idempotent, and safe against the run goroutine
// exiting on its own: it closes the channel of the run this checker believes is
// active.
//
// It gates on the CHANNEL, not on the running flag: a superseded goroutine can
// never leave a newer run's channel here (finishRun's generation guard), so
// whatever h.stopCh names is either the live run or an already-exited one, and
// closing it is safe either way — no run ever closes its own channel. Gating on
// the flag instead (the old behaviour) made Stop a no-op for a run whose flag a
// stale goroutine had cleared, leaving it live and unstoppable (finding F60).
func (h *HealthChecker) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.stopCh == nil {
		return
	}
	close(h.stopCh) //nolint:channil // nil is the idempotence sentinel; all mutation under h.mu
	// The nil above is deliberate: Stop is idempotent, every stopCh mutation
	// happens under h.mu, and no sender selects on a nil channel (Start
	// installs a fresh one).
	h.stopCh = nil
	h.running = false
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
