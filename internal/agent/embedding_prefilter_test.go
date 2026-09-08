package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// fakeEmbedder returns the centroid vector of the "intent" it is told to
// mimic, optionally with noise, so threshold/margin behavior is testable
// without a model.
type fakeEmbedder struct {
	mu        map[string][]float64 // intent name -> centroid vector
	dimension int
	err       error
	// bias, when non-nil, rewrites the returned vector for assertions.
	bias func(text string) ([]float64, bool)
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.bias != nil {
		if vec, ok := f.bias(text); ok {
			return vec, nil
		}
	}
	// Default: return the code centroid for any input mentioning "code",
	// chat centroid otherwise.
	if strings.Contains(text, "code") {
		if v, ok := f.mu["code"]; ok {
			return v, nil
		}
	}
	if v, ok := f.mu["chat"]; ok {
		return v, nil
	}
	return make([]float64, f.dimension), nil
}

func testCentroids(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	store := map[string]any{
		"model":     "fake",
		"dimension": 4,
		"built_at":  "2026-09-07T00:00:00Z",
		"corpus":    "fake",
		"centroids": []map[string]any{
			{"intent": "code", "agent": "coder", "count": 2, "vector": []float64{1, 0, 0, 0}},
			{"intent": "chat", "agent": "chat", "count": 2, "vector": []float64{0, 1, 0, 0}},
		},
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "centroids.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testPrefilter(t *testing.T, emb PrefilterEmbedder, mutate func(*config.ClassifierPrefilterConfig)) *EmbeddingPrefilter {
	t.Helper()
	path := testCentroids(t)
	cfg := config.ClassifierPrefilterConfig{
		Enabled:       true,
		CentroidsPath: path,
		Dimension:     4,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return NewEmbeddingPrefilter(emb, cfg, testLogger())
}

// TestPrefilter_DirectRouteAboveThreshold: exact-centroid match routes
// directly with the stored intent + agent and the prefilter method tag.
func TestPrefilter_DirectRouteAboveThreshold(t *testing.T) {
	emb := &fakeEmbedder{mu: map[string][]float64{
		"code": {1, 0, 0, 0},
		"chat": {0, 1, 0, 0},
	}}
	p := testPrefilter(t, emb, nil)

	intent := p.Match(context.Background(), "please write code for me")
	if intent == nil {
		t.Fatal("expected direct route, got nil")
	}
	if intent.Type != "code" || intent.AgentType != "coder" {
		t.Errorf("intent = %s/%s, want code/coder", intent.Type, intent.AgentType)
	}
	if intent.Method != "embedding_prefilter" {
		t.Errorf("method = %q, want embedding_prefilter", intent.Method)
	}
	if intent.Confidence < DefaultPrefilterThreshold {
		t.Errorf("confidence = %v, want >= %v", intent.Confidence, DefaultPrefilterThreshold)
	}
}

// TestPrefilter_BelowThresholdFallsThrough: an ambiguous vector (equidistant
// between centroids) must return nil — the LLM chain takes over.
func TestPrefilter_BelowThresholdFallsThrough(t *testing.T) {
	emb := &fakeEmbedder{}
	emb.mu = map[string][]float64{}
	p := testPrefilter(t, emb, func(c *config.ClassifierPrefilterConfig) {
		c.Threshold = 0.99
	})

	// 45° vector between the code and chat centroids → score ~0.707 < 0.99.
	intent := p.Match(context.Background(), "mixed signals neither way")
	if intent != nil {
		t.Fatalf("expected nil on low score, got %+v", intent)
	}
}

// TestPrefilter_MarginRejectsRunnerUpTies: two centroids at the same high
// score must NOT direct-route even though both clear the threshold — a tie
// is exactly the ambiguity the prefilter must not guess on.
func TestPrefilter_MarginRejectsRunnerUpTies(t *testing.T) {
	emb := &fakeEmbedder{mu: map[string][]float64{
		"code": {0.8, 0.8, 0, 0}, // cosine vs both centroids identical
		"chat": {0, 0, 0, 0},
	}}
	p := testPrefilter(t, emb, func(c *config.ClassifierPrefilterConfig) {
		c.Threshold = 0.5
	})

	intent := p.Match(context.Background(), "tied between code and chat")
	if intent != nil {
		t.Fatalf("expected nil on margin tie, got %+v", intent)
	}
}

// TestPrefilter_EmbedErrorFallsThrough: embedder failure returns nil (LLM
// chain) and never an error — the prefilter can only skip work.
func TestPrefilter_EmbedErrorFallsThrough(t *testing.T) {
	emb := &fakeEmbedder{err: errors.New("server down")}
	p := testPrefilter(t, emb, nil)

	if intent := p.Match(context.Background(), "anything"); intent != nil {
		t.Fatalf("expected nil on embed error, got %+v", intent)
	}
}

// TestPrefilter_EmptyInputSkipsEmbedCall: blank input returns nil without
// hitting the embedder.
func TestPrefilter_EmptyInputSkipsEmbedCall(t *testing.T) {
	calls := 0
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		calls++
		return []float64{1, 0, 0, 0}, nil
	})
	p := testPrefilter(t, emb, nil)

	if intent := p.Match(context.Background(), "   "); intent != nil {
		t.Fatal("expected nil for empty input")
	}
	if calls != 0 {
		t.Fatalf("embedder called %d times for empty input", calls)
	}
}

// TestPrefilter_DimensionMismatchFallsThrough: config-declared dimension
// mismatch degrades to the LLM chain with a warning, not an error.
func TestPrefilter_DimensionMismatchFallsThrough(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{1, 0}, nil // 2-dim, store/config expect 4
	})
	p := testPrefilter(t, emb, nil)

	if intent := p.Match(context.Background(), "some input"); intent != nil {
		t.Fatal("expected nil on dimension mismatch")
	}
}

// TestPrefilter_MissingStoreInert: no centroids file → permanently nil,
// no panic, warning logged once.
func TestPrefilter_MissingStoreInert(t *testing.T) {
	cfg := config.ClassifierPrefilterConfig{
		Enabled:       true,
		CentroidsPath: filepath.Join(t.TempDir(), "absent.json"),
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		t.Fatal("embedder must not be called when store is missing")
		return nil, nil
	})
	p := NewEmbeddingPrefilter(emb, cfg, testLogger())

	for i := 0; i < 3; i++ {
		if intent := p.Match(context.Background(), "input"); intent != nil {
			t.Fatal("expected nil with missing store")
		}
	}
}

// TestPrefilter_ReloadPicksUpNewStore: Reload() forces re-read; a rebuilt
// store is honored without constructing a new prefilter.
func TestPrefilter_ReloadPicksUpNewStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "centroids.json")
	writeStore := func(intents []map[string]any) {
		t.Helper()
		store := map[string]any{
			"model": "fake", "dimension": 4, "built_at": "t", "corpus": "c",
			"centroids": intents,
		}
		data, err := json.Marshal(store)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeStore([]map[string]any{
		{"intent": "old", "agent": "chat", "count": 1, "vector": []float64{1, 0, 0, 0}},
	})

	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{1, 0, 0, 0}, nil
	})
	p := NewEmbeddingPrefilter(emb, config.ClassifierPrefilterConfig{
		Enabled: true, CentroidsPath: path, Dimension: 4,
	}, testLogger())

	if got := p.Match(context.Background(), "x"); got == nil || got.Type != "old" {
		t.Fatalf("first load: got %+v, want old", got)
	}

	writeStore([]map[string]any{
		{"intent": "new", "agent": "chat", "count": 1, "vector": []float64{1, 0, 0, 0}},
	})
	p.Reload()
	if got := p.Match(context.Background(), "x"); got == nil || got.Type != "new" {
		t.Fatalf("after reload: got %+v, want new", got)
	}
}

// embedFunc adapts a closure to PrefilterEmbedder.
type embedFunc func(ctx context.Context, text string) ([]float64, error)

func (f embedFunc) Embed(ctx context.Context, text string) ([]float64, error) {
	return f(ctx, text)
}
