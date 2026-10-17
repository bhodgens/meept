// teardown.go releases upstream provider resources when a session ends
// (session-affinity tree leaf 06, master Contract 6). Local llama.cpp
// endpoints get a KV-cache erase POST; cloud providers get an explicit
// no-op type so the "cloud has nothing to release" decision is visible in
// code, not prose. The TeardownCoordinator subscribes to the session
// store's SessionLifecycleHook (Contract 5) and fans out async per event;
// daemon shutdown runs a synchronous, budget-bounded sweep.
//
// Lock-order rule: the coordinator never holds its own mutex while calling
// into the resolver, the runtime manager, or an adapter. It takes
// SNAPSHOTS (resolver.SessionEndpoints, manager lookups) under no lock at
// all — its own mutex only guards its small bookkeeping maps — so no lock
// ordering cycle with the resolver/store locks can exist. Teardown is
// best-effort by contract: no error from this file can fail a turn, an
// archive, or a shutdown.
package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// llamaServerEraseRoute is the llama-server KV-cache slot-erase route,
// appended verbatim to the endpoint's base URL. VERIFIED against the
// pinned llama.cpp checkout ($MEEPT_HOME/deps/llama.cpp):
//   - route registration: tools/server/server.cpp:285-286 —
//     `ctx_http.post("/slots/:id_slot", ...)` (GET /slots lists, POST
//     /slots/<id> acts, with an optional ?action= query param);
//   - action dispatch: tools/server/server-context.cpp:4778-4798 —
//     post_slots reads `action` (save/restore/erase) and dispatches
//     handle_slots_erase;
//   - unit test: tools/server/tests/unit/test_slot_save.py:147 —
//     `POST /slots/1?action=erase` returns 200 and clears the slot's
//     cached tokens.
//
// Slot ids wrap (get_slot_by_id, server-context.cpp:1520), and the
// default llama-server has exactly one slot (n_parallel defaults to 1,
// common/common.h:455), so erasing slot 0 clears the KV cache the daemon
// serves. The route lives in this ONE constant; the adapter is its only
// consumer. Requires `--slot-save-path` on the spawned llama-server
// (post_slots returns 501 NOT_SUPPORTED without it) — meept's managed
// spawn commands add it alongside the model flags.
const llamaServerEraseRoute = "/slots/0?action=erase"

// TeardownAdapter releases upstream resources for one session.
type TeardownAdapter interface {
	// Handles reports whether this adapter owns the endpoint kind.
	Handles(endpointKey string) bool
	// Teardown is best-effort: bounded by TeardownTimeout, errors are
	// returned not panicked; callers log and continue.
	Teardown(ctx context.Context, sessionID, endpointKey string) error
}

// llamaEndpointPrefix is the runtime prefix ComputeEndpointKey uses for
// llama.cpp endpoints ("llama-cpp:<host>:<port>"). Both the daemon's
// registered lifecycle configs and the synthetic local-models provider
// (register_local_model.go) carry it, so it identifies every endpoint
// LlamaServerTeardown may erase.
const llamaEndpointPrefix = string(RuntimeLlamaCpp) + ":"

// localModelsEndpointKey is the endpoint key the synthetic local-models
// provider registers (register_local_model.go: llmLocalEndpointKey). The
// literal is duplicated as a teardown-local constant so teardown does not
// reach into registration internals; both must stay in sync (a drift is
// caught by the Handles test over ComputeEndpointKey output).
const localModelsEndpointKey = "llama-cpp:127.0.0.1"

// endpointBaseURLer resolves an endpoint key to a base URL for teardown.
// *RuntimeManager satisfies it; a nil resolver or an unresolvable key
// skips the erase (nothing known to erase against).
type endpointBaseURLer interface {
	// TeardownBaseURL resolves endpointKey -> base URL. ok=false means
	// the endpoint is not a known live runtime (never erase blindly).
	TeardownBaseURL(endpointKey string) (string, bool)
}

// TeardownBaseURL resolves a runtime endpoint key to its base URL via the
// provider registered against it (Contract 6's teardown accessor). The
// lookup mirrors EndpointBaseURL but keys on the ENDPOINT (the teardown
// coordinator only ever sees keys, not provider ids). A nil manager or an
// unknown endpoint returns false: a down or unregistered runtime has no
// cache worth erasing, and the erase must never guess a base URL.
func (m *RuntimeManager) TeardownBaseURL(endpointKey string) (string, bool) {
	if m == nil || endpointKey == "" {
		return "", false
	}
	m.mu.Lock()
	providerID := m.endpointProviderLocked(endpointKey)
	ep, ok := m.endpoints[endpointKey]
	var baseURL string
	if ok && ep != nil && ep.hc != nil {
		baseURL = ep.hc.baseURL
	}
	m.mu.Unlock()
	if providerID == "" || baseURL == "" {
		return "", false
	}
	return baseURL, true
}

// LlamaServerTeardown erases the KV cache of a local llama-server endpoint
// when the LAST session using it goes away. Endpoints are shared between
// sessions: the skip decision (reference count over the resolver's
// session→endpoint snapshot) lives HERE so every erase path — lifecycle
// event or shutdown sweep — inherits it.
type LlamaServerTeardown struct {
	// manager resolves endpoint keys to base URLs. May be nil (skips).
	manager endpointBaseURLer
	// endpointsFn returns the session→endpoint snapshot used for
	// reference counting (the resolver's SessionEndpoints). May be nil
	// (nothing else holds the endpoint -> always erase).
	endpointsFn func() map[string]string
	// timeout bounds each erase HTTP call.
	timeout time.Duration
	client  *http.Client
	logger  *slog.Logger
}

// NewLlamaServerTeardown builds the llama-server teardown adapter.
// timeout <= 0 falls back to DefaultSessionAffinityTeardownTimeout's
// daemon-wired value (the llm.session_affinity default, 5s).
func NewLlamaServerTeardown(manager *RuntimeManager, endpointsFn func() map[string]string, timeout time.Duration, logger *slog.Logger) *LlamaServerTeardown {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &LlamaServerTeardown{
		manager:     manager,
		endpointsFn: endpointsFn,
		timeout:     timeout,
		client:      &http.Client{Timeout: timeout},
		logger:      logger,
	}
}

// Handles reports whether endpointKey is a llama.cpp runtime endpoint
// (the only kind with a KV cache to erase).
func (t *LlamaServerTeardown) Handles(endpointKey string) bool {
	return strings.HasPrefix(endpointKey, llamaEndpointPrefix)
}

// otherSessionsOnEndpoint counts sessions — other than sessionID — still
// mapped to endpointKey in the snapshot.
func (t *LlamaServerTeardown) otherSessionsOnEndpoint(sessionID, endpointKey string, snapshot map[string]string) int {
	n := 0
	for sid, key := range snapshot {
		if sid != sessionID && key == endpointKey {
			n++
		}
	}
	return n
}

// Teardown POSTs the erase route to the endpoint's base URL when sessionID
// is the last session on it. Skips (nil) when other sessions still map to
// the same endpoint (never erase a live session's cache out from under it)
// or when the endpoint is not a currently-known runtime. The HTTP call is
// bounded by the configured timeout; every skip and every error is logged,
// never propagated to a turn or an archive.
func (t *LlamaServerTeardown) Teardown(ctx context.Context, sessionID, endpointKey string) error {
	if t == nil || !t.Handles(endpointKey) {
		return nil
	}
	// Reference count from the resolver snapshot: another live session
	// still maps here -> skip. Snapshot taken under NO coordinator lock
	// (SessionEndpoints returns a copy) so no lock cycle is possible.
	if t.endpointsFn != nil {
		snapshot := t.endpointsFn()
		if n := t.otherSessionsOnEndpoint(sessionID, endpointKey, snapshot); n > 0 {
			t.logger.Debug("teardown: skipping llama-server erase, endpoint still in use",
				"session_id", sessionID,
				"endpoint_key", endpointKey,
				"other_sessions", n)
			return nil
		}
	}
	if t.manager == nil {
		return nil
	}
	baseURL, ok := t.manager.TeardownBaseURL(endpointKey)
	if !ok || baseURL == "" {
		t.logger.Debug("teardown: no live runtime endpoint to erase",
			"session_id", sessionID,
			"endpoint_key", endpointKey)
		return nil
	}
	url := strings.TrimRight(baseURL, "/") + llamaServerEraseRoute
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("teardown: build erase request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		// Context deadline (shutdown budget or per-call timeout): the
		// bounded-error case Contract 6 describes.
		return fmt.Errorf("teardown: erase %s: %w", endpointKey, err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort drain
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("teardown: erase %s: status %d", endpointKey, resp.StatusCode)
	}
	t.logger.Info("teardown: llama-server kv cache erased",
		"session_id", sessionID,
		"endpoint_key", endpointKey)
	return nil
}

// CloudTeardown is the explicit no-op for cloud providers (OpenAI,
// Anthropic, Codex, ...): no upstream session API exists, so there is
// nothing to release. The type exists so the decision is visible in code
// (Contract 6), not folded into a silent default.
type CloudTeardown struct {
	logger *slog.Logger
}

// NewCloudTeardown builds the cloud no-op adapter.
func NewCloudTeardown(logger *slog.Logger) *CloudTeardown {
	if logger == nil {
		logger = slog.Default()
	}
	return &CloudTeardown{logger: logger}
}

// Handles matches every non-llama endpoint key: anything that is not a
// llama.cpp runtime is a cloud/API endpoint as far as teardown cares.
func (t *CloudTeardown) Handles(endpointKey string) bool {
	return endpointKey != "" && !strings.HasPrefix(endpointKey, llamaEndpointPrefix)
}

// Teardown is a no-op returning nil. It logs at Debug (never Info: a
// no-op per archive would be log spam) and never fails.
func (t *CloudTeardown) Teardown(_ context.Context, sessionID, endpointKey string) error {
	if t != nil && t.logger != nil {
		t.logger.Debug("teardown: cloud endpoint, nothing to release",
			"session_id", sessionID,
			"endpoint_key", endpointKey)
	}
	return nil
}

// TeardownCoordinator fans session lifecycle events out to the teardown
// adapters. It implements session.SessionLifecycleHook — the internal/llm
// package cannot import internal/session (import cycle: internal/session
// is imported by packages llm depends on via the daemon graph), so the
// daemon wiring adapts the narrow OnSessionEvent shape through the small
// adapter in this file (coordinatorHook).
//
// OnSessionEvent is ASYNC per event (a goroutine bounded by
// TeardownTimeout): the store invokes the hook on its archive path and
// must not block on HTTP. ShutdownSweep is the synchronous counterpart for
// daemon shutdown.
type TeardownCoordinator struct {
	adapters []TeardownAdapter
	// endpointsFn returns the session→endpoint snapshot (resolver).
	endpointsFn func() map[string]string
	timeout     time.Duration
	logger      *slog.Logger

	// mu guards inFlight only. NEVER held across an adapter call or a
	// resolver/manager method (lock-order rule: snapshots only).
	mu       sync.Mutex
	inFlight sync.WaitGroup
}

// NewTeardownCoordinator builds the coordinator over its adapters.
// timeout <= 0 falls back to the 5s llm.session_affinity default.
func NewTeardownCoordinator(adapters []TeardownAdapter, endpointsFn func() map[string]string, timeout time.Duration, logger *slog.Logger) *TeardownCoordinator {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &TeardownCoordinator{
		adapters:    adapters,
		endpointsFn: endpointsFn,
		timeout:     timeout,
		logger:      logger,
	}
}

// endpointForSession resolves the endpoint key a session last used from
// the snapshot. Missing session (never served, or already evicted from
// the LRU) -> nothing to tear down.
func (c *TeardownCoordinator) endpointForSession(sessionID string) (string, bool) {
	if c.endpointsFn == nil {
		return "", false
	}
	snapshot := c.endpointsFn()
	key, ok := snapshot[sessionID]
	return key, ok && key != ""
}

// OnSessionEvent implements session.SessionLifecycleHook for daemon wiring
// (the event parameter types as session.SessionEvent at the call site; the
// plain string here keeps internal/llm free of the internal/session
// import, which would cycle — internal/session already imports internal/
// llm for its summarizer bridge). The event kind is deliberately not
// differentiated: archive, delete, and idle ALL release the session's
// upstream resources; the endpoint snapshot lookup is the only gate.
func (c *TeardownCoordinator) OnSessionEvent(ctx context.Context, sessionID string, event string) {
	c.dispatchAsync(sessionID)
}

// TeardownHookFunc adapts a plain function to the session store's
// SessionLifecycleHook interface for the daemon wiring (the coordinator
// takes a string event; the store's interface carries
// session.SessionEvent, which internal/llm cannot name — the daemon
// converts with string(event) and wraps the closure in this type).
type TeardownHookFunc func(ctx context.Context, sessionID string, event string)

// OnSessionEvent satisfies the store-side hook shape.
func (f TeardownHookFunc) OnSessionEvent(ctx context.Context, sessionID string, event string) {
	if f == nil {
		return
	}
	f(ctx, sessionID, event)
}

// dispatch performs the actual fan-out (shared by the hook adapter and the
// sweep). endpointKey is re-resolved per event from the snapshot — the
// accounting session ID is the sticky map's key (leaf 01's WithTaskScope
// value), so the lookup is exactly the key the preference was remembered
// under.
func (c *TeardownCoordinator) dispatch(ctx context.Context, sessionID string) {
	endpointKey, ok := c.endpointForSession(sessionID)
	if !ok {
		c.logger.Debug("teardown: session has no remembered endpoint",
			"session_id", sessionID)
		return
	}
	for _, adapter := range c.adapters {
		if adapter == nil || !adapter.Handles(endpointKey) {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("teardown adapter panicked: %v", r)
				}
			}()
			return adapter.Teardown(callCtx, sessionID, endpointKey)
		}()
		cancel()
		if err != nil {
			// Best-effort by contract: log and continue. An erase
			// failure never fails the archive/delete caller.
			c.logger.Warn("teardown: adapter failed",
				"session_id", sessionID,
				"endpoint_key", endpointKey,
				"error", err)
		}
	}
}

// dispatchAsync runs dispatch on its own goroutine and registers it with
// inFlight so ShutdownSweep can wait for stragglers.
func (c *TeardownCoordinator) dispatchAsync(sessionID string) {
	c.mu.Lock()
	c.inFlight.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.inFlight.Done()
		c.dispatch(context.Background(), sessionID)
	}()
}

// ShutdownSweep tears down every session with a remembered endpoint,
// SYNCHRONOUSLY, under a total budget of TeardownTimeout *
// min(sweepMaxSessions, len(sessions)). Errors and overruns are logged,
// never returned to the shutdown path (shutdown must always complete).
// Sessions without a remembered endpoint are skipped (nothing to release).
func (c *TeardownCoordinator) ShutdownSweep(ctx context.Context) {
	if c == nil {
		return
	}
	sweepBudget := c.sweepBudget()
	// Wait (bounded) for lifecycle-event goroutines still in flight so
	// the sweep is the final word on teardown before runtimes stop.
	waitCtx, cancel := context.WithTimeout(ctx, sweepBudget)
	defer cancel()
	waitDone := make(chan struct{})
	go func() {
		c.inFlight.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-waitCtx.Done():
		c.logger.Warn("teardown: shutdown sweep budget exceeded waiting for in-flight events",
			"budget", sweepBudget.String())
	}
	if c.endpointsFn == nil {
		return
	}
	snapshot := c.endpointsFn()
	if len(snapshot) == 0 {
		return
	}
	sweepCtx, sweepCancel := context.WithTimeout(ctx, sweepBudget)
	defer sweepCancel()
	for sessionID, endpointKey := range snapshot {
		if endpointKey == "" {
			continue
		}
		for _, adapter := range c.adapters {
			if adapter == nil || !adapter.Handles(endpointKey) {
				continue
			}
			if err := sweepCtx.Err(); err != nil {
				c.logger.Warn("teardown: shutdown sweep budget exhausted, abandoning rest",
					"budget", sweepBudget.String(),
					"sessions_total", len(snapshot))
				return
			}
			callCtx, cancel := context.WithTimeout(sweepCtx, c.timeout)
			err := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("teardown adapter panicked: %v", r)
					}
				}()
				return adapter.Teardown(callCtx, sessionID, endpointKey)
			}()
			cancel()
			if err != nil {
				c.logger.Warn("teardown: sweep adapter failed",
					"session_id", sessionID,
					"endpoint_key", endpointKey,
					"error", err)
			}
		}
	}
}

// sweepMaxSessions caps the shutdown budget multiplier (Contract 6:
// TeardownTimeout * min(8, len(sessions))) — more than 8 sessions still
// cost at most 8 timeouts, keeping shutdown bounded regardless of history.
const sweepMaxSessions = 8

// sweepBudget computes the total sweep budget.
func (c *TeardownCoordinator) sweepBudget() time.Duration {
	n := 0
	if c.endpointsFn != nil {
		n = len(c.endpointsFn())
	}
	if n > sweepMaxSessions {
		n = sweepMaxSessions
	}
	if n < 1 {
		n = 1
	}
	return c.timeout * time.Duration(n)
}

// LifecycleEvent mirrors session.SessionEvent for the internal/llm side of
// the hook seam. Values match the internal/session constants 1:1; the
// daemon wiring converts. (internal/llm cannot import internal/session.)
type LifecycleEvent string

const (
	// LifecycleArchive mirrors session.SessionEventArchive.
	LifecycleArchive LifecycleEvent = "archive"
	// LifecycleDelete mirrors session.SessionEventDelete.
	LifecycleDelete LifecycleEvent = "delete"
	// LifecycleIdle mirrors session.SessionEventIdle.
	LifecycleIdle LifecycleEvent = "idle"
)

// CoordinatorHook adapts *TeardownCoordinator to a string-typed
// OnSessionEvent hook (structural seam for callers whose event type is a
// plain string; the daemon wires the coordinator DIRECTLY — its
// OnSessionEvent already matches session.SessionLifecycleHook's shape).
// The event kind is deliberately not differentiated: archive, delete, and
// idle ALL release the session's upstream resources.
type CoordinatorHook struct {
	Coordinator *TeardownCoordinator
}

// OnSessionEvent forwards any event kind to the coordinator's async
// dispatch.
func (h *CoordinatorHook) OnSessionEvent(ctx context.Context, sessionID string, event string) {
	if h == nil || h.Coordinator == nil {
		return
	}
	h.Coordinator.OnSessionEvent(ctx, sessionID, event)
}
