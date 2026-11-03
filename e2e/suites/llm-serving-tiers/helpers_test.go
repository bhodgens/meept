//go:build e2e

// Package llmservingtiers is the hermetic e2e suite for the
// llm-serving-tiers tree (docs/plans/20261031-llm-serving-tiers, leaf 05):
// tier depth ceilings + degraded-tier metadata signaling, semantic cache
// hit/exemption/degradation posture, models.json5 --cache-reuse loader
// compatibility, and the llm_calls store shape the TTFT column lands in.
//
// Suite topology (documented deviation, mirrors the context-memory-engine
// precedent): leaf 01's tier gate is fully implemented inside
// internal/llm (tier_policy.go + provider_manager.go rotation + errors_tier.go)
// but the daemon-side SetTierPolicy call site (components.go consuming
// llm.tiers) is NOT wired yet, and leaf 02's client/daemon wiring
// (LookupBehindMiss call site + components.go construction) is NOT landed.
// A chat-turn drive therefore cannot reach either feature through the
// harness daemon. Scenarios that need tier semantics run the REAL
// construction seams in-process over the REAL ProviderManager (real
// OpenAI-compatible clients over real stub endpoints) and the REAL
// SemanticCache; the live-daemon scenarios pin the observability the
// daemon DOES expose today: config acceptance of llm.tiers +
// llm.cache.semantic keys, leaf 03's --cache-reuse loader compatibility,
// and the llm_calls rows (leaf 04's ttft_ms column is not landed, so the
// suite pins the exact-store shape the migration extends - see
// config_ttft_test.go for the documented gap).
package llmservingtiers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/vector"
)

// ---------------------------------------------------------------------------
// multi-member OpenAI-compatible stub topology (session-affinity pattern:
// one stub per alias member, each answering /health itself - the health
// checker canary never routes through the LLM client).
// ---------------------------------------------------------------------------

// memberStub is one OpenAI-compatible stub endpoint: it records every
// /v1/chat/completions request and serves a scripted completion (or an
// empty one - the ErrEmptyResponse lane - or a quota shape) per the stub's
// mode.
type memberStub struct {
	mu     sync.Mutex
	mode   string // "ok" | "empty" | "quota"
	quota  llmShape
	hits   int
	server *httptest.Server
	v1     string // URL + /v1 for models.json5 / ModelConfig.BaseURL
}

// llmShape is the quota payload shape (mirrors harness.QuotaResponse).
type llmShape struct {
	Code     string
	ResetsAt int64
}

func (s *memberStub) setMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *memberStub) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// base returns the stub's baseURL (URL + /v1).
func (s *memberStub) base() string {
	return s.v1
}

func newMemberStub(t *testing.T, mode string) *memberStub {
	t.Helper()
	stub := &memberStub{mode: mode}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.hits++
		mode := stub.mode
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "empty":
			// 200 OK with blank content: the ErrEmptyResponse flake lane
			// (parseResponseWithTools classifies it; the client burns its
			// short-retry budget, then the bare sentinel rotates).
			_, _ = io.WriteString(w, `{"id":"stub","object":"chat.completion","created":0,"model":"stub-model","choices":[{"index":0,"message":{"role":"assistant","content":"   "},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		case "quota":
			// 429 + the quota wire shape the client parses into
			// *QuotaResetError (parseQuotaBody).
			body, _ := json.Marshal(map[string]any{
				"error": map[string]any{
					"type":      "usage_limit_reached",
					"message":   "stub quota",
					"resets_at": stub.quota.ResetsAt,
				},
			})
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write(body)
		default:
			_, _ = fmt.Fprintf(w, `{"id":"stub","object":"chat.completion","created":0,"model":"stub-model","choices":[{"index":0,"message":{"role":"assistant","content":"stub ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		}
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	stub.server = server
	stub.v1 = server.URL + "/v1"
	return stub
}

// tierManager builds a REAL ProviderManager over the given member stubs
// (one real OpenAI-compatible Client per stub, never a hand-rolled
// chatter) and installs the given tier policy. This is the exported
// construction seam the daemon itself uses (NewProviderManager +
// SetTierPolicy in components.go) - the seam the missing daemon call site
// will consume. Members are named prov-1..N in priority order; each
// carries the completion capability (plus any extras given).
func tierManager(t *testing.T, policy *llm.TierPolicy, stubs []*memberStub) *llm.ProviderManager {
	t.Helper()
	cfgs := make([]*llm.ModelConfig, 0, len(stubs))
	for i, st := range stubs {
		cfgs = append(cfgs, &llm.ModelConfig{
			ProviderID:   fmt.Sprintf("prov-%d", i+1),
			ModelID:      "stub-model",
			BaseURL:      st.base(),
			APIKey:       fmt.Sprintf("key-%d", i+1), // distinct credentials
			Timeout:      15 * time.Second,
			Capabilities: map[string]bool{"completion": true},
		})
	}
	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: cfgs,
		Logger:    slog.New(slog.DiscardHandler),
	})
	pm.SetTierPolicy(policy)
	return pm
}

// tierChat runs one non-streaming chat turn through the manager.
func tierChat(ctx context.Context, pm *llm.ProviderManager) (*llm.Response, error) {
	return pm.Chat(ctx, []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}})
}

// ---------------------------------------------------------------------------
// stub embedder endpoint (leaf 02's adapter shape: an OpenAI-compatible
// /embeddings endpoint served through vector.OpenAIProvider, the existing
// embedding path the daemon adapts onto llm.Embedder - no new embedding
// path, per leaf 02's Embedder contract).
// ---------------------------------------------------------------------------

// embedStub is a scripted OpenAI-compatible /embeddings endpoint: it maps
// the input text to a deterministic unit vector keyed on a marker word,
// records its hit count, and can be failed (500) to exercise the
// exact-only degradation.
type embedStub struct {
	mu     sync.Mutex
	hits   int
	fail   bool
	server *httptest.Server
}

func (s *embedStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func (s *embedStub) setFail(f bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = f
}

// newEmbedStub starts the embedder stub; the returned provider is the
// REAL vector.OpenAIProvider pointed at it (leaf 02's adapter shape).
func newEmbedStub(t *testing.T) (*embedStub, *vector.OpenAIProvider) {
	t.Helper()
	stub := &embedStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/embeddings", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.hits++
		fail := stub.fail
		stub.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"embedder down"}}`)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		vecs := make([]map[string]any, 0, len(body.Input))
		for _, text := range body.Input {
			// Deterministic 4-dim vector keyed on the marker word: texts
			// sharing the marker get cosine 1.0; different markers are
			// orthogonal (cosine 0) - far below the 0.95 floor.
			markers := map[string][]float64{
				"quorum": {1, 0, 0, 0},
				"budget": {0, 1, 0, 0},
			}
			v := []float64{0, 0, 0, 1}
			for marker, dir := range markers {
				if contains(text, marker) {
					v = dir
					break
				}
			}
			vecs = append(vecs, map[string]any{
				"object":    "embedding",
				"index":     len(vecs),
				"embedding": v,
			})
		}
		_, _ = fmt.Fprintf(w, `{"object":"list","data":%s,"model":"stub-embed","usage":{"prompt_tokens":1,"total_tokens":1}}`, mustJSON(vecs))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	stub.server = server
	prov := vector.NewOpenAIProvider(vector.OpenAIProviderConfig{
		BaseURL: server.URL,
		Model:   "stub-embed",
		Logger:  slog.New(slog.DiscardHandler),
	})
	return stub, prov
}

// embedderAdapter adapts the REAL vector provider onto the REAL
// llm.Embedder seam (the adapter shape leaf 02 names: the daemon adapts
// the existing memory/vector embedding provider onto llm.Embedder).
type embedderAdapter struct {
	prov *vector.OpenAIProvider
}

func (a embedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return a.prov.GenerateEmbedding(ctx, text)
}

// semanticCache builds a REAL llm.SemanticCache over the embedder with the
// given enabled posture.
func semanticCache(prov *vector.OpenAIProvider, enabled bool) *llm.SemanticCache {
	return llm.NewSemanticCache(embedderAdapter{prov: prov}, llm.SemanticCacheConfig{
		Enabled:   enabled,
		Threshold: llm.DefaultSemanticThreshold,
		// The stub serves 4-dim vectors; nothing to configure for that.
		MaxEntries: llm.DefaultSemanticMaxEntries,
		TTL:        llm.DefaultSemanticTTL,
	})
}

// ---------------------------------------------------------------------------
// tiny stdlib helpers (avoid pulling strings just for one call at depth)
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
