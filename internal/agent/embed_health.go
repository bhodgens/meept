package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"sync"
)

// EmbedHealthCheck guards the Stage-0 embedding prefilter against a degraded
// or silently-changed embedding pipeline (meept issue #42).
//
// Mechanism (measured and verified, classi-fly docs/MEEPT-TESTS.md test 3 /
// docs/AXES-2026-09-12.md axis D): cosine distance from the incoming
// embedding to the NEAREST vector in a fixed reference set of known-good
// embeddings. Per-item distance to a fixed reference set detected every
// distribution shift with 2.5-step median latency and 0.83% false alarms.
//
// Threshold: the 95th percentile of the reference's own cross-fitted
// nearest-neighbour cosine distances (each vector scored against the OTHER
// vectors, never itself), so ~95% of known-good embeddings pass by
// construction. Reference implementation: classi-fly
// tools/eval/embed_health.py calibrate().
//
// Fail-safe policy (OOD = safety): a missing/corrupt/empty reference set,
// a dimension mismatch, NaN/Inf input, or a zero-norm input all resolve to
// unhealthy=false — the caller falls through to the full LLM chain. The
// check can only skip prefilter work, never enable a confident route on
// garbage. Mirrors the prefilter invariant (embedding_prefilter.go): the
// gate can only skip work, never degrade routing.
type EmbedHealthCheck struct {
	// refNorm is the reference matrix, row-normalized, read-only after
	// NewEmbedHealthCheck (built once, never mutated) so Check needs no lock.
	refNorm [][]float64
	// threshold is the calibrated 95th-percentile NN distance. Valid only
	// when loaded is true.
	threshold float64
	loaded    bool
	logger    *slog.Logger
}

// embedHealthEps guards division-by-zero when normalizing degenerate rows.
const embedHealthEps = 1e-12

// refSet is the on-disk reference format: a bare JSON 2-D array
// [[float,...],...] of known-good embedding vectors, all of equal
// dimension. Produced by tools/build_embed_health_ref.py from the
// prefilter centroid store (~/.meept/classifier_prefilter_centroids.json).
type refSet [][]float64

// NewEmbedHealthCheck loads the reference set from refPath (JSON
// [[float,...],...]), calibrates the 95th-percentile threshold, and returns
// the check. Errors (never silently disables) when the file is missing,
// unparseable, or degenerate (< 2 vectors, ragged rows, NaN/Inf) — fail
// closed, per the owner decision that OOD policy is safety-first.
func NewEmbedHealthCheck(refPath string, logger *slog.Logger) (*EmbedHealthCheck, error) {
	if logger == nil {
		logger = slog.Default()
	}
	c := &EmbedHealthCheck{logger: logger.With("component", "embed_health")}

	data, err := os.ReadFile(refPath)
	if err != nil {
		return nil, fmt.Errorf("embed health reference unreadable: %w", err)
	}
	var raw refSet
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("embed health reference corrupt JSON: %w", err)
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("embed health reference needs >= 2 vectors, got %d", len(raw))
	}
	dim := len(raw[0])
	if dim == 0 {
		return nil, fmt.Errorf("embed health reference vectors have dimension 0")
	}
	for i, row := range raw {
		if len(row) != dim {
			return nil, fmt.Errorf("embed health reference ragged: row %d has %d dims, want %d", i, len(row), dim)
		}
		for _, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("embed health reference contains NaN/Inf (row %d)", i)
			}
		}
	}

	thr, err := c.calibrate(raw)
	if err != nil {
		return nil, fmt.Errorf("embed health calibration failed: %w", err)
	}

	c.refNorm = make([][]float64, len(raw))
	for i, row := range raw {
		norm := 0.0
		for _, v := range row {
			norm += v * v
		}
		norm = math.Sqrt(norm)
		out := make([]float64, dim)
		if norm <= embedHealthEps {
			// A zero row cannot occur for finite input unless all-zero;
			// keep it as-is so it simply never wins the min-distance scan.
			copy(out, row)
		} else {
			inv := 1.0 / norm
			for j, v := range row {
				out[j] = v * inv
			}
		}
		c.refNorm[i] = out
	}
	c.threshold = thr
	c.loaded = true
	c.logger.Info("embed health check loaded",
		"path", refPath,
		"vectors", len(c.refNorm),
		"dimension", dim,
		"threshold", thr,
	)
	return c, nil
}

// calibrate computes the threshold: the pct percentile (0..1) of
// cross-fitted nearest-neighbour cosine distances within the reference set.
// Folded into 5 deterministic contiguous folds; each vector's score is its
// distance to the nearest vector in the OTHER folds, so no vector is ever
// scored against itself. Deterministic: fixed fold count, no shuffling.
func (c *EmbedHealthCheck) calibrate(ref refSet) (float64, error) {
	const folds = 5
	const pct = 0.95
	n := len(ref)
	if n < 2 {
		return 0, fmt.Errorf("calibration needs >= 2 vectors, got %d", n)
	}
	normed := make([][]float64, n)
	for i, row := range ref {
		norm := 0.0
		for _, v := range row {
			norm += v * v
		}
		norm = math.Sqrt(norm)
		out := make([]float64, len(row))
		if norm <= embedHealthEps {
			return 0, fmt.Errorf("calibration set contains a zero vector (row %d)", i)
		}
		inv := 1.0 / norm
		for j, v := range row {
			out[j] = v * inv
		}
		normed[i] = out
	}
	k := folds
	if k > n {
		k = n
	}
	dists := make([]float64, 0, n)
	for f := 0; f < k; f++ {
		for i := f; i < n; i += k { // deterministic contiguous folds: i % k == f
			best := math.Inf(1)
			for j := 0; j < n; j++ {
				if j%k == f {
					continue // same fold: never score against self/fold-mates
				}
				s := dot(normed[i], normed[j])
				if 1.0-s < best {
					best = 1.0 - s
				}
			}
			dists = append(dists, best)
		}
	}
	sort.Float64s(dists)
	// Nearest-rank percentile (deterministic, no interpolation ambiguity):
	// index ceil(pct*N)-1, clamped to the last element.
	idx := int(math.Ceil(pct*float64(len(dists)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(dists) {
		idx = len(dists) - 1
	}
	return dists[idx], nil
}

// dot returns the inner product of two equal-length vectors. No bounds
// check: callers guarantee equal length (same matrix).
func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// Check reports whether embedding looks like it came from the healthy
// embedding pipeline: cosine distance to the nearest reference vector
// compared against the calibrated threshold. Loaded=false (no reference —
// e.g. the shipped default when the file is absent) means the check is
// INERT and returns healthy=true so it never blocks routing; operators
// opt in by generating the reference file. Degenerate inputs (dimension
// mismatch, NaN/Inf, zero norm) return healthy=false (fail safe: fall
// through to the LLM chain rather than route on garbage).
func (c *EmbedHealthCheck) Check(embedding []float64) (healthy bool, distance float64) {
	if !c.loaded {
		return true, 0
	}
	// Dimension gate first: a wrong-length vector cannot be scored.
	if len(embedding) == 0 || len(embedding) != len(c.refNorm[0]) {
		return false, 0
	}
	norm := 0.0
	for _, v := range embedding {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false, 0
		}
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm <= embedHealthEps {
		return false, 0
	}
	inv := 1.0 / norm
	x := make([]float64, len(embedding))
	for i, v := range embedding {
		x[i] = v * inv
	}
	best := math.Inf(1)
	for _, r := range c.refNorm {
		if d := 1.0 - dot(x, r); d < best {
			best = d
		}
	}
	return best <= c.threshold, best
}

// Loaded reports whether a reference set is active. False = the check is
// inert (Check always returns healthy=true).
func (c *EmbedHealthCheck) Loaded() bool { return c.loaded }

// Threshold exposes the calibrated 95th-percentile distance (for tests and
// the startup log only).
func (c *EmbedHealthCheck) Threshold() float64 { return c.threshold }

// embedHealthRefOnce guards the one-time "reference missing" startup log so
// a per-construction retry loop cannot spam it.
var embedHealthRefOnce sync.Once

// embedHealthRefLogWarn logs the inert-check condition once per process.
func embedHealthRefLogWarn(logger *slog.Logger, path string, err error) {
	embedHealthRefOnce.Do(func() {
		logger.Warn("embed health reference unavailable; embed health check inert", "path", path, "error", err)
	})
}
