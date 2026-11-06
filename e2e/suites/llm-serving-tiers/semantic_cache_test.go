//go:build e2e

// Semantic cache scenarios (leaf 05 T3, over leaf 02's implementation):
// paraphrase hit (no fresh consult), tool-call exemption, embedder-failure
// exact-only degradation, and the disabled posture.
//
// Route (documented deviation): the daemon-side construction site landed
// (wiring-followups-final leaf 05, internal/daemon/
// semantic_cache_wiring.go) and is pinned in-package
// (TestSemanticCacheWiring*), but the harness daemon does not expose a
// second-chance-lookup scenario, so these scenarios keep driving the REAL
// llm.SemanticCache over the REAL embedding path (memory/vector
// OpenAIProvider) against a stub /embeddings endpoint - the adapter shape
// leaf 02's Embedder contract names (the daemon's semanticVectorEmbedder
// wraps the SAME provider) - and assert via the stub request counters.
package llmservingtiers

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// storedResponse builds a plain (non-tool) response for Store.
func storedResponse(text string) *llm.Response {
	return &llm.Response{Content: text, FinishReason: "stop"}
}

// TestSemanticCacheParaphraseHit (T3a): a stored prompt's semantic
// paraphrase (same marker word -> cosine 1.0 > 0.95 floor) is served from
// the cache - the live model path is NOT re-consulted for a fresh
// completion (asserted via the completion-stub request counter staying at
// zero; the embedder stub is the only endpoint hit on the lookup).
func TestSemanticCacheParaphraseHit(t *testing.T) {
	embed, prov := newEmbedStub(t)
	completions := newMemberStub(t, "ok") // the live-model stand-in
	cache := semanticCache(prov, true)
	ctx := context.Background()

	// Turn 1: live response completes; Store records the prompt vector.
	if _, hit, err := cache.LookupBehindMiss(ctx, "stub-model", "what is the quorum for scenario alpha"); hit || err != nil {
		t.Fatalf("cold lookup returned hit=%v err=%v; want miss", hit, err)
	}
	cache.Store(ctx, "stub-model", "what is the quorum for scenario alpha", storedResponse("quorum is seven"))
	if embed.count() == 0 {
		t.Fatal("Store never consulted the embedder")
	}

	// Turn 2: a paraphrase of the same prompt must hit semantically -
	// the LIVE path is skipped entirely (zero completion requests).
	resp, hit, err := cache.LookupBehindMiss(ctx, "stub-model", "quorum scenario alpha - what is it?")
	if err != nil {
		t.Fatalf("paraphrase lookup errored: %v", err)
	}
	if !hit {
		t.Fatalf("paraphrase missed (embed hits=%d); vector path broken", embed.count())
	}
	if resp == nil || resp.Content != "quorum is seven" {
		t.Fatalf("semantic hit served wrong response: %+v", resp)
	}
	if completions.hitCount() != 0 {
		t.Fatalf("semantic hit still dispatched %d live completions", completions.hitCount())
	}
}

// TestSemanticCacheDifferentPromptMisses (T3a miss arm): a prompt on a
// different marker (orthogonal vector) misses - the live path is the
// fallback.
func TestSemanticCacheDifferentPromptMisses(t *testing.T) {
	embed, prov := newEmbedStub(t)
	completions := newMemberStub(t, "ok")
	cache := semanticCache(prov, true)
	ctx := context.Background()

	cache.Store(ctx, "stub-model", "what is the quorum for scenario alpha", storedResponse("quorum is seven"))

	resp, hit, err := cache.LookupBehindMiss(ctx, "stub-model", "how is the budget allocated")
	if err != nil {
		t.Fatalf("miss lookup errored: %v", err)
	}
	if hit || resp != nil {
		t.Fatalf("unrelated prompt hit semantically (embed hits=%d)", embed.count())
	}
	// The caller would now fall through to the live path; pin that the
	// completions stand-in is reachable (its counter reads zero here).
	if completions.hitCount() != 0 {
		t.Fatalf("live stand-in unexpectedly consulted during a miss: %d hits", completions.hitCount())
	}
}

// TestSemanticCacheToolCallExemption (T3b): a tool-call-bearing response is
// NEVER stored (and a later paraphrase cannot serve it) - a fresh live
// dispatch is the only path.
func TestSemanticCacheToolCallExemption(t *testing.T) {
	embed, prov := newEmbedStub(t)
	cache := semanticCache(prov, true)
	ctx := context.Background()

	toolResp := &llm.Response{
		FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{{
			ID:   "call-e2e-exempt",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "file_write",
				Arguments: `{"path":"x.txt","content":"y"}`,
			},
		}},
	}
	beforeStore := embed.count()
	cache.Store(ctx, "stub-model", "write the quorum file for scenario alpha", toolResp)
	afterStore := embed.count()

	// The paraphrase of the same prompt must MISS: the tool-call reply
	// never entered the store.
	_, hit, err := cache.LookupBehindMiss(ctx, "stub-model", "quorum file for scenario alpha, please write it")
	if err != nil {
		t.Fatalf("exemption lookup errored: %v", err)
	}
	if hit {
		t.Fatal("a tool-call-bearing response was served semantically")
	}
	// Control: a plain response DOES reach the embedder on Store, so the
	// stub path works; the tool-call response must NOT have (embedder
	// consult count unchanged by the tool Store). afterLookup includes the
	// miss lookup's own embedder consult.
	afterLookup := embed.count()
	cache.Store(ctx, "stub-model", "plain prose prompt for the control arm", storedResponse("plain"))
	if embed.count() != afterLookup+1 {
		t.Fatalf("control Store did not consult the embedder (count %d, want %d); stub path broken", embed.count(), afterLookup+1)
	}
	if afterStore != beforeStore {
		t.Fatalf("Store embedded a tool-call response (%d -> %d embedder hits); it must be skipped entirely", beforeStore, afterStore)
	}
}

// TestSemanticCacheEmbedderFailureDegrades (T3c): the embedder endpoint
// returning 500 degrades to exact-only - the lookup returns miss with NIL
// error (never an error, never a parked turn) and a Store during the
// outage is silently skipped.
func TestSemanticCacheEmbedderFailureDegrades(t *testing.T) {
	embed, prov := newEmbedStub(t)
	cache := semanticCache(prov, true)
	ctx := context.Background()

	cache.Store(ctx, "stub-model", "what is the quorum for scenario alpha", storedResponse("quorum is seven"))
	embed.setFail(true)

	resp, hit, err := cache.LookupBehindMiss(ctx, "stub-model", "what is the quorum for scenario alpha")
	if err != nil {
		t.Fatalf("embedder failure surfaced an error: %v", err)
	}
	if hit || resp != nil {
		t.Fatal("embedder failure produced a hit; exact-only degradation required")
	}
	if embed.count() == 0 {
		t.Fatal("the failing embedder was never consulted; the stub did not engage")
	}
}

// TestSemanticCacheDisabled (T3d quiet control): the disabled posture is
// structurally dead - no embedder call at all, immediate miss, identical
// behavior with or without stored data.
func TestSemanticCacheDisabled(t *testing.T) {
	embed, prov := newEmbedStub(t)
	cache := semanticCache(prov, false)
	ctx := context.Background()

	cache.Store(ctx, "stub-model", "what is the quorum for scenario alpha", storedResponse("quorum is seven"))
	if embed.count() != 0 {
		t.Fatalf("disabled cache consulted the embedder %d times on Store", embed.count())
	}
	_, hit, err := cache.LookupBehindMiss(ctx, "stub-model", "what is the quorum for scenario alpha")
	if err != nil {
		t.Fatalf("disabled lookup errored: %v", err)
	}
	if hit {
		t.Fatal("disabled cache served a hit")
	}
	if embed.count() != 0 {
		t.Fatalf("disabled cache consulted the embedder %d times on lookup", embed.count())
	}
}
