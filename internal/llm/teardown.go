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
	"sort"
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
	// SweepTeardown is the daemon-shutdown path: every session with a
	// remembered endpoint gets its release, WITHOUT the live-session
	// reference count. At shutdown all sessions end simultaneously, so
	// per-session refcounting would make each session see the others and
	// nothing would erase (observed live: 2 sessions on one endpoint
	// produced 0 erases); the runtimes stop right after the sweep anyway.
	SweepTeardown(ctx context.Context, sessionID, endpointKey string) error
}

// llamaEndpointPrefix is the runtime prefix ComputeEndpointKey uses for
// llama.cpp endpoints ("llama-cpp:<host>:<port>"). Both the daemon's
// registered lifecycle configs and the synthetic local-models provider
// (register_local_model.go) carry it, so it identifies every endpoint
// LlamaServerTeardown may erase.
const llamaEndpointPrefix = string(RuntimeLlamaCpp) + ":"

// isLlamaSnapshotKey reports whether endpointKey has the RESOLVER
// SNAPSHOT shape EndpointKey(cfg) = "<host>|<credential>" whose host part
// resolves to a known llama runtime. The resolver's session→endpoint map
// (the teardown coordinator's only key source) stores exactly this shape
// — the runtime-key prefix check alone never matched it, so in a live
// daemon the llama adapter never fired and every erase fell to
// CloudTeardown's no-op (leaf 07 GAP B). A snapshot key is llama-shaped
// when RuntimeManager.TeardownBaseURL can resolve its host portion to a
// registered endpoint; a nil manager cannot answer, so the key claims
// llama ownership only if its credential part is non-empty AND the host
// is not resolvable as a cloud endpoint — resolved conservatively here as
// "has a host|credential shape"; the actual erase still refuses unknown
// endpoints inside Teardown (TeardownBaseURL miss -> nothing to erase).
// The credential part ("provider:key:<hex>" / "provider:oauth:<x>" /
// "provider:default") never contains "|", so the split is unambiguous.
func isLlamaSnapshotKey(endpointKey string, manager endpointBaseURLer) bool {
	host, cred, ok := strings.Cut(endpointKey, "|")
	if !ok || host == "" || cred == "" {
		return false
	}
	if manager == nil {
		return false
	}
	_, resolvable := manager.TeardownBaseURL(endpointKey)
	return resolvable
}

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
// coordinator only ever sees keys, not provider ids).
//
// The coordinator's keys come from the Resolver snapshot, whose shape is
// EndpointKey(cfg) = "host|credential" (models.go) — NOT the runtime key
// "llama-cpp:<host>:<port>" this map is indexed by. Two shapes, one host:
// so resolution falls back to a HOST match over registered endpoints. The
// fallback is safe for the teardown use: erasing the KV cache of the one
// llama-server listening on that host is the intent. NOTE the snapshot's
// host portion DOES carry the port — EndpointKey stores endpointHost's
// host:PORT — and the host-fallback comparison below relies on it, so
// two llama runtimes on one host are disambiguated by the snapshot key's
// port; only legacy port-less keys fall to the bare-hostname compare
// (first registered wins).
// A nil manager or an unknown endpoint returns false: a down or
// unregistered runtime has no cache worth erasing, and the erase must
// never guess a base URL.
func (m *RuntimeManager) TeardownBaseURL(endpointKey string) (string, bool) {
	if m == nil || endpointKey == "" {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Exact runtime-key match first (test stubs and any future caller
	// that stores runtime-shaped keys). An endpoint registered under
	// this exact key resolves even with NO provider mapping: requiring
	// one (endpointProviderLocked returns the key itself on a miss)
	// made runtime-shaped keys without a mapping fall through to the
	// host fallback, which cannot match the "llama-cpp:" prefix and
	// returned false.
	if ep, ok := m.endpoints[endpointKey]; ok && ep != nil && ep.hc != nil {
		return ep.hc.baseURL, ep.hc.baseURL != ""
	}
	// Host fallback: EndpointKey(cfg) snapshot keys are "host|credential".
	// Extract the host portion and match it against registered endpoints'
	// health-checker base URLs.
	host := endpointKey
	if i := strings.IndexByte(host, '|'); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return "", false
	}
	// Deterministic choice when several endpoints share the host: sort
	// keys. A host with multiple llama runtimes cannot be disambiguated
	// from a port-less key; the first registered wins and the erase still
	// targets a live runtime on that host.
	//
	// Comparison identity: EndpointKey(cfg) stores url.Host (host:PORT —
	// models.go endpointHost), so the registered side must be compared in
	// the same shape. hostPortFromBaseURL alone returns the BARE hostname
	// and never matched a port-bearing snapshot host — the GAP B remnant
	// that kept Handles() false for every live llama endpoint (the erase
	// fell to CloudTeardown's no-op). endpointHost yields host:port for
	// both sides; the bare-hostname comparison is kept as a fallback for
	// port-less legacy keys.
	keys := make([]string, 0, len(m.endpoints))
	for k, ep := range m.endpoints {
		if ep == nil || ep.hc == nil || ep.hc.baseURL == "" {
			continue
		}
		eHost := endpointHost(ep.hc.baseURL)
		eHostOnly, _ := hostPortFromBaseURL(ep.hc.baseURL)
		if eHost == host || eHostOnly == host {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "", false
	}
	sort.Strings(keys)
	return m.endpoints[keys[0]].hc.baseURL, true
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

// Handles reports whether endpointKey is a llama.cpp endpoint — either a
// runtime-shaped key ("llama-cpp:<host>:<port>", ComputeEndpointKey) or a
// resolver-snapshot key ("<host>|<credential>", EndpointKey(cfg)) whose
// host resolves to a registered live llama runtime via the manager. The
// snapshot shape is what the teardown coordinator actually receives from
// the resolver's session→endpoint map (leaf 07 GAP B: the prefix check
// alone never matched it, so the llama adapter never fired in a live
// daemon); an unresolvable key claims nothing, and the erase still
// refuses unknown endpoints inside Teardown.
func (t *LlamaServerTeardown) Handles(endpointKey string) bool {
	// Nil check FIRST: a typed-nil receiver must claim nothing — the
	// prefix check below answers from constants alone, so without this
	// ordering a nil *LlamaServerTeardown still claimed every
	// runtime-shaped key.
	if t == nil {
		return false
	}
	if strings.HasPrefix(endpointKey, llamaEndpointPrefix) {
		return true
	}
	if !strings.Contains(endpointKey, "|") {
		return false
	}
	return isLlamaSnapshotKey(endpointKey, t.manager)
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

// Teardown POSTs the erase route when sessionID is the last session on the
// endpoint (live-session reference count over the resolver snapshot).
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
	return t.erase(ctx, sessionID, endpointKey)
}

// SweepTeardown is the shutdown path: no reference count (all sessions end
// together; see the interface comment). One erase per session with a
// remembered endpoint.
func (t *LlamaServerTeardown) SweepTeardown(ctx context.Context, sessionID, endpointKey string) error {
	if t == nil || !t.Handles(endpointKey) {
		return nil
	}
	return t.erase(ctx, sessionID, endpointKey)
}

// erase resolves the endpoint's base URL and POSTs the erase route. Shared
// by the live-event and sweep paths; every skip/error is logged by the
// callers or returned for the coordinator's best-effort handling.
func (t *LlamaServerTeardown) erase(ctx context.Context, sessionID, endpointKey string) error {
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
	// The provider baseURL carries the OpenAI-compatible version prefix
	// ("/v1") meept's chat clients need; llama-server's slot route lives at
	// the server ROOT (pinned route registration above). Strip the version
	// suffix before appending the route — POST <base>/v1/slots/0 answers
	// 404 (observed live by the session-teardown e2e).
	baseForRoute := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	url := baseForRoute + llamaServerEraseRoute
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
// llama.cpp runtime (or a resolver-snapshot key that resolves to a live
// llama runtime — see LlamaServerTeardown.Handles, GAP B) is a cloud/API
// endpoint as far as teardown cares. The llama check here is prefix-only:
// the cloud adapter never holds the runtime manager, so a snapshot key
// this adapter sees is by definition not a resolvable llama endpoint when
// the llama adapter (wired with the manager) declined it first — the
// coordinator consults adapters in order and the llama adapter precedes
// the cloud one in the daemon wiring (components.go).
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

// SweepTeardown mirrors Teardown: cloud endpoints have nothing to release,
// at shutdown or any time.
func (t *CloudTeardown) SweepTeardown(ctx context.Context, sessionID, endpointKey string) error {
	return t.Teardown(ctx, sessionID, endpointKey)
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
	// identityResolve resolves a session id to its OTHER identity
	// (primary id -> conversation id). The snapshot keys on the ACCOUNTING
	// id (the WithTaskScope value, conv-* for interactive turns) while the
	// store's lifecycle hook fires with the PRIMARY id (session-*) — the
	// documented session_id/conversation_id duality. On a snapshot miss
	// under the given id the coordinator asks this seam once and retries
	// with the returned identity. Nil = single-identity lookups only.
	// Guarded by identityMu (dispatchAsync goroutines read it while the
	// daemon wiring may still call SetIdentityResolver).
	identityResolve func(sessionID string) (string, bool)
	identityMu      sync.RWMutex
	timeout         time.Duration
	logger          *slog.Logger

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
// the LRU) -> nothing to tear down. The duality rule (root AGENTS.md):
// the lookup resolves BOTH identities — the id as given, then (on miss)
// the alternate identity via identityResolve — because the snapshot keys
// on the accounting id while lifecycle events arrive with the primary id.
// The THIRD return is the identity the snapshot actually keys the session
// under (the resolved alternate on a duality hit): refcounting and logs
// must use it, or the session's own snapshot row reads as "another
// session on this endpoint" and the erase skips itself.
func (c *TeardownCoordinator) endpointForSession(sessionID string) (string, string, bool) {
	if c.endpointsFn == nil {
		return "", sessionID, false
	}
	snapshot := c.endpointsFn()
	key, ok := snapshot[sessionID]
	if ok && key != "" {
		return key, sessionID, true
	}
	// Copy the resolver under identityMu, then CALL it with no lock held
	// (the seam may take the session store's lock; the coordinator's
	// lock-order rule forbids holding any of its own mutexes across an
	// outside call).
	c.identityMu.RLock()
	resolve := c.identityResolve
	c.identityMu.RUnlock()
	if resolve != nil {
		if alt, found := resolve(sessionID); found && alt != "" && alt != sessionID {
			if key, ok = snapshot[alt]; ok && key != "" {
				return key, alt, true
			}
		}
	}
	return "", sessionID, false
}

// SetIdentityResolver wires the primary-id -> conversation-id seam used on
// a snapshot miss (the session_id/conversation_id duality: lifecycle events
// carry the primary id, the snapshot keys on the accounting id). Nil
// disables the retry. Safe to call while dispatchAsync goroutines are in
// flight: the write is guarded by identityMu, the same lock the dispatch
// readers take.
func (c *TeardownCoordinator) SetIdentityResolver(resolve func(sessionID string) (string, bool)) {
	if c == nil || resolve == nil {
		return
	}
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	c.identityResolve = resolve
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
	endpointKey, snapshotID, ok := c.endpointForSession(sessionID)
	if !ok {
		c.logger.Debug("teardown: session has no remembered endpoint",
			"session_id", sessionID)
		return
	}
	// snapshotID is the identity the snapshot keys this session under
	// (differs from sessionID on a duality hit): the adapter's reference
	// count scans the snapshot for OTHER sessions on the endpoint, so the
	// dispatch id must match the snapshot's keying or the session excludes
	// itself and the erase skips.
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
			return adapter.Teardown(callCtx, snapshotID, endpointKey)
		}()
		cancel()
		if err != nil {
			// Best-effort by contract: log and continue. An erase
			// failure never fails the archive/delete caller.
			c.logger.Warn("teardown: adapter failed",
				"session_id", sessionID,
				"snapshot_id", snapshotID,
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
// SYNCHRONOUSLY, under a bounded budget. The total worst case is
// 2 * TeardownTimeout * min(sweepMaxSessions, len(sessions)): the
// in-flight wait and the sweep itself are each bounded by the full
// sweepBudget, and the wait can consume it entirely before the sweep
// starts its own (same-sized) window. Splitting the budget was rejected:
// it would halve the erase window for every healthy shutdown to fix a
// bound that only a stuck in-flight adapter ever approaches, and the
// shutdown contract is "always completes", not "completes in exactly N".
// Errors and overruns are logged, never returned to the shutdown path.
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
				// Sweep path: NO live-session reference count — every
				// session ends together at shutdown, so refcounting would
				// skip every erase (each session sees the others).
				return adapter.SweepTeardown(callCtx, sessionID, endpointKey)
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
