package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

const pfInlineStore = `{
  "model": "fake",
  "dimension": 4,
  "built_at": "t",
  "corpus": "c",
  "k": 5,
  "examples": [
    {"intent": "chat", "agent": "chat", "text": "a", "vector": [0, 1, 0, 0]},
    {"intent": "chat", "agent": "chat", "text": "b", "vector": [0, 1, 0, 0]},
    {"intent": "chat", "agent": "chat", "text": "c", "vector": [0, 1, 0, 0]},
    {"intent": "chat", "agent": "chat", "text": "d", "vector": [0, 1, 0, 0]},
    {"intent": "chat", "agent": "chat", "text": "e", "vector": [0, 1, 0, 0]}
  ]
}`

const pfCodeStore = `{
  "model": "fake",
  "dimension": 4,
  "built_at": "t",
  "corpus": "c",
  "k": 5,
  "examples": [
    {"intent": "code", "agent": "coder", "text": "a", "vector": [1, 0, 0, 0]},
    {"intent": "code", "agent": "coder", "text": "b", "vector": [1, 0, 0, 0]},
    {"intent": "code", "agent": "coder", "text": "c", "vector": [1, 0, 0, 0]},
    {"intent": "code", "agent": "coder", "text": "d", "vector": [1, 0, 0, 0]},
    {"intent": "code", "agent": "coder", "text": "e", "vector": [1, 0, 0, 0]}
  ]
}`

// newPrefilterDispatcher builds a dispatcher with an enabled prefilter
// backed by the given store JSON and a fake embedder pinned to axis-1
// (the chat examples) or axis-0 (the code examples). The optional mutator
// adjusts the PrefilterConfig before construction.
func newPrefilterDispatcher(t *testing.T, store string, vec []float64, mutate func(*config.ClassifierPrefilterConfig)) *Dispatcher {
	t.Helper()
	path := filepath.Join(t.TempDir(), "centroids.json")
	if err := os.WriteFile(path, []byte(store), 0o600); err != nil {
		t.Fatal(err)
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return vec, nil
	})
	cfg := config.ClassifierPrefilterConfig{
		Enabled:       true,
		BaseURL:       "http://127.0.0.1:1/v1", // never dialed; embedder is injected below
		CentroidsPath: path,
		Dimension:     4,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	d := NewDispatcher(DispatcherConfig{
		Logger:          testLogger(),
		PrefilterConfig: cfg,
	})
	if d.prefilter == nil {
		t.Fatal("prefilter not constructed despite enabled config")
	}
	d.prefilter.embedder = emb
	return d
}

// TestDispatcher_PrefilterDirectRouteSkipsClassifierChain verifies the
// Stage-0 contract for INLINE intents (chat/status/etc.): with the
// prefilter enabled, a confident unanimous vote routes DIRECTLY
// (method=embedding_prefilter) without any LLM classification. H6 gates
// the direct route to inline intents only — chat has
// ShouldCreateTask()==false and ShouldDispatchAsync()==false, so the
// verdict is honored.
func TestDispatcher_PrefilterDirectRouteSkipsClassifierChain(t *testing.T) {
	// Near-axis-1 query (cos ≈ 0.99 to the chat examples, below
	// selfMatchCutoff). Note: no LLMClient, no CapabilityMatcher — any
	// classification beyond the prefilter would NOT reliably land on
	// chat, so the direct route is attributable to Stage-0 alone.
	d := newPrefilterDispatcher(t, pfInlineStore, []float64{0.1, 0.99, 0, 0}, nil)

	res, err := d.ClassifyAndRoute(context.Background(), "hello there friend how are you doing today", "session-pf-1", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent")
	}
	if res.Intent.Method != "embedding_prefilter" {
		t.Fatalf("method = %q, want embedding_prefilter (chain ran instead of prefilter)", res.Intent.Method)
	}
	if res.Intent.Type != "chat" || res.AgentID != "chat" {
		t.Errorf("routed %s/%s, want chat/chat", res.Intent.Type, res.AgentID)
	}
}

// TestDispatcher_PrefilterTaskIntentFallsThrough verifies the H6 safety
// gate: the prefilter direct route bypasses instruction parsing,
// multi-intent detection, agent override, planning and task creation, so
// a verdict whose intent would create a task (code: ShouldCreateTask()
// == true) MUST be suppressed and the full chain must run instead.
func TestDispatcher_PrefilterTaskIntentFallsThrough(t *testing.T) {
	d := newPrefilterDispatcher(t, pfCodeStore, []float64{0.99, 0.1, 0, 0}, nil)

	res, err := d.ClassifyAndRoute(context.Background(), "please write some code for me", "session-pf-task-1", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent (chain must still classify)")
	}
	if res.Intent.Method == "embedding_prefilter" {
		t.Fatal("task-creating intent (code) routed via prefilter direct route — must fall through to the full chain")
	}
}

// TestDispatcher_PrefilterAgentOverrideFallsThrough verifies that an
// explicit client agent override disables the prefilter direct route
// entirely: the override is resolved in step 5.3 of the full chain,
// which the direct route would bypass.
func TestDispatcher_PrefilterAgentOverrideFallsThrough(t *testing.T) {
	d := newPrefilterDispatcher(t, pfInlineStore, []float64{0.1, 0.99, 0, 0}, nil)

	res, err := d.ClassifyAndRoute(context.Background(), "hello there friend how are you doing today", "session-pf-override-1", nil, "planner")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent (chain must still classify)")
	}
	if res.Intent.Method == "embedding_prefilter" {
		t.Fatal("prefilter direct route honored despite client agent override — must fall through")
	}
}

// TestDispatcher_PrefilterCompoundSignalStillExcluded pins the pre-existing
// exclusion: compound-signal inputs never hit the prefilter, regardless of
// verdict availability.
func TestDispatcher_PrefilterCompoundSignalStillExcluded(t *testing.T) {
	calls := 0
	path := filepath.Join(t.TempDir(), "centroids.json")
	if err := os.WriteFile(path, []byte(pfInlineStore), 0o600); err != nil {
		t.Fatal(err)
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		calls++
		return []float64{0.1, 0.99, 0, 0}, nil
	})
	d := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
		PrefilterConfig: config.ClassifierPrefilterConfig{
			Enabled:       true,
			BaseURL:       "http://127.0.0.1:1/v1",
			CentroidsPath: path,
			Dimension:     4,
		},
	})
	d.prefilter.embedder = emb

	// Long input (>= compoundKeywordThreshold) containing a compound signal
	// word — the prefilter must not even be consulted.
	in := "review the deployment and also create a summary of everything that happened during the release process this week"
	if !hasCompoundSignalWords(in) {
		t.Fatal("test input lost its compound signal (fixture drift)")
	}
	res, err := d.ClassifyAndRoute(context.Background(), in, "session-pf-compound-1", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if calls != 0 {
		t.Fatalf("prefilter embedder consulted %d times for compound-signal input; must be excluded", calls)
	}
	if res.Intent != nil && res.Intent.Method == "embedding_prefilter" {
		t.Fatal("compound-signal input routed via prefilter")
	}
}

// TestDispatcher_PrefilterAssertOnlyNeverRoutes verifies the assert-mode
// contract: the prefilter RUNS and produces a verdict, but the dispatcher
// must fall through to the LLM chain — the final classification method is
// NOT embedding_prefilter.
func TestDispatcher_PrefilterAssertOnlyNeverRoutes(t *testing.T) {
	d := newPrefilterDispatcher(t, pfCodeStore, []float64{0.99, 0.1, 0, 0}, func(c *config.ClassifierPrefilterConfig) {
		c.AssertOnly = true
	})
	if !d.prefilter.assertOnly {
		t.Fatal("assertOnly flag not propagated")
	}

	res, err := d.ClassifyAndRoute(context.Background(), "please write some code for me", "session-assert-1", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent (chain must still classify)")
	}
	if res.Intent.Method == "embedding_prefilter" {
		t.Fatal("assert_only mode routed via prefilter — must always fall through")
	}
}

// TestDispatcher_PrefilterDisabledUnchanged verifies the disabled path:
// without PrefilterConfig.Enabled the dispatcher constructs no prefilter
// and behavior is byte-identical to the legacy chain (nil prefilter field).
func TestDispatcher_PrefilterDisabledUnchanged(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
	})
	if d.prefilter != nil {
		t.Fatal("prefilter constructed with disabled config")
	}
}

// TestDispatcher_PrefilterMissingBaseURLInert verifies the guard: Enabled
// without BaseURL constructs no prefilter (daemon logs the gap, chain
// unchanged).
func TestDispatcher_PrefilterMissingBaseURLInert(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
		PrefilterConfig: config.ClassifierPrefilterConfig{
			Enabled:       true,
			CentroidsPath: filepath.Join(t.TempDir(), "c.json"),
		},
	})
	if d.prefilter != nil {
		t.Fatal("prefilter constructed without base_url")
	}
}

// TestDispatcher_PrefilterTimeoutPropagation verifies M15: the embed
// client built by NewDispatcher honors cfg.PrefilterConfig.TimeoutSeconds
// (with the same <=0 → default normalization NewEmbeddingPrefilter uses)
// instead of the hardcoded 2s default.
func TestDispatcher_PrefilterTimeoutPropagation(t *testing.T) {
	build := func(timeoutSeconds int) *Dispatcher {
		d := NewDispatcher(DispatcherConfig{
			Logger: testLogger(),
			PrefilterConfig: config.ClassifierPrefilterConfig{
				Enabled:        true,
				BaseURL:        "http://127.0.0.1:1/v1",
				CentroidsPath:  filepath.Join(t.TempDir(), "c.json"),
				TimeoutSeconds: timeoutSeconds,
			},
		})
		if d.prefilter == nil {
			t.Fatal("prefilter not constructed despite enabled config")
		}
		return d
	}

	d := build(7)
	emb, ok := d.prefilter.embedder.(*openAIEmbedClient)
	if !ok {
		t.Fatalf("embedder is %T, want *openAIEmbedClient", d.prefilter.embedder)
	}
	if emb.http.Timeout != 7*time.Second {
		t.Errorf("embed client timeout = %v, want 7s (config TimeoutSeconds honored)", emb.http.Timeout)
	}

	// Zero falls back to the default (same normalization as
	// NewEmbeddingPrefilter).
	d0 := build(0)
	emb0, ok := d0.prefilter.embedder.(*openAIEmbedClient)
	if !ok {
		t.Fatalf("embedder is %T, want *openAIEmbedClient", d0.prefilter.embedder)
	}
	if emb0.http.Timeout != defaultPrefilterTimeout {
		t.Errorf("embed client timeout = %v, want default %v (TimeoutSeconds=0)", emb0.http.Timeout, defaultPrefilterTimeout)
	}
}
