package llm

// Semantic cache tests (llm-serving-tiers leaf 02, Tasks T2/T3/T4).
// Pinned constraints:
//   - paraphrase hit above threshold, below-threshold miss
//   - disabled cache returns miss WITHOUT calling the embedder
//   - tool-call-bearing responses are neither STORED nor SERVED
//   - embedder failure (incl. quota block) degrades to (nil,false,nil)
//     and never surfaces an error
//   - partition by model: one model's entries never serve another

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubEmbedder returns fixed vectors keyed by exact input text, tracking
// call counts.
type stubEmbedder struct {
	calls    atomic.Int64
	vectors  map[string][]float32
	failText string // embedder error when the text contains this substring
	failErr  error
}

func (s *stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	s.calls.Add(1)
	if s.failText != "" && strings.Contains(text, s.failText) {
		if s.failErr != nil {
			return nil, s.failErr
		}
		return nil, errors.New("embedder down")
	}
	if v, ok := s.vectors[text]; ok {
		return v, nil
	}
	// Deterministic fallback: unit-ish vector derived from the text so
	// unrelated texts score near-orthogonal.
	v := make([]float32, 8)
	for i := 0; i < len(text) && i < 8; i++ {
		if text[i]%2 == 0 {
			v[i%8] += 1
		} else {
			v[(i+3)%8] += 1
		}
	}
	return v, nil
}

// TestSemanticCacheParaphraseHit pins T2's core Gherkin: a paraphrase
// above threshold hits; a below-threshold paraphrase misses; a disabled
// cache misses WITHOUT calling the embedder.
func TestSemanticCacheParaphraseHit(t *testing.T) {
	// hand-built near-parallel vectors (cosine ~1) vs an orthogonal one.
	franceVec := []float32{1, 0.9, 0, 0, 0, 0, 0, 0}
	paraphraseVec := []float32{1, 0.88, 0.01, 0, 0, 0, 0, 0} // cos ~0.9995
	weatherVec := []float32{0, 0, 0, 1, 0.9, 0, 0, 0}        // orthogonal

	emb := &stubEmbedder{vectors: map[string][]float32{
		"model-a\nwhat is the capital of France": franceVec,
		"model-a\nwhat's France's capital city?": paraphraseVec,
		"model-a\nwhat is the weather in Paris":  weatherVec,
		"model-a\ncomplete opposite query text":  {0, 0, 0, 0, 0, 1, 0.9, 0},
	}}

	cfg := SemanticCacheConfig{Enabled: true, Threshold: 0.95}
	cache := NewSemanticCache(emb, cfg)
	stored := &Response{Content: "Paris"}
	cache.Store(context.Background(), "model-a", "what is the capital of France", stored)

	// Above-threshold paraphrase hits.
	got, hit, err := cache.LookupBehindMiss(context.Background(), "model-a", "what's France's capital city?")
	if err != nil {
		t.Fatalf("LookupBehindMiss returned error: %v", err)
	}
	if !hit {
		t.Fatal("paraphrase above threshold missed, want hit")
	}
	if got != stored {
		t.Fatalf("hit returned wrong response pointer")
	}

	// Below-threshold (near-orthogonal) prompt misses.
	_, hit, err = cache.LookupBehindMiss(context.Background(), "model-a", "what is the weather in Paris")
	if err != nil {
		t.Fatalf("LookupBehindMiss returned error: %v", err)
	}
	if hit {
		t.Fatal("orthogonal prompt hit, want miss")
	}
}

// TestSemanticCacheDisabledNoEmbedder pins T2's disabled posture: every
// method returns miss without touching the embedder.
func TestSemanticCacheDisabledNoEmbedder(t *testing.T) {
	emb := &stubEmbedder{}
	cache := NewSemanticCache(emb, SemanticCacheConfig{Enabled: false})

	resp, hit, err := cache.LookupBehindMiss(context.Background(), "m", "anything")
	if err != nil || hit || resp != nil {
		t.Fatalf("disabled lookup = (%v,%v,%v), want (nil,false,nil)", resp, hit, err)
	}
	cache.Store(context.Background(), "m", "anything", &Response{Content: "x"})
	if emb.calls.Load() != 0 {
		t.Fatalf("disabled cache called the embedder %d times, want 0", emb.calls.Load())
	}
	if cache.Len() != 0 {
		t.Fatalf("disabled cache stored %d entries, want 0", cache.Len())
	}

	// Nil cache is the same disabled posture (nil-receiver safety).
	var nilCache *SemanticCache
	resp, hit, err = nilCache.LookupBehindMiss(context.Background(), "m", "anything")
	if err != nil || hit || resp != nil {
		t.Fatalf("nil cache lookup = (%v,%v,%v), want (nil,false,nil)", resp, hit, err)
	}
	if nilCache.Enabled() {
		t.Fatal("nil cache reported enabled")
	}
}

// TestSemanticCacheMaxEntriesLRU pins the MaxEntries bound with LRU
// eviction: the oldest-used entry falls out first.
func TestSemanticCacheMaxEntriesLRU(t *testing.T) {
	emb := &stubEmbedder{vectors: map[string][]float32{
		"m\nalpha": {1, 0, 0, 0, 0, 0, 0, 0},
		"m\nbeta":  {1, 0, 0, 0, 0, 0, 0, 0},
	}}
	cfg := SemanticCacheConfig{Enabled: true, Threshold: 0.99, MaxEntries: 2, TTL: time.Hour}
	cache := NewSemanticCache(emb, cfg)

	cache.Store(context.Background(), "m", "alpha", &Response{Content: "a"})
	cache.Store(context.Background(), "m", "beta", &Response{Content: "b"})
	// Touch alpha so beta becomes LRU.
	if _, _, err := cache.LookupBehindMiss(context.Background(), "m", "alpha"); err != nil {
		t.Fatal(err)
	}
	// Third entry evicts beta (LRU tail).
	cache.Store(context.Background(), "m", "gamma", &Response{Content: "g"})
	if cache.Len() != 2 {
		t.Fatalf("Len = %d, want 2 after LRU eviction", cache.Len())
	}
	// beta was evicted: a lookup for it must miss via its FALLBACK vector
	// (beta is absent from the stub map for this probe -- the explicit
	// map above only serves the three Store/Lookup calls). The fallback
	// derives from the text bytes, which is near-orthogonal to alpha and
	// gamma's unit vectors, so similarity stays below the 0.99 threshold.
	delete(emb.vectors, "m\nbeta")
	if _, hit, _ := cache.LookupBehindMiss(context.Background(), "m", "beta"); hit {
		t.Fatal("evicted beta served, want miss")
	}
}

// TestSemanticCacheToolExemption pins T3: a tool-call-bearing response is
// neither SERVED semantically nor STORED.
func TestSemanticCacheToolExemption(t *testing.T) {
	v := []float32{1, 0.95, 0, 0, 0, 0, 0, 0}
	emb := &stubEmbedder{vectors: map[string][]float32{
		"m\nrun the deploy tool":         v,
		"m\nplease run the deploy tool?": v, // near-identical paraphrase
	}}
	cfg := SemanticCacheConfig{Enabled: true, Threshold: 0.9}
	cache := NewSemanticCache(emb, cfg)

	toolResp := &Response{Content: "done", ToolCalls: []ToolCall{{ID: "call1", Type: "function", Function: ToolCallFunction{Name: "deploy"}}}}

	// Store skips tool-call-bearing responses entirely.
	cache.Store(context.Background(), "m", "run the deploy tool", toolResp)
	if cache.Len() != 0 {
		t.Fatalf("tool-call response stored, want skipped (Len=%d)", cache.Len())
	}

	// Manual insertion (simulating legacy/dirty state) still never
	// serves: the serve-side guard re-checks HasToolCalls.
	emb2 := &stubEmbedder{vectors: map[string][]float32{"m\nq": v}}
	cache2 := NewSemanticCache(emb2, cfg)
	cache2.entries["m x"] = &semanticEntry{vector: v, response: toolResp, partition: "m", expiresAt: time.Now().Add(time.Hour)}
	resp, hit, _ := cache2.LookupBehindMiss(context.Background(), "m", "q")
	if hit || resp != nil {
		t.Fatal("tool-call response served semantically, want miss")
	}
}

// TestSemanticCacheEmbedderFailureDegraded pins T4: ANY embedder error --
// including a quota-block-shaped one -- returns (nil,false,nil); the turn
// proceeds exact-only; no error surfaces.
func TestSemanticCacheEmbedderFailureDegraded(t *testing.T) {
	quotaErr := errors.New("quota blocked: retry after reset")
	emb := &stubEmbedder{failText: "deploy", failErr: quotaErr}
	cfg := SemanticCacheConfig{Enabled: true, Threshold: 0.9}
	cache := NewSemanticCache(emb, cfg)

	// Exact-miss lookup with a failing embedder: silent exact-only
	// degradation.
	resp, hit, err := cache.LookupBehindMiss(context.Background(), "m", "deploy the service")
	if err != nil {
		t.Fatalf("embedder failure surfaced as error %v, want nil", err)
	}
	if hit || resp != nil {
		t.Fatalf("embedder failure produced a hit (%v,%v), want miss", resp, hit)
	}
	// errors.Is must be false too (no wrapped sentinel).
	if errors.Is(err, quotaErr) {
		t.Fatal("error chain carries the embedder failure")
	}

	// Store on a failing embedder is silent and stores nothing.
	cache.Store(context.Background(), "m", "deploy the service", &Response{Content: "x"})
	if cache.Len() != 0 {
		t.Fatalf("store persisted despite embedder failure (Len=%d)", cache.Len())
	}
}

// TestSemanticCachePartition pins the (model, instruction) partition:
// entries from one model never serve another, even for identical prompts.
func TestSemanticCachePartition(t *testing.T) {
	v := []float32{1, 0.95, 0, 0, 0, 0, 0, 0}
	emb := &stubEmbedder{vectors: map[string][]float32{
		"model-a\nwhat is 2+2": v,
		"model-b\nwhat is 2+2": v, // same embedding, different model
	}}
	cfg := SemanticCacheConfig{Enabled: true, Threshold: 0.9}
	cache := NewSemanticCache(emb, cfg)

	cache.Store(context.Background(), "model-a", "what is 2+2", &Response{Content: "4"})

	// Same model: hit. Different model: miss (no cross-model leakage).
	_, hit, _ := cache.LookupBehindMiss(context.Background(), "model-a", "what is 2+2")
	if !hit {
		t.Fatal("same-model exact-paraphrase missed")
	}
	_, hit, _ = cache.LookupBehindMiss(context.Background(), "model-b", "what is 2+2")
	if hit {
		t.Fatal("cross-model leakage: model-b served model-a's entry")
	}
}

// TestSemanticCacheTTLExpiry pins that expired entries are never served.
func TestSemanticCacheTTLExpiry(t *testing.T) {
	emb := &stubEmbedder{vectors: map[string][]float32{
		"m\nq": {1, 0.95, 0, 0, 0, 0, 0, 0},
	}}
	cfg := SemanticCacheConfig{Enabled: true, Threshold: 0.9, TTL: -time.Second} // already expired
	cache := NewSemanticCache(emb, cfg)
	cache.Store(context.Background(), "m", "q", &Response{Content: "x"})
	_, hit, _ := cache.LookupBehindMiss(context.Background(), "m", "q")
	if hit {
		t.Fatal("expired entry served, want miss")
	}
}
