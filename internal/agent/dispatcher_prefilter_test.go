package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestDispatcher_PrefilterDirectRouteSkipsClassifierChain verifies the
// Stage-0 contract end to end: with the prefilter enabled, a confident
// centroid hit routes DIRECTLY (method=embedding_prefilter) without any
// LLM classification. The fake embedder returns a vector identical to the
// code centroid, so the prefilter must decide before classifyIntent's
// keyword/LLM chain would.
func TestDispatcher_PrefilterDirectRouteSkipsClassifierChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "centroids.json")
	store := `{
  "model": "fake",
  "dimension": 4,
  "built_at": "t",
  "corpus": "c",
  "centroids": [
    {"intent": "code", "agent": "coder", "count": 1, "vector": [1, 0, 0, 0]},
    {"intent": "chat", "agent": "chat", "count": 1, "vector": [0, 1, 0, 0]}
  ]
}`
	if err := os.WriteFile(path, []byte(store), 0o600); err != nil {
		t.Fatal(err)
	}

	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{1, 0, 0, 0}, nil // exactly the code centroid
	})

	logger := testLogger()
	d := NewDispatcher(DispatcherConfig{
		Logger: logger,
		PrefilterConfig: config.ClassifierPrefilterConfig{
			Enabled:       true,
			BaseURL:       "http://127.0.0.1:1/v1", // never dialed; embedder is injected below
			CentroidsPath: path,
			Dimension:     4,
			Threshold:     0.90,
		},
	})
	// Inject the fake embedder into the prefilter the dispatcher built.
	if d.prefilter == nil {
		t.Fatal("prefilter not constructed despite enabled config")
	}
	d.prefilter.embedder = emb

	// Note: no LLMClient, no CapabilityMatcher — any classification beyond
	// the prefilter would fall to keyword/heuristic, NOT code/coder at this
	// confidence. The direct route is attributable to Stage-0 alone.
	res, err := d.ClassifyAndRoute(context.Background(), "please write some code for me", "session-pf-1", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent")
	}
	if res.Intent.Method != "embedding_prefilter" {
		t.Fatalf("method = %q, want embedding_prefilter (chain ran instead of prefilter)", res.Intent.Method)
	}
	if res.Intent.Type != "code" || res.AgentID != "coder" {
		t.Errorf("routed %s/%s, want code/coder", res.Intent.Type, res.AgentID)
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
