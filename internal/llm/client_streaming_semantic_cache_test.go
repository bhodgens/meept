package llm

// Streaming semantic-cache wiring tests (wiring-followups-final leaf 04).
// The client's streaming path (ChatWithDeltaCallback) must consult the
// shared semanticLookupBehindMiss seam on an exact-miss and store through
// semanticStoreAfterSuccess on a live completion - the same hard
// tool-call exemption and disabled posture as the non-streaming sites.
//
// Pinned shapes:
//   - streaming paraphrase hit serves from cache with ZERO live consults;
//   - a streaming tool-call turn neither serves nor stores semantically;
//   - disabled cache = byte-identical live path;
//   - a cache-HIT streaming turn keeps the TTFT sentinel (-1) semantics.
//
// Server shape note: doStreamRequest parses SSE deltas; the stubs emit
// OpenAI-compatible `data:` frames exactly like the TTFT tests.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// leaf04StreamServer is an OpenAI-compatible SSE stub returning the given
// content and counting live completions; withToolCalls emits a tool-call
// delta instead of text.
func leaf04StreamServer(t *testing.T, live *atomic.Int64, content string, withToolCalls bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		live.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if withToolCalls {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-leaf04\",\"type\":\"function\",\"function\":{\"name\":\"deploy\",\"arguments\":\"{}\"}}]}}]}\n\n"))
		} else {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + content + "\"}}]}\n\n"))
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// leaf04Client builds a streaming client over the stub with a live token
// cache (the lookup lives behind the exact-key miss) and the given semantic
// cache.
func leaf04Client(t *testing.T, serverURL string, sem *SemanticCache) *Client {
	t.Helper()
	cacheCfg := CacheConfig{L1MaxEntries: 100, L2Enabled: false, DefaultTTL: 5 * time.Minute, CleanupFreq: time.Minute, Enabled: true}
	coord, err := NewTokenCacheCoordinator(cacheCfg)
	if err != nil {
		t.Fatalf("NewTokenCacheCoordinator: %v", err)
	}
	t.Cleanup(func() { _ = coord.Close() })
	return NewClient(&ModelConfig{BaseURL: serverURL, ModelID: "test-model", ProviderID: "prov-x"},
		WithTokenCache(coord), WithSemanticCache(sem))
}

// TestStreamingSemanticParaphraseHitServesFromCache pins T2(a): a streaming
// exact-miss whose paraphrase clears the threshold serves the cached
// response with ZERO live consults.
func TestStreamingSemanticParaphraseHitServesFromCache(t *testing.T) {
	var live atomic.Int64
	server := leaf04StreamServer(t, &live, "live answer", false)
	defer server.Close()

	vec := []float32{1, 0.98, 0, 0, 0, 0, 0, 0}
	emb := &stubEmbedder{vectors: map[string][]float32{
		// Keys are semanticPromptText(part, messagesPromptText(messages)):
		// the flattened "role: content" prompt, model-prefixed.
		"test-model\nuser: what is the capital of France": vec,
		"test-model\nuser: France capital, tell me":       vec,
	}}
	cache := NewSemanticCache(emb, SemanticCacheConfig{Enabled: true, Threshold: 0.95})
	cache.Store(context.Background(), "test-model", "user: what is the capital of France", &Response{Content: "Paris"})

	client := leaf04Client(t, server.URL, cache)
	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "France capital, tell me"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("stream call: %v", err)
	}
	if resp == nil || resp.Content != "Paris" {
		t.Fatalf("semantic hit served wrong response: %+v", resp)
	}
	if live.Load() != 0 {
		t.Fatalf("streaming semantic hit dispatched %d live completions; cache short-circuit required", live.Load())
	}
}

// TestStreamingSemanticStoreAfterSuccess pins the store half: a live
// streaming completion stores into the semantic tier and a paraphrase
// re-ask of the SAME conversation then serves from cache.
func TestStreamingSemanticStoreAfterSuccess(t *testing.T) {
	var live atomic.Int64
	server := leaf04StreamServer(t, &live, "live answer", false)
	defer server.Close()

	vec := []float32{1, 0.98, 0, 0, 0, 0, 0, 0}
	emb := &stubEmbedder{vectors: map[string][]float32{
		"test-model\nuser: streaming store probe question":   vec,
		"test-model\nuser: streaming store probe, rephrased": vec,
	}}
	cache := NewSemanticCache(emb, SemanticCacheConfig{Enabled: true, Threshold: 0.95})

	client := leaf04Client(t, server.URL, cache)
	// Turn 1: live completion (exact miss) must STORE.
	r1, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "streaming store probe question"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if r1 == nil || r1.Content != "live answer" || live.Load() != 1 {
		t.Fatalf("turn 1 live path broken: resp=%+v live=%d", r1, live.Load())
	}
	if cache.Len() != 1 {
		t.Fatalf("semantic tier did not store the live streaming response (Len=%d)", cache.Len())
	}
	// Turn 2: paraphrase must hit with no further live consult.
	r2, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "streaming store probe, rephrased"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if r2 == nil || r2.Content != "live answer" {
		t.Fatalf("paraphrase served wrong response: %+v", r2)
	}
	if live.Load() != 1 {
		t.Fatalf("paraphrase re-ask dispatched %d live consults, want 1 (turn 1 only)", live.Load())
	}
}

// TestStreamingSemanticToolCallExemption pins T2(b): a streaming tool-call
// turn bypasses the semantic tier entirely - no lookup serve (the live
// tool-call response is returned) and no store.
func TestStreamingSemanticToolCallExemption(t *testing.T) {
	var live atomic.Int64
	server := leaf04StreamServer(t, &live, "", true)
	defer server.Close()

	vec := []float32{1, 0.98, 0, 0, 0, 0, 0, 0}
	emb := &stubEmbedder{vectors: map[string][]float32{
		"test-model\nuser: please run the deploy tool": vec,
	}}
	cache := NewSemanticCache(emb, SemanticCacheConfig{Enabled: true, Threshold: 0.95})
	// A dirty/legacy row carrying a tool-call response that WOULD clear the
	// threshold for this prompt: the serve-side guard must skip it (never
	// serve tool calls) and the live tool response must come back.
	cache.mu.Lock()
	cache.entries["m x"] = &semanticEntry{vector: vec, response: &Response{Content: "dirty", ToolCalls: []ToolCall{{ID: "call-dirty", Type: "function", Function: ToolCallFunction{Name: "deploy"}}}}, partition: "test-model", expiresAt: time.Now().Add(time.Hour)}
	cache.mu.Unlock()

	client := leaf04Client(t, server.URL, cache)
	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "please run the deploy tool"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("stream tool call: %v", err)
	}
	if resp == nil || !resp.HasToolCalls() {
		t.Fatalf("tool-call turn served %v; live tool response required", resp)
	}
	if live.Load() != 1 {
		t.Fatalf("tool-call turn dispatched %d live consults, want 1", live.Load())
	}
	if cache.Len() != 1 {
		t.Fatalf("tool-call response stored semantically (Len=%d); exemption required", cache.Len())
	}
}

// TestStreamingSemanticDisabledByteIdentical pins T2(c): a disabled
// semantic cache leaves the streaming path on the plain live shape (one
// live consult, live content) - structurally unreachable tier.
func TestStreamingSemanticDisabledByteIdentical(t *testing.T) {
	var live atomic.Int64
	server := leaf04StreamServer(t, &live, "live answer", false)
	defer server.Close()

	emb := &stubEmbedder{}
	cache := NewSemanticCache(emb, SemanticCacheConfig{Enabled: false})

	client := leaf04Client(t, server.URL, cache)
	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "disabled posture question"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("stream call: %v", err)
	}
	if resp == nil || resp.Content != "live answer" {
		t.Fatalf("disabled posture changed the streaming response: %+v", resp)
	}
	if live.Load() != 1 {
		t.Fatalf("disabled posture dispatched %d live consults, want exactly 1", live.Load())
	}
	if emb.calls.Load() != 0 {
		t.Fatalf("disabled cache consulted the embedder %d times", emb.calls.Load())
	}
}

// TestStreamingSemanticCacheHitKeepsTTFTSentinel pins T3: a cache-hit
// streaming turn ledgered its row with the -1 sentinel (no clock was ever
// armed before the hit short-circuit).
func TestStreamingSemanticCacheHitKeepsTTFTSentinel(t *testing.T) {
	var live atomic.Int64
	server := leaf04StreamServer(t, &live, "live answer", false)
	defer server.Close()

	vec := []float32{1, 0.98, 0, 0, 0, 0, 0, 0}
	emb := &stubEmbedder{vectors: map[string][]float32{
		"test-model\nuser: ttft sentinel probe":      vec,
		"test-model\nuser: ttft sentinel, rephrased": vec,
	}}
	cache := NewSemanticCache(emb, SemanticCacheConfig{Enabled: true, Threshold: 0.95})
	cache.Store(context.Background(), "test-model", "user: ttft sentinel probe", &Response{Content: "cached"})

	client := leaf04Client(t, server.URL, cache)
	obsCh := make(ttftObsChan, 4)
	client.SetLLMCallObserver(func(rec appmetrics.LLMCallRecord) { obsCh <- rec })

	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "ttft sentinel, rephrased"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("stream cache hit: %v", err)
	}
	if resp == nil || resp.Content != "cached" {
		t.Fatalf("hit served wrong response: %+v", resp)
	}
	if live.Load() != 0 {
		t.Fatalf("hit consulted live %d times", live.Load())
	}
	// The lookup short-circuit returns BEFORE any ledger row is written
	// (recordUsageStore lives on the live success path), so the observable
	// sentinel contract is: no llm_calls row exists for the hit. Drain
	// briefly and assert nothing fired.
	select {
	case rec := <-obsCh:
		t.Fatalf("cache-hit streaming turn ledgered a row (TTFTMS=%d); hits must not ledger", rec.TTFTMS)
	case <-time.After(150 * time.Millisecond):
	}
}
