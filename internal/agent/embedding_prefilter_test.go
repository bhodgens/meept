package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// knnStore is the on-disk kNN index format (scripts/build_prefilter_
// centroids.py "examples" array).
func writeKNNStore(t *testing.T, path string, examples []map[string]any) {
	t.Helper()
	store := map[string]any{
		"model": "fake", "dimension": 4, "built_at": "t", "corpus": "c",
		"k":        5,
		"examples": examples,
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// basisVec returns the unit 4-dim vector along one of the 4 axes — lets
// tests construct crisp similarity geometry.
func basisVec(i int) []float64 {
	v := make([]float64, 4)
	v[i%4] = 1
	return v
}

// nearAxis0 returns a query vector at cos ≈ 0.995 to axis 0 — near enough
// to vote with axis-0 examples, far enough (below selfMatchCutoff) to not
// be treated as a self-match.
func nearAxis0() []float64 {
	return []float64{0.99, 0.1, 0, 0}
}

func exampleSet() []map[string]any {
	// 5 code examples on axis 0, 5 chat on axis 1 — exactly k=5 per
	// intent, so a perfect axis query votes, and anything off-axis dies
	// on unanimity or floor.
	ex := make([]map[string]any, 0, 10)
	for i := 0; i < 5; i++ {
		ex = append(ex, map[string]any{
			"intent": "code", "agent": "coder", "text": fmt.Sprintf("code ex %d", i),
			"vector": basisVec(0),
		})
	}
	for i := 0; i < 5; i++ {
		ex = append(ex, map[string]any{
			"intent": "chat", "agent": "chat", "text": fmt.Sprintf("chat ex %d", i),
			"vector": basisVec(1),
		})
	}
	return ex
}

func knnPrefilter(t *testing.T, emb PrefilterEmbedder, examples []map[string]any, mutate func(*config.ClassifierPrefilterConfig)) *EmbeddingPrefilter {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knn.json")
	writeKNNStore(t, path, examples)
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

// TestPrefilter_KNNUnanimousDirectRoute: query exactly on axis 0 → all 5
// code neighbors agree → direct route with the stored intent/agent and the
// unanimity floor as confidence.
func TestPrefilter_KNNUnanimousDirectRoute(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis0(), nil
	})
	p := knnPrefilter(t, emb, exampleSet(), nil)

	intent := p.Match(context.Background(), "please write code")
	if intent == nil {
		t.Fatal("expected direct route, got nil")
	}
	if intent.Type != "code" || intent.AgentType != "coder" {
		t.Errorf("intent = %s/%s, want code/coder", intent.Type, intent.AgentType)
	}
	if intent.Method != "embedding_prefilter" {
		t.Errorf("method = %q, want embedding_prefilter", intent.Method)
	}
	if intent.Confidence < 0.99 {
		t.Errorf("confidence = %v, want ~1.0 (exact axis match)", intent.Confidence)
	}
}

// TestPrefilter_KNNDissenterAbstains: mixed top-5 (4 chat near axis 1 +
// 1 code tilted toward the query, all above the floor) → the code
// dissenter kills the vote → nil. THE core precision property.
func TestPrefilter_KNNDissenterAbstains(t *testing.T) {
	// chat examples sit exactly on axis 1 (cos 0.80 to query); the single
	// code example is tilted to score 0.89 — it ranks FIRST but is alone
	// against 4 chat examples. Top-5 = 1 code + 4 chat → dissent.
	tiltedCode := []float64{0.9, 0.44, 0, 0}
	ex := exampleSet() // 5 code on axis 0, 5 chat on axis 1
	ex[0] = map[string]any{ // replace one pure-code with the tilted one
		"intent": "code", "agent": "coder", "text": "tilted code ex",
		"vector": tiltedCode,
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{0.6, 0.8, 0, 0}, nil
	})
	p := knnPrefilter(t, emb, ex, nil)

	if intent := p.Match(context.Background(), "ambiguous input"); intent != nil {
		t.Fatalf("expected nil on mixed vote, got %+v", intent)
	}
}

// TestPrefilter_KNNTiltedUnanimousRoutes: same geometry but with the code
// side holding 5 tilted examples — top-5 unanimous code → routes with the
// tilted winner's agent.
func TestPrefilter_KNNTiltedUnanimousRoutes(t *testing.T) {
	tiltedCode := []float64{0.9, 0.44, 0, 0}
	ex := make([]map[string]any, 0, 10)
	for i := 0; i < 5; i++ {
		ex = append(ex, map[string]any{
			"intent": "code", "agent": "coder", "text": "tilted",
			"vector": tiltedCode,
		})
	}
	for i := 0; i < 5; i++ {
		ex = append(ex, map[string]any{
			"intent": "chat", "agent": "chat", "text": "chat",
			"vector": basisVec(1),
		})
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{0.6, 0.8, 0, 0}, nil
	})
	p := knnPrefilter(t, emb, ex, nil)

	intent := p.Match(context.Background(), "code please")
	if intent == nil {
		t.Fatal("expected direct route")
	}
	if intent.Type != "code" {
		t.Errorf("intent = %s, want code", intent.Type)
	}
	// confidence = unanimity floor = min cosine among the 5 winners ≈ 0.89
	if intent.Confidence < 0.88 || intent.Confidence > 0.91 {
		t.Errorf("confidence = %v, want ~0.89 (tilted floor)", intent.Confidence)
	}
}

// TestPrefilter_KNNSparseAbstains: fewer than k neighbors above the
// threshold floor → no vote → nil.
func TestPrefilter_KNNSparseAbstains(t *testing.T) {
	// Query on axis 3 (nothing lives there): zero neighbors above any
	// reasonable floor.
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return basisVec(3), nil
	})
	p := knnPrefilter(t, emb, exampleSet(), nil)

	if intent := p.Match(context.Background(), "out of distribution"); intent != nil {
		t.Fatalf("expected nil on sparse neighborhood, got %+v", intent)
	}
}

// TestPrefilter_KNNSmallIndexCannotVote: fewer than k examples TOTAL in
// the index → permanently nil (never routes on thin evidence).
func TestPrefilter_KNNSmallIndexCannotVote(t *testing.T) {
	ex := []map[string]any{
		{"intent": "code", "agent": "coder", "vector": basisVec(0)},
		{"intent": "code", "agent": "coder", "vector": basisVec(0)},
	}
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis0(), nil
	})
	p := knnPrefilter(t, emb, ex, nil)

	if intent := p.Match(context.Background(), "anything"); intent != nil {
		t.Fatal("expected nil with fewer than k examples")
	}
}

// TestPrefilter_EmbedErrorFallsThrough: embedder failure returns nil (LLM
// chain) and never an error — the prefilter can only skip work.
func TestPrefilter_EmbedErrorFallsThrough(t *testing.T) {
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nil, errors.New("server down")
	})
	p := knnPrefilter(t, emb, exampleSet(), nil)

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
		return nearAxis0(), nil
	})
	p := knnPrefilter(t, emb, exampleSet(), nil)

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
	p := knnPrefilter(t, emb, exampleSet(), nil)

	if intent := p.Match(context.Background(), "some input"); intent != nil {
		t.Fatal("expected nil on dimension mismatch")
	}
}

// TestPrefilter_MissingStoreInert: no index file → permanently nil, no
// panic, warning logged once.
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

// TestPrefilter_LegacyCentroidStoreLoads: old centroid-format stores
// (schema from the first cut) load as pseudo-examples and keep the
// prefilter working across the upgrade. With k equal to the entry count
// and a query near both centroids, both pseudo-examples vote — but they
// DISAGREE (code vs chat), so the vote dies. What matters here: the
// legacy store LOADED (not "unreadable"/inert), which the missing-index
// test already proves is the alternative outcome.
func TestPrefilter_LegacyCentroidStoreLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	store := map[string]any{
		"model": "fake", "dimension": 4, "built_at": "t", "corpus": "c",
		"centroids": []map[string]any{
			{"intent": "code", "agent": "coder", "count": 2, "vector": basisVec(0)},
			{"intent": "chat", "agent": "chat", "count": 2, "vector": basisVec(1)},
		},
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Query 45° between the two centroids: both pseudo-examples clear any
	// sane floor, both vote, they dissent → nil. Proves the legacy store
	// loaded and participates in voting.
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return []float64{0.7071, 0.7071, 0, 0}, nil
	})
	p := NewEmbeddingPrefilter(emb, config.ClassifierPrefilterConfig{
		Enabled: true, CentroidsPath: path, Dimension: 4,
	}, testLogger())
	p.k = 2 // legacy store has 2 entries; k must not exceed it to vote

	if intent := p.Match(context.Background(), "x"); intent != nil {
		t.Fatalf("legacy store with dissenting centroids: got %+v, want nil (vote killed)", intent)
	}

	// Same store, unanimous geometry: query on the code axis, k=1 — only
	// the code pseudo-example is in the top-1 → routes.
	p.k = 1
	if intent := p.Match(context.Background(), "x"); intent == nil || intent.Type != "code" {
		t.Fatalf("legacy store k=1: got %+v, want code direct route", intent)
	}
}

// TestPrefilter_ReloadPicksUpNewStore: Reload() forces re-read; a rebuilt
// index is honored without constructing a new prefilter.
func TestPrefilter_ReloadPicksUpNewStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knn.json")
	writeKNNStore(t, path, []map[string]any{
		{"intent": "old", "agent": "chat", "vector": basisVec(0)},
		{"intent": "old", "agent": "chat", "vector": basisVec(0)},
	})
	emb := embedFunc(func(_ context.Context, _ string) ([]float64, error) {
		return nearAxis0(), nil
	})
	p := NewEmbeddingPrefilter(emb, config.ClassifierPrefilterConfig{
		Enabled: true, CentroidsPath: path, Dimension: 4,
	}, testLogger())
	p.k = 2

	if got := p.Match(context.Background(), "x"); got == nil || got.Type != "old" {
		t.Fatalf("first load: got %+v, want old", got)
	}

	writeKNNStore(t, path, []map[string]any{
		{"intent": "new", "agent": "chat", "vector": basisVec(0)},
		{"intent": "new", "agent": "chat", "vector": basisVec(0)},
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
