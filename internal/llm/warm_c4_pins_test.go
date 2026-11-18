package llm

// C4 hard-rule pins added with the opts-shaped refactor (issue #142):
//   - rule 3 (no cache traffic): warm never touches the exact-key
//     tokenCache or the semantic tier, while the same client's real turn
//     demonstrably does (control), proving the bypass is warm-specific
//     and not a mis-wired cache.
//   - rule 5 (no identity side effects): a warm — success OR failure —
//     never touches ProviderManager health counters, never rotates the
//     Resolver alias head, never records an alias failure, and never
//     writes the sticky per-session endpoint map.
//   - ProviderManager.Warm routing: the WithResolvedModel opt (the
//     warmer layer's alias resolution) reorders dispatch exactly like
//     the real turn's selection lane, and a failing warm does NOT fail
//     over to a second provider.
//
// Rules 2 (payload identity), 4 (no retry), 6 (lane/slot posture) are
// pinned in warm_test.go (TestWarm_PrefixIdentity,
// TestWarm_QuotaUpstream_QuietNoRetry, TestWarm_NeverQueuesBehindRealTurn).

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- recording ResponseCache (rule 3) ---

// recordingCache counts every interface method so a test can assert the
// warm path performs ZERO cache traffic while a real turn performs the
// expected Get/Put (control).
type recordingCache struct {
	mu      sync.Mutex
	gets    int
	puts    int
	lastGet CacheKey
	lastPut CacheKey
	entries map[string]*Response
}

func newRecordingCache() *recordingCache {
	return &recordingCache{entries: map[string]*Response{}}
}

func (r *recordingCache) counts() (gets, puts int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gets, r.puts
}

func (r *recordingCache) Get(_ context.Context, key CacheKey) (*CacheEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gets++
	r.lastGet = key
	if resp, ok := r.entries[key.String()]; ok {
		return &CacheEntry{Response: resp}, true
	}
	return nil, false
}

func (r *recordingCache) Put(_ context.Context, key CacheKey, resp *Response) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.puts++
	r.lastPut = key
	if resp != nil {
		cp := *resp
		r.entries[key.String()] = &cp
	}
}

func (r *recordingCache) Invalidate(context.Context, CacheKey)     {}
func (r *recordingCache) InvalidateByFile(context.Context, string) {}
func (r *recordingCache) Clear()                                   {}
func (r *recordingCache) Stats() CacheStats                        { return CacheStats{} }
func (r *recordingCache) Close() error                             { return nil }

// TestWarm_CacheBypass_ExactKey pins C4 rule 3 on the exact-key tier: a
// completed warm performs ZERO tokenCache Get/Put even though the same
// client + same messages are cache-instrumented; then a real Chat of the
// SAME messages does hit the cache lane (Get control), proving the
// recorder is wired and the bypass belongs to the warm alone.
func TestWarm_CacheBypass_ExactKey(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	rc := newRecordingCache()
	c := warmTestClient(t, srv.URL, WithTokenCache(rc))

	msgs := warmMessages()
	if err := c.Warm(context.Background(), msgs, WithTaskScope("", "sess-42")); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if !waitFor(func() bool { return len(recs()) == 1 && laneIdle(c) }, 3*time.Second) {
		t.Fatal("warm did not complete")
	}
	if gets, puts := rc.counts(); gets != 0 || puts != 0 {
		t.Errorf("warm touched the exact-key cache: gets=%d puts=%d, want 0/0 (C4 rule 3)", gets, puts)
	}

	// Control: the real turn on the same client/messages uses the cache
	// lane (one Get; one Put after the upstream answer).
	if _, err := c.Chat(context.Background(), msgs, WithTaskScope("task-1", "sess-42")); err != nil {
		t.Fatalf("control Chat: %v", err)
	}
	gets, puts := rc.counts()
	if gets < 1 || puts < 1 {
		t.Fatalf("control Chat did not use the cache (gets=%d puts=%d) — recorder mis-wired, warm assertion vacuous", gets, puts)
	}
}

// TestWarm_CacheBypass_SemanticTier pins C4 rule 3 on the semantic tier:
// with an enabled semantic cache that ALREADY holds an answer for the
// warm's prompt, a warm must never consult the embedder (zero semantic
// lookups), must dispatch its own request, and must never store.
func TestWarm_CacheBypass_SemanticTier(t *testing.T) {
	srv, recs := warmUpstream(t, nil)

	emb := &stubEmbedder{vectors: map[string][]float32{}}
	sem := NewSemanticCache(emb, SemanticCacheConfig{Enabled: true, Threshold: 0.95})

	rc := newRecordingCache()
	cfg := &ModelConfig{BaseURL: srv.URL + "/v1", ModelID: "test-model", ProviderID: "openai-test", MaxTokens: 4096}
	c := NewClient(cfg, WithLogger(slog.New(slog.DiscardHandler)),
		WithTokenCache(rc), WithSemanticCache(sem))

	// Pre-seed the semantic tier with a near-duplicate answer, so a
	// lookup WOULD hit if the warm consulted it.
	msgs := []ChatMessage{{Role: RoleUser, Content: "What is the weather in Denver right now?"}}
	seedText := "user: What is the weather in Denver right now?"
	emb.vectors["test-model\n"+seedText] = []float32{1, 0, 0, 0, 0, 0, 0, 0}
	sem.Store(context.Background(), "test-model", seedText, &Response{Content: "Sunny."})

	callsBefore := emb.calls.Load()
	if err := c.Warm(context.Background(), msgs, WithTaskScope("", "s")); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if !waitFor(func() bool { return len(recs()) == 1 && laneIdle(c) }, 3*time.Second) {
		t.Fatal("warm did not reach upstream")
	}
	time.Sleep(250 * time.Millisecond) // grace any async store path

	if got := emb.calls.Load(); got != callsBefore {
		t.Errorf("warm consulted the semantic tier %d embedder call(s), want 0 (C4 rule 3)", got-callsBefore)
	}
	if gets, puts := rc.counts(); gets != 0 || puts != 0 {
		t.Errorf("warm touched the exact-key cache: gets=%d puts=%d, want 0/0", gets, puts)
	}
	if n := len(recs()); n != 1 {
		t.Errorf("upstream saw %d request(s), want 1 (warm must dispatch AND must not retry)", n)
	}
}

// TestWarm_EmptyCompletionNoRetry pins C4 rule 4's named case: a
// max_tokens=1 warm that comes back with an EMPTY completion (legitimately
// empty output) must NOT be re-dispatched — the warm lane never enters the
// ErrEmptyResponse retry loop. The fake answers 200 with blank content;
// the upstream counter must stay at exactly 1.
func TestWarm_EmptyCompletionNoRetry(t *testing.T) {
	srv, recs := warmUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":0,"total_tokens":3}}`))
	})
	c := warmTestClient(t, srv.URL)

	if err := c.Warm(context.Background(), warmMessages(), WithTaskScope("", "s")); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if !waitFor(func() bool { return len(recs()) >= 1 && laneIdle(c) }, 3*time.Second) {
		t.Fatal("warm never completed")
	}
	// Give any hypothetical retry loop time to fire.
	time.Sleep(400 * time.Millisecond)
	if n := len(recs()); n != 1 {
		t.Errorf("empty-completion warm saw %d upstream requests, want exactly 1 (no re-dispatch)", n)
	}
}

// --- rule 5: no identity side effects through the ProviderManager ---

// warmStatusServer counts requests per endpoint and answers 500 (a plain
// server error: the class the turn lane would record as an alias/endpoint
// failure) or 200 depending on status.
func warmStatusServer(t *testing.T, status int, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"w"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestProviderManager_Warm_NoIdentitySideEffects pins C4 rule 5 at the
// manager lane: a warm whose upstream FAILS must leave every health
// counter, the failover rotation, and the resolver's sticky session map
// exactly as they were. The real Chat path increments failure counters on
// the same 500 (control) — proving the counters are live and the warm
// genuinely bypasses them.
func TestProviderManager_Warm_NoIdentitySideEffects(t *testing.T) {
	var primaryHits, backupHits atomic.Int64
	primary := warmStatusServer(t, http.StatusInternalServerError, &primaryHits)
	backup := warmStatusServer(t, http.StatusOK, &backupHits)

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primary.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backup.URL, ModelID: "m2"},
		},
		FailoverTimeout: 2 * time.Second,
		Logger:          slog.New(slog.DiscardHandler),
	})

	resolver := NewResolver(createTestConfig(), slog.New(slog.DiscardHandler))
	pm.SetStickyEndpointResolver(resolver)

	healthBefore := pm.GetProviderHealth()

	msgs := []ChatMessage{{Role: RoleUser, Content: "warm identity probe"}}
	// Warm with a session scope: the sticky-report seam keys on
	// chatOptions.sessionID, so a leak WOULD be observable here.
	if err := pm.Warm(context.Background(), msgs, WithTaskScope("", "sess-warm")); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	// Wait for the warm request to reach (and fail at) the upstream, then
	// grace the async lane completion.
	if !waitFor(func() bool { return primaryHits.Load() >= 1 }, 3*time.Second) {
		t.Fatal("warm never reached the primary upstream")
	}
	time.Sleep(400 * time.Millisecond)

	// Health counters untouched.
	healthAfter := pm.GetProviderHealth()
	if len(healthBefore) != len(healthAfter) {
		t.Fatalf("health entries changed shape: %d -> %d", len(healthBefore), len(healthAfter))
	}
	for i, h := range healthAfter {
		b := healthBefore[i]
		if h.FailureCount != b.FailureCount || h.SuccessCount != b.SuccessCount ||
			h.ConsecutiveFails != b.ConsecutiveFails || h.Status != b.Status {
			t.Errorf("warm mutated health of %s: fails=%d consec=%d status=%v (was %d/%d/%v) — C4 rule 5",
				h.ProviderID, h.FailureCount, h.ConsecutiveFails, h.Status,
				b.FailureCount, b.ConsecutiveFails, b.Status)
		}
	}
	// No failover: a failing warm stays single-shot (also pins rule 4 at
	// the manager lane).
	if n := backupHits.Load(); n != 0 {
		t.Errorf("failed warm fail-overed to the backup %d time(s), want 0", n)
	}
	// The sticky session-endpoint map is untouched by the failed warm.
	if _, ok := resolver.PreferredEndpoint("sess-warm"); ok {
		t.Error("warm wrote the sticky per-session endpoint preference — C4 rule 5")
	}

	// Control: the real turn on the SAME client state DOES record the
	// failure (counters move), proving the warm bypass is real.
	if _, err := pm.Chat(context.Background(), msgs, WithTaskScope("", "sess-ctrl")); err == nil {
		t.Log("control Chat succeeded via failover (expected)")
	}
	moved := false
	for _, h := range pm.GetProviderHealth() {
		if h.ProviderID == "primary" && h.FailureCount > 0 {
			moved = true
		}
	}
	if !moved {
		t.Error("control Chat did not move the primary failure counter — harness too weak to prove rule 5")
	}
}

// --- ProviderManager.Warm routing (C4 rule 1 at the manager lane) ---

// TestProviderManager_Warm_RoutesByResolvedModel pins the warmer-layer
// contract: the WithResolvedModel opt (the alias resolution the CALLER
// performed via PeekAliasForWarm) reorders the warm dispatch to the
// resolved provider, and ONLY that provider is warmed (no failover).
func TestProviderManager_Warm_RoutesByResolvedModel(t *testing.T) {
	var primaryHits, backupHits atomic.Int64
	primary := warmStatusServer(t, http.StatusOK, &primaryHits)
	backup := warmStatusServer(t, http.StatusOK, &backupHits)

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primary.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backup.URL, ModelID: "m2"},
		},
		FailoverTimeout: 2 * time.Second,
		Logger:          slog.New(slog.DiscardHandler),
	})

	msgs := []ChatMessage{{Role: RoleUser, Content: "routed warm"}}
	if err := pm.Warm(context.Background(), msgs,
		WithResolvedModel(&ModelConfig{ProviderID: "backup", ModelID: "m2"}),
		WithTaskScope("", "sess-42")); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if !waitFor(func() bool { return backupHits.Load() >= 1 }, 3*time.Second) {
		t.Fatalf("warm never reached the resolved provider (primary=%d backup=%d)",
			primaryHits.Load(), backupHits.Load())
	}
	time.Sleep(300 * time.Millisecond)
	if n := primaryHits.Load(); n != 0 {
		t.Errorf("routed warm also hit the non-selected primary %d time(s), want 0 (single dispatch, no failover)", n)
	}
}

// --- Resolver.PeekAliasForWarm read-only pins ---

// TestPeekAliasForWarm_ReadOnly pins that the warmer layer's alias
// resolution observes the serving candidate WITHOUT touching the state
// the real turn reads (rotation index, cooldown, sticky pins, endpoint
// preference map) — C4 rule 1's "resolution lives at the Resolver layer"
// must not come at the cost of resolution SIDE EFFECTS on the warm path.
func TestPeekAliasForWarm_ReadOnly(t *testing.T) {
	r := NewResolver(createTestConfig(), slog.New(slog.DiscardHandler))

	// Pre-arm observable state: a rotation position + a sticky pin.
	h := r.getOrCreateHealth("coder")
	h.CurrentIndex = 1

	mc, ok := r.PeekAliasForWarm("coder", "sess-peek")
	if !ok || mc == nil {
		t.Fatal("PeekAliasForWarm must resolve the coder alias")
	}
	// Read-only verdicts.
	h2 := r.health["coder"]
	if h2.CurrentIndex != 1 {
		t.Errorf("peek mutated rotation index: %d, want 1 (unchanged)", h2.CurrentIndex)
	}
	if len(h2.StickyPins) != 0 {
		t.Errorf("peek wrote sticky pins: %v", h2.StickyPins)
	}
	if !h2.LastFailure.IsZero() || !h2.CooldownUntil.IsZero() {
		t.Error("peek armed cooldown/failure state")
	}
	if _, ok := r.PreferredEndpoint("sess-peek"); ok {
		t.Error("peek wrote the session endpoint preference")
	}

	// Unknown alias / empty name: skip, never a guess.
	if _, ok := r.PeekAliasForWarm("no-such-alias", "s"); ok {
		t.Error("unknown alias resolved")
	}
	if _, ok := r.PeekAliasForWarm("", "s"); ok {
		t.Error("empty alias resolved")
	}
	// nil resolver is safe.
	var nilR *Resolver
	if _, ok := nilR.PeekAliasForWarm("coder", "s"); ok {
		t.Error("nil resolver resolved")
	}
}
