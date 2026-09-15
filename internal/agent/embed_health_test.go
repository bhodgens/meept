package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// writeRefJSON writes a reference-set JSON file and returns its path.
func writeRefJSON(t *testing.T, vecs [][]float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ref.json")
	data, err := json.Marshal(vecs)
	if err != nil {
		t.Fatalf("marshal reference: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}
	return path
}

// writeRawJSON writes arbitrary bytes as a "reference" file (corrupt case).
func writeRawJSON(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ref.json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write raw reference: %v", err)
	}
	return path
}

// TestNewEmbedHealthCheckErrors covers the fail-closed constructor: missing,
// corrupt, and degenerate reference sets must return an error, never a
// silently-disabled check.
func TestNewEmbedHealthCheckErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string // literal file bytes ("" = valid 2-D array from rows)
		rows [][]float64
		want bool
	}{
		{name: "missing file", want: true},
		{
			name: "corrupt JSON",
			raw:  "{not json at all",
			want: true,
		},
		{
			name: "empty array",
			rows: [][]float64{},
			want: true,
		},
		{
			name: "single vector (cannot calibrate)",
			rows: [][]float64{{1, 0, 0}},
			want: true,
		},
		{
			name: "ragged rows",
			rows: [][]float64{{1, 0, 0}, {0, 1}},
			want: true,
		},
		{
			// encoding/json cannot marshal NaN/Inf, so write the literal bytes.
			name: "NaN in reference",
			raw:  "[[1,0,0],[NaN,1,0]]",
			want: true,
		},
		{
			name: "Inf in reference",
			raw:  "[[1,0,0],[Infinity,1,0]]",
			want: true,
		},
		{
			name: "zero vector in reference",
			rows: [][]float64{{1, 0, 0}, {0, 0, 0}},
			want: true,
		},
		{
			name: "valid reference loads",
			rows: [][]float64{
				{1, 0.1, 0}, {0.95, 0.05, 0.02}, {0.9, 0.15, 0.05}, {1.02, -0.05, 0.01},
				{0, 1, 0.1}, {0.05, 0.98, 0}, {-0.02, 1.01, 0.05}, {0.03, 0.95, -0.02},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			switch {
			case tc.raw != "":
				path = writeRawJSON(t, tc.raw)
			case tc.rows != nil:
				path = writeRefJSON(t, tc.rows)
			default:
				path = filepath.Join(t.TempDir(), "nope.json")
			}
			hc, err := NewEmbedHealthCheck(path, slog.New(slog.DiscardHandler))
			if tc.want && err == nil {
				t.Fatalf("expected error, got nil (loaded=%v)", hc != nil && hc.Loaded())
			}
			if !tc.want {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !hc.Loaded() {
					t.Fatal("check should be loaded for a valid reference")
				}
				if hc.Threshold() <= 0 || hc.Threshold() >= 1 {
					t.Errorf("threshold %v outside plausible cosine-distance range (0,1)", hc.Threshold())
				}
			}
		})
	}
}

// inertCheckConfig returns a minimal prefilter config for Match-level tests.
func inertCheckConfig(refPath string) config.ClassifierPrefilterConfig {
	return config.ClassifierPrefilterConfig{
		EmbedHealthCheck: config.EmbedHealthCheckConfig{
			Enabled:       refPath != "",
			ReferencePath: refPath,
		},
	}
}

// healthFakeEmbedder returns canned vectors in order; err is returned when
// set; with no vecs and no err it fails like a dead endpoint (Match must
// fall through, not route).
type healthFakeEmbedder struct {
	vecs [][]float64
	err  error
}

func (f *healthFakeEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.vecs) == 0 {
		return nil, errors.New("healthFakeEmbedder: no canned vectors left")
	}
	v := f.vecs[0]
	f.vecs = f.vecs[1:]
	return v, nil
}

// storeFixture is a minimal in-memory kNN store handed to the prefilter via
// the CentroidsPath override, so Match-level tests run hermetic (no
// dependency on ~/.meept state).
func storeFixture(t *testing.T, examples []prefilterExample) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.json")
	data, err := json.Marshal(prefilterStore{
		Model:     "test-model",
		Dimension: len(examples[0].Vector),
		Examples:  examples,
	})
	if err != nil {
		t.Fatalf("marshal store: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}
	return path
}

// buildPrefilterWithHealth constructs a prefilter with a hermetic kNN index
// (three code-labeled examples) and the given canned embeddings, so the
// health gate — not index availability — is the variable under test.
func buildPrefilterWithHealth(t *testing.T, refPath string, embedder PrefilterEmbedder) *EmbeddingPrefilter {
	t.Helper()
	cfg := inertCheckConfig(refPath)
	cfg.CentroidsPath = storeFixture(t, []prefilterExample{
		{Intent: "code", Agent: "coder", Vector: []float64{1, 0, 0, 0}},
		{Intent: "code", Agent: "coder", Vector: []float64{0.98, 0.05, 0, 0}},
		{Intent: "code", Agent: "coder", Vector: []float64{1.02, -0.03, 0.01, 0}},
		{Intent: "chat", Agent: "", Vector: []float64{0, 0, 1, 0}},
		{Intent: "chat", Agent: "", Vector: []float64{0.02, 0, 0.99, 0}},
		{Intent: "chat", Agent: "", Vector: []float64{-0.01, 0.01, 1.01, 0}},
	})
	p := NewEmbeddingPrefilter(embedder, cfg, slog.New(slog.DiscardHandler))
	if refPath != "" && p.embedHealth == nil {
		t.Fatal("embed health check failed to load; Match-level test is vacuous")
	}
	if !p.loadIndex(false) {
		t.Fatalf("hermetic store failed to load")
	}
	return p
}

// TestMatchHealthGateFailsSafe: an unhealthy embedding must never produce a
// direct route, even when the kNN neighborhood would have voted. The same
// prefilter with a healthy (reference-like) embedding routes normally, so
// the gate — not the store — is what blocks the degenerate case.
func TestMatchHealthGateFailsSafe(t *testing.T) {
	ref := [][]float64{
		{1, 0.05, 0, 0}, {0.98, 0.02, 0.01, 0}, {1.01, -0.03, 0, 0.02}, {0.99, 0.08, 0.01, 0},
		{0, 1, 0.05, 0}, {0.02, 0.99, 0, 0.01}, {-0.01, 1.02, 0.02, 0}, {0.03, 0.97, 0.01, 0},
	}
	refPath := writeRefJSON(t, ref)

	// Zeroed embedding: degenerate, must be gated even though the store
	// is loaded and would vote.
	p := buildPrefilterWithHealth(t, refPath, &healthFakeEmbedder{vecs: [][]float64{{0, 0, 0, 0}}})
	if pi := p.Match(context.Background(), "route me to the coder"); pi != nil {
		t.Fatalf("zeroed embedding must not route; got %+v", pi)
	}

	// Same store, healthy embedding from the reference distribution:
	// the gate passes it through. The embedding is an exact match for the
	// verbatim corpus (cos 0.9998 to example 1, above selfMatchCutoff) so
	// the kNN vote ITSELF abstains on it — but the point of this arm is
	// that the health gate is a pass-through for healthy input, not the
	// abstain decision. Probe the gate outcome directly via the internal
	// field rather than inferring from the Match verdict.
	p2 := buildPrefilterWithHealth(t, refPath, &healthFakeEmbedder{vecs: [][]float64{{1, 0.02, 0, 0}}})
	if ok, dist := p2.embedHealth.Check([]float64{1, 0.02, 0, 0}); !ok {
		t.Fatalf("healthy embedding gated: dist=%.6f thr=%.6f", dist, p2.embedHealth.Threshold())
	}
	if pi := p2.Match(context.Background(), "route me to the coder"); pi != nil {
		// Fine either way: routed directly, or abstained via
		// selfMatchCutoff and fell to the LLM chain. Never gated by
		// health (asserted above).
		_ = pi
	}
}

// goldCluster generates n deterministic unit-ish vectors around a random
// direction with small per-component jitter — a stand-in for real
// known-good embeddings (clustered, same distribution).
func goldCluster(rng *rand.Rand, n, dim int) [][]float64 {
	center := make([]float64, dim)
	for j := range center {
		center[j] = rng.NormFloat64()
	}
	out := make([][]float64, n)
	for i := range out {
		v := make([]float64, dim)
		for j := range v {
			v[j] = center[j] + 0.05*rng.NormFloat64()
		}
		out[i] = v
	}
	return out
}

// prefilterStoreMeta is just the metadata + labels of the kNN store —
// enough to stratify by intent without pulling all vectors into memory
// twice.
type prefilterStoreMeta struct {
	Examples []struct {
		Intent string `json:"intent"`
	} `json:"examples"`
}

// TestGoldEmbeddingsRetention: >= 95% of gold (held-out, not in the
// reference) embeddings must pass the check. Uses the live prefilter store
// vectors (~/.meept/classifier_prefilter_centroids.json) as gold data with
// a STRATIFIED split — the store is ordered by intent, so a naive
// first/second-half split starves the reference of whole classes and the
// held-out side of others; alternating rows within each intent block
// mirrors the class distribution on both sides. Falls back to synthetic
// clusters when no store exists (fresh checkouts still get coverage).
func TestGoldEmbeddingsRetention(t *testing.T) {
	var ref, gold [][]float64
	if vecs, intents := loadPrefilterStoreVectors(t); vecs != nil {
		ref, gold = stratifiedSplit(vecs, intents)
	} else {
		rng := rand.New(rand.NewSource(42))
		ref = goldCluster(rng, 40, 32)
		gold = goldCluster(rng, 40, 32)
	}

	hc, err := NewEmbedHealthCheck(writeRefJSON(t, ref), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("load reference: %v", err)
	}
	pass := 0
	for i, v := range gold {
		if ok, dist := hc.Check(v); ok {
			pass++
		} else {
			t.Logf("gold[%d] flagged: distance=%.4f threshold=%.4f", i, dist, hc.Threshold())
		}
	}
	rate := float64(pass) / float64(len(gold))
	if rate < 0.95 {
		t.Errorf("gold retention %.3f < 0.95 (%d/%d passed)", rate, pass, len(gold))
	}
}

// stratifiedSplit alternates rows within each contiguous same-intent block
// (the store is intent-ordered): even-index rows of a block go to the
// reference, odd-index to gold. Returns (ref, gold).
func stratifiedSplit(vecs [][]float64, intents []string) ([][]float64, [][]float64) {
	var ref, gold [][]float64
	runStart := 0
	for i := 1; i <= len(vecs); i++ {
		if i < len(vecs) && intents[i] == intents[runStart] {
			continue
		}
		for j := runStart; j < i; j++ {
			if (j-runStart)%2 == 0 {
				ref = append(ref, vecs[j])
			} else {
				gold = append(gold, vecs[j])
			}
		}
		runStart = i
	}
	return ref, gold
}

// TestDegradedEmbeddingsFlagged: zeroed, truncated, and heavily noised
// embeddings must be flagged (>= 95%). Refuses to run vacuously when the
// synthetic clusters would pass on their own (sanity arm).
func TestDegradedEmbeddingsFlagged(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const dim = 32
	ref := goldCluster(rng, 40, dim)
	hc, err := NewEmbedHealthCheck(writeRefJSON(t, ref), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("load reference: %v", err)
	}

	type degradedCase struct {
		name string
		vec  []float64
	}
	var cases []degradedCase
	for i := 0; i < 20; i++ {
		gold := goldCluster(rng, 1, dim)[0]

		zeroed := make([]float64, dim)
		copy(zeroed, gold)
		for j := range zeroed {
			zeroed[j] = 0
		}
		cases = append(cases, degradedCase{"zeroed", zeroed})

		trunc := make([]float64, dim/2)
		copy(trunc, gold[:dim/2])
		cases = append(cases, degradedCase{"truncated", trunc})

		noised := make([]float64, dim)
		for j := range noised {
			noised[j] = gold[j] + 4.0*rng.NormFloat64() // swamps the 0.05 signal
		}
		cases = append(cases, degradedCase{"noised", noised})
	}

	flagged := 0
	for _, tc := range cases {
		if ok, dist := hc.Check(tc.vec); !ok {
			flagged++
		} else {
			t.Errorf("%s embedding PASSED (distance=%.4f <= threshold=%.4f); must be flagged", tc.name, dist, hc.Threshold())
		}
	}
	if rate := float64(flagged) / float64(len(cases)); rate < 0.95 {
		t.Errorf("degraded flag rate %.3f < 0.95 (%d/%d)", rate, flagged, len(cases))
	}
}

// TestDegenerateInputFlagged: NaN, Inf, zero-norm, dimension mismatch, and
// empty embeddings are unhealthy, and never panic.
func TestDegenerateInputFlagged(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	ref := goldCluster(rng, 20, 8)
	hc, err := NewEmbedHealthCheck(writeRefJSON(t, ref), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("load reference: %v", err)
	}
	tests := []struct {
		name string
		vec  []float64
	}{
		{"NaN component", []float64{1, math.NaN(), 0, 0, 0, 0, 0, 0}},
		{"Inf component", []float64{1, math.Inf(1), 0, 0, 0, 0, 0, 0}},
		{"all zero", make([]float64, 8)},
		{"dim too short", []float64{1, 0, 0}},
		{"dim too long", make([]float64, 16)},
		{"empty", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, _ := hc.Check(tc.vec)
			if ok {
				t.Errorf("degenerate input %q reported healthy", tc.name)
			}
		})
	}
}

// TestConcurrentCheckSafe: Check from 10 goroutines under -race must be
// clean and deterministic (same input, same verdict).
func TestConcurrentCheckSafe(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	const dim = 16
	ref := goldCluster(rng, 30, dim)
	hc, err := NewEmbedHealthCheck(writeRefJSON(t, ref), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("load reference: %v", err)
	}
	input := goldCluster(rng, 1, dim)[0]
	want, _ := hc.Check(input)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				ok, _ := hc.Check(input)
				if ok != want {
					errs <- fmt.Errorf("nondeterministic verdict: got %v want %v", ok, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestInertWhenReferenceMissing: Enabled with a missing file leaves the
// gate nil (inert, warn logged once) — Match behavior unchanged.
func TestInertWhenReferenceMissing(t *testing.T) {
	cfg := config.ClassifierPrefilterConfig{
		EmbedHealthCheck: config.EmbedHealthCheckConfig{
			Enabled:       true,
			ReferencePath: filepath.Join(t.TempDir(), "absent.json"),
		},
	}
	p := NewEmbeddingPrefilter(&healthFakeEmbedder{err: context.Canceled}, cfg, slog.New(slog.DiscardHandler))
	if p.embedHealth != nil {
		t.Fatal("missing reference must leave the health check inert (nil)")
	}
}

// TestDisabledCheckNil: Enabled=false never attempts the reference load.
func TestDisabledCheckNil(t *testing.T) {
	cfg := config.ClassifierPrefilterConfig{} // Enabled defaults false
	p := NewEmbeddingPrefilter(&healthFakeEmbedder{err: context.Canceled}, cfg, slog.New(slog.DiscardHandler))
	if p.embedHealth != nil {
		t.Fatal("disabled config must not construct a health check")
	}
}

// loadPrefilterStoreVectors extracts raw vectors + intent labels from the
// live prefilter store (~/.meept/classifier_prefilter_centroids.json) for
// the gold-retention test. Returns (nil, nil) when unavailable — the test
// then falls back to synthetic clusters instead of failing.
func loadPrefilterStoreVectors(t *testing.T) ([][]float64, []string) {
	t.Helper()
	path := config.MeeptPath("classifier_prefilter_centroids.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var store struct {
		Examples []struct {
			Intent string    `json:"intent"`
			Vector []float64 `json:"vector"`
		} `json:"examples"`
		Centroids []struct {
			Intent string    `json:"intent"`
			Vector []float64 `json:"vector"`
		} `json:"centroids"`
	}
	if json.Unmarshal(data, &store) != nil {
		return nil, nil
	}
	var vecs [][]float64
	var intents []string
	for _, e := range store.Examples {
		if len(e.Vector) > 0 {
			vecs = append(vecs, e.Vector)
			intents = append(intents, e.Intent)
		}
	}
	for _, c := range store.Centroids {
		if len(c.Vector) > 0 {
			vecs = append(vecs, c.Vector)
			intents = append(intents, c.Intent)
		}
	}
	if len(vecs) < 4 {
		return nil, nil
	}
	return vecs, intents
}
