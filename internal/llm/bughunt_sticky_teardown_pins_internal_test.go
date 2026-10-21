package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

// Regression pins for the 2026-10 sticky/teardown bughunt wave. Every test
// here FAILS against the pre-fix code (red-run evidence recorded in the fix
// report); together they pin: LRU re-remember integrity, the
// ChatWithProgress reporting seam, the read-side sticky hook (no shared
// cursor mutation), affinity-config locking, compare-and-forget on failure,
// the typed-nil Handles receiver, TeardownBaseURL exact match without a
// provider mapping, and SetIdentityResolver synchronization.

// bughuntLogger keeps noise out of test output.
func bughuntLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- item 1: LRU bookkeeping across re-remember ---

// TestStickyLRURerememberNoDuplicates pins the re-remember path: splicing
// a session's LRU slot must never leave stale sibling indexes behind (the
// old parallel order-map went stale, a later re-remember skipped removal,
// appended a DUPLICATE session id, and the next eviction deleted a live
// session's preference).
func TestStickyLRURerememberNoDuplicates(t *testing.T) {
	r := newStickyPrefResolver(t)
	keyA := EndpointKey(r.aliases["medium"].Models[0])
	keyB := EndpointKey(r.aliases["medium"].Models[1])

	r.RememberSessionEndpoint("sa", keyA)
	r.RememberSessionEndpoint("sb", keyB)
	r.RememberSessionEndpoint("sc", keyA)
	// Re-remember A (splices the front), then C (whose cached index went
	// stale in the old code).
	r.RememberSessionEndpoint("sa", keyA)
	r.RememberSessionEndpoint("sc", keyB)

	assertLRUConsistent(t, r)

	// Lowered cap: a just-re-remembered session must survive eviction —
	// the evicted slot belongs to the actual LRU front, not to a live
	// session whose duplicate id sat in the list.
	origCap := stickySessionEndpointCap
	stickySessionEndpointCap = 3
	defer func() { stickySessionEndpointCap = origCap }()

	r2 := newStickyPrefResolver(t)
	r2.RememberSessionEndpoint("s1", keyA)
	r2.RememberSessionEndpoint("s2", keyA)
	r2.RememberSessionEndpoint("s3", keyA)
	r2.RememberSessionEndpoint("s1", keyB) // refresh s1's recency
	r2.RememberSessionEndpoint("s4", keyA) // evicts the LRU front (s2)

	assertLRUConsistent(t, r2)
	if _, ok := r2.PreferredEndpoint("s1"); !ok {
		t.Fatal("just-re-remembered session s1 must survive eviction")
	}
	if _, ok := r2.PreferredEndpoint("s2"); ok {
		t.Fatal("LRU front (s2) must be the evicted session, not a live one")
	}
	if got, _ := r2.PreferredEndpoint("s1"); got != keyB {
		t.Fatalf("s1 preference = %q, want refreshed key %q", got, keyB)
	}
}

// assertLRUConsistent checks the map/list invariant: same length, no
// duplicate ids in the LRU slice, every id in the list present in the map.
func assertLRUConsistent(t *testing.T, r *Resolver) {
	t.Helper()
	if len(r.sessionEndpointLRU) != len(r.sessionEndpoints) {
		t.Fatalf("LRU/map divergence: lru=%d map=%d (lru=%v)",
			len(r.sessionEndpointLRU), len(r.sessionEndpoints), r.sessionEndpointLRU)
	}
	seen := make(map[string]bool, len(r.sessionEndpointLRU))
	for _, sid := range r.sessionEndpointLRU {
		if seen[sid] {
			t.Fatalf("duplicate session id %q in LRU list %v", sid, r.sessionEndpointLRU)
		}
		seen[sid] = true
		if _, ok := r.sessionEndpoints[sid]; !ok {
			t.Fatalf("LRU lists %q with no map entry", sid)
		}
	}
}

// --- item 2: ChatWithProgress reporting seam ---

// bughuntPMConfig builds a two-provider topology (distinct hosts => distinct
// EndpointKeys) shared by the resolver fixture and the PM pins below. The
// provider-level API keys are DISTINCT: the loader normalizes a model's key
// to its provider's, so identical keys would collapse both EndpointKeys'
// credential parts (host still differs, but keep identities honest).
func bughuntPMConfig() *ProvidersConfig {
	return &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"pa": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://127.0.0.1:11/v1",
					APIKey:  "***",
				},
				Models: map[string]ModelDef{
					"ma": {Name: "ma", Capabilities: []string{"completion"}},
				},
			},
			"pb": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://127.0.0.1:12/v1",
					APIKey:  "***",
				},
				Models: map[string]ModelDef{
					"mb": {Name: "mb", Capabilities: []string{"completion"}},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			"general": {Models: []string{"pa/ma", "pb/mb"}},
		},
	}
}

// bughuntResolverConfigs returns the resolver's OWN loaded alias models:
// PM entries built from these carry byte-identical EndpointKeys to the
// resolver's healthy-candidate gate (a hand-built ModelConfig would
// diverge in loader-normalized fields like the credential).
func bughuntResolverConfigs(t *testing.T, r *Resolver) (*ModelConfig, *ModelConfig) {
	t.Helper()
	models := r.aliases["general"].Models
	if len(models) != 2 {
		t.Fatalf("fixture alias must have 2 models, got %d", len(models))
	}
	return models[0], models[1]
}

// bughuntPMOver builds a one-provider PM whose single entry serves cfg
// through a stubChatter returning resp (nil resp) or err, with the
// bughunt resolver wired as the sticky resolver.
func bughuntPMOver(cfg *ModelConfig, r *Resolver, resp *Response, err error) *ProviderManager {
	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{cfg},
		Logger:    bughuntLogger(),
	})
	pm.SetStickyEndpointResolver(r)
	for _, entry := range pm.providers {
		entry.Chatter = &stubChatter{resp: resp, err: err}
	}
	return pm
}

// TestChatWithProgressReportsSessionEndpoint pins the missing success-path
// report: ChatWithProgress must remember the serving endpoint for the
// scoped session exactly like Chat and chatWithAttemptTaggedDeltas do.
func TestChatWithProgressReportsSessionEndpoint(t *testing.T) {
	r := NewResolver(bughuntPMConfig(), bughuntLogger())
	cfgA, _ := bughuntResolverConfigs(t, r)
	pm := bughuntPMOver(cfgA, r, &Response{Content: "ok"}, nil)

	resp, err := pm.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(ProgressStage, string) {},
		WithTaskScope("task-1", "sess-cwp"))
	if err != nil {
		t.Fatalf("ChatWithProgress: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	want := EndpointKey(cfgA)
	got, ok := r.PreferredEndpoint("sess-cwp")
	if !ok || got != want {
		t.Fatalf("ChatWithProgress did not report the session endpoint: got (%q,%v), want (%q,true)", got, ok, want)
	}
}

// --- item 3: sticky hook must not move the shared rotation cursor ---

// TestStickyHookDoesNotMoveSharedCursor pins the READ-side contract: a
// scoped resolve may serve the remembered endpoint, but the NEXT unscoped
// resolve must return the pre-hook rotation order (the shared
// AliasHealth.CurrentIndex is never repositioned by stickiness).
func TestStickyHookDoesNotMoveSharedCursor(t *testing.T) {
	r := newStickyPrefResolver(t) // alias "medium": [openai/gpt-a, xai/grok]
	modelB := r.aliases["medium"].Models[1]
	keyB := EndpointKey(modelB)
	r.RememberSessionEndpoint("sess-sticky", keyB)

	// Scoped resolve: the healthy remembered endpoint wins for THIS call.
	scoped, err := r.ResolveForAlias("medium", "sess-sticky")
	if err != nil {
		t.Fatalf("scoped resolve: %v", err)
	}
	if scoped.ProviderID != modelB.ProviderID || scoped.ModelID != modelB.ModelID {
		t.Fatalf("scoped resolve served %s/%s, want sticky %s/%s",
			scoped.ProviderID, scoped.ModelID, modelB.ProviderID, modelB.ModelID)
	}

	// Unscoped resolve: rotation cursor untouched by the hook above —
	// still the pre-hook head (gpt-a), NOT the sticky member.
	unscoped, err := r.ResolveForAlias("medium", "")
	if err != nil {
		t.Fatalf("unscoped resolve: %v", err)
	}
	if unscoped.ModelID != "gpt-a" {
		t.Fatalf("sticky hook leaked into the shared rotation cursor: unscoped resolve served %s/%s, want rotation head openai/gpt-a",
			unscoped.ProviderID, unscoped.ModelID)
	}

	// A different session without a preference also keeps rotation order.
	other, err := r.ResolveForAlias("medium", "sess-other")
	if err != nil {
		t.Fatalf("other-session resolve: %v", err)
	}
	if other.ModelID != "gpt-a" {
		t.Fatalf("other-session resolve served %s/%s, want gpt-a", other.ProviderID, other.ModelID)
	}
}

// bughuntRawModelConfig hand-builds a ModelConfig for pins that do NOT
// go through a resolver's healthy-candidate gate (client construction,
// TeardownBaseURL host matching) — those never compare against loaded
// alias models, so loader normalization is irrelevant here.
func bughuntRawModelConfig(providerID, host, key, modelID string) *ModelConfig {
	return &ModelConfig{
		ProviderID:  providerID,
		BaseURL:     "http://" + host + "/v1",
		APIKey:      key,
		ModelID:     modelID,
		ProviderAPI: "openai",
	}
}

// --- item 4: affinity config must be lock-guarded (race pin) ---

// TestSessionAffinityConfigRaceSafe exercises the SetSessionAffinityConfig
// writer against the applyAffinityHeader reader concurrently. Run under
// -race: the pre-fix unlocked write/read is a data race the detector
// reports; with configMu on both sides it is clean.
func TestSessionAffinityConfigRaceSafe(t *testing.T) {
	c := NewClient(bughuntRawModelConfig("pa", "127.0.0.1:11", "ka", "ma"))
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			c.SetSessionAffinityConfig(&AffinityConfig{Enabled: true, Header: "session_id"})
			c.SetSessionAffinityConfig(&AffinityConfig{Enabled: false, Header: "x"})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:11/v1/chat/completions", nil)
			if err != nil {
				t.Errorf("NewRequest: %v", err)
				return
			}
			c.applyAffinityHeader(req, "sess-race")
			if h := req.Header.Get("session_id"); h != "" && h != "sess-race" {
				t.Errorf("affinity header value corrupted: %q", h)
				return
			}
		}
		close(stop)
	}()

	wg.Wait()
}

// --- item 5: compare-and-forget on failure ---

// TestStickyForgetOnlyMatchingEndpoint pins the failure-side seam: a
// failed attempt against provider B must NOT wipe the session's
// preference for endpoint A; failing A itself must drop it.
func TestStickyForgetOnlyMatchingEndpoint(t *testing.T) {
	r := NewResolver(bughuntPMConfig(), bughuntLogger())
	cfgA, cfgB := bughuntResolverConfigs(t, r)
	keyA := EndpointKey(cfgA)

	r.RememberSessionEndpoint("sess-f", keyA)
	if got, ok := r.PreferredEndpoint("sess-f"); !ok || got != keyA {
		t.Fatalf("precondition: preference must be remembered (%q,%v)", got, ok)
	}

	// Fail against B: preference for A must survive.
	pmB := bughuntPMOver(cfgB, r, nil, errors.New("stub: provider B down"))
	if _, err := pmB.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-1", "sess-f")); err == nil {
		t.Fatal("expected B failure to surface")
	}
	if got, ok := r.PreferredEndpoint("sess-f"); !ok || got != keyA {
		t.Fatalf("failure against a DIFFERENT endpoint wiped the preference: got (%q,%v), want (%q,true)", got, ok, keyA)
	}

	// Fail against A itself: preference must drop.
	pmA := bughuntPMOver(cfgA, r, nil, errors.New("stub: provider A down"))
	if _, err := pmA.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-1", "sess-f")); err == nil {
		t.Fatal("expected A failure to surface")
	}
	if got, ok := r.PreferredEndpoint("sess-f"); ok {
		t.Fatalf("failure of the REMEMBERED endpoint must drop the preference, still got %q", got)
	}

	// Direct compare-and-forget semantics.
	keyB := EndpointKey(cfgB)
	r.RememberSessionEndpoint("sess-d", keyA)
	if r.ForgetSessionEndpointIf("sess-d", keyB) {
		t.Fatal("ForgetSessionEndpointIf matched a different key")
	}
	if _, ok := r.PreferredEndpoint("sess-d"); !ok {
		t.Fatal("non-matching forget must leave the preference intact")
	}
	if !r.ForgetSessionEndpointIf("sess-d", keyA) {
		t.Fatal("matching forget must report the drop")
	}
	if _, ok := r.PreferredEndpoint("sess-d"); ok {
		t.Fatal("matching forget must drop the preference")
	}
	var nilR *Resolver
	if nilR.ForgetSessionEndpointIf("s", keyA) {
		t.Fatal("nil receiver must no-op")
	}
	if r.ForgetSessionEndpointIf("sess-x", "") {
		t.Fatal("empty endpointKey must never match")
	}
}

// --- item 7: typed-nil Handles receiver ---

// TestLlamaHandlesTypedNilReceiver pins the nil-guard ordering: a typed-nil
// *LlamaServerTeardown must claim NOTHING, including runtime-shaped keys
// the constant-prefix check would otherwise accept before the nil guard.
func TestLlamaHandlesTypedNilReceiver(t *testing.T) {
	var lt *LlamaServerTeardown
	if lt.Handles("llama-cpp:127.0.0.1:8080") {
		t.Fatal("typed-nil receiver claimed a runtime-shaped key (nil guard runs after the prefix check)")
	}
	if lt.Handles("127.0.0.1:8080|pa:key:abc") {
		t.Fatal("typed-nil receiver claimed a snapshot key")
	}
	if lt.Handles("") {
		t.Fatal("typed-nil receiver claimed the empty key")
	}
	// The constructed adapter keeps claiming runtime keys.
	real := NewLlamaServerTeardown(nil, nil, time.Second, bughuntLogger())
	if !real.Handles("llama-cpp:127.0.0.1:8080") {
		t.Fatal("constructed adapter must still claim runtime-shaped keys")
	}
}

// --- item 9a: TeardownBaseURL exact match without a provider mapping ---

// TestTeardownBaseURLRuntimeKeyNoProviderMapping pins the exact-match
// branch: an endpoint registered under a runtime-shaped key resolves via
// TeardownBaseURL even when NO provider maps to it (endpointProviderLocked
// returns the key itself on a miss, and the old `providerID != endpointKey`
// condition let that miss defeat the exact match; the host fallback cannot
// save a "llama-cpp:"-prefixed key).
func TestTeardownBaseURLRuntimeKeyNoProviderMapping(t *testing.T) {
	mgr := NewRuntimeManager(bughuntLogger())
	const key = "llama-cpp:127.0.0.1:9100"
	mgr.mu.Lock()
	mgr.endpoints[key] = &endpointProcess{
		hc: NewHealthChecker(&RuntimeConfig{HealthTimeout: time.Second}, "http://127.0.0.1:9100/v1"),
	}
	mgr.mu.Unlock()

	got, ok := mgr.TeardownBaseURL(key)
	if !ok || got != "http://127.0.0.1:9100/v1" {
		t.Fatalf("TeardownBaseURL(%q) = (%q,%v), want (http://127.0.0.1:9100/v1,true) — runtime-shaped key without a provider mapping must resolve via exact match", key, got, ok)
	}

	// The b1af5ce6 host-fallback behavior stays intact: a snapshot-shaped
	// key still resolves through the host:PORT comparison.
	snapKey := EndpointKey(bughuntRawModelConfig("pa", "127.0.0.1:9100", "ka", "ma"))
	if _, ok := mgr.TeardownBaseURL(snapKey); !ok {
		t.Fatalf("host fallback regressed: snapshot key %q no longer resolves", snapKey)
	}
}

// --- item 9b: SetIdentityResolver synchronization (race pin) ---

// TestIdentityResolverRaceSafe wires the identity resolver while
// OnSessionEvent dispatches are in flight. Run under -race: the pre-fix
// unsynchronized c.identityResolve write/read is a reported data race.
func TestIdentityResolverRaceSafe(t *testing.T) {
	c := NewTeardownCoordinator(
		nil,
		func() map[string]string { return map[string]string{} }, // always misses
		time.Second,
		bughuntLogger(),
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			c.SetIdentityResolver(func(sessionID string) (string, bool) {
				return "conv-" + sessionID, true
			})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			c.OnSessionEvent(context.Background(), "session-race", "archive")
		}
	}()
	wg.Wait()
	c.inFlight.Wait() // let every dispatch finish before the detector reports
}
