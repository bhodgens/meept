package conflict

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// RecallResult is one recalled memory that may conflict with a candidate.
// Callers adapt internal/memory's VectorSearchResult to this shape.
type RecallResult struct {
	MemoryID string
	Content  string
}

// RecallStore is the narrow seam the detector needs from the memory store.
// Defined here (not in internal/memory) so the conflict package stays
// dependency-free; callers adapt. It mirrors the shape of
// internal/memory.VectorSearcher plus an embeddings probe.
type RecallStore interface {
	// Search returns up to limit stored memories similar to query, best
	// first (when embeddings are available the ranking is the store's).
	Search(ctx context.Context, query string, limit int) ([]RecallResult, error)
	// EmbeddingsAvailable reports whether Search ranks by embeddings. When
	// false, topSimilar re-ranks results by token-overlap similarity.
	EmbeddingsAvailable() bool
}

// RecallTarget is a recalled memory chosen for the prompt, with the
// similarity score that selected it.
type RecallTarget struct {
	RecallResult
	Score float64
}

// topSimilar returns the k most relevant stored memories for candidate c.
//
// Vector path: when the store has embeddings, Search's own ranking is
// trusted and results pass through with descending positional scores.
//
// Fallback (no embeddings): the store returns a coarse recall pool and
// topSimilar re-ranks it by token-overlap Jaccard similarity between the
// candidate and each result — mirroring internal/memory/graph.go's
// `similar`-edge scoring (lowercased whitespace tokens, punctuation
// trimmed) and distill.go's tokenJaccard dedupe fallback.
//
// A recall error fails open: (nil, nil) — the caller treats it as "nothing
// recalled", so detection short-circuits to store.
func topSimilar(ctx context.Context, store RecallStore, c Candidate, k int) ([]RecallTarget, error) {
	if store == nil || k <= 0 {
		return nil, nil
	}

	results, err := store.Search(ctx, c.Content, k)
	if err != nil {
		// Fail open: recall is an optimization, not a gate. The nil error
		// is deliberate — DetectBatch treats "nothing recalled" as
		// skip-detection, and a recall outage must not fail the batch.
		//nolint:nilerr // fail-open by contract (SHARED-CONVENTIONS §2.3)
		return nil, nil
	}
	if len(results) == 0 {
		return nil, nil
	}

	targets := make([]RecallTarget, 0, len(results))
	if store.EmbeddingsAvailable() {
		// Trust the store's vector ranking; assign descending positional
		// scores so the field stays meaningful downstream.
		for i, r := range results {
			targets = append(targets, RecallTarget{
				RecallResult: r,
				Score:        1.0 - float64(i)/float64(len(results)+1),
			})
		}
		return truncateTargets(targets, k), nil
	}

	// Token-overlap fallback: score every recalled result against the
	// candidate's canonical text and keep the top k.
	candTokens := tokenize(c.Content)
	for _, r := range results {
		targets = append(targets, RecallTarget{
			RecallResult: r,
			Score:        jaccard(candTokens, tokenize(r.Content)),
		})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Score != targets[j].Score {
			return targets[i].Score > targets[j].Score
		}
		return targets[i].MemoryID < targets[j].MemoryID
	})
	return truncateTargets(targets, k), nil
}

func truncateTargets(targets []RecallTarget, k int) []RecallTarget {
	if len(targets) > k {
		targets = targets[:k]
	}
	return targets
}

// tokenize mirrors internal/memory's dedupe tokenizer: lowercase whitespace
// fields with surrounding punctuation trimmed.
func tokenize(s string) map[string]bool {
	out := make(map[string]bool)
	for f := range strings.FieldsSeq(strings.ToLower(s)) {
		out[strings.Trim(f, ".,;:!?\"'()")] = true
	}
	delete(out, "")
	return out
}

// jaccard is the intersection-over-union of two token sets (0 when either
// side is empty), matching internal/memory's jaccardSimilarity.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// recallBatch recalls targets for every candidate in one batch. The map is
// keyed by candidate index within the batch; candidates with nothing
// recalled (or on recall error) are simply absent.
func recallBatch(ctx context.Context, store RecallStore, cands []Candidate, perCandidate int) (map[int][]RecallTarget, error) {
	if store == nil {
		//nolint:nilnil // nil store = recall disabled; absent keys mean "nothing recalled" (fail-open contract)
		return nil, nil
	}
	out := make(map[int][]RecallTarget, len(cands))
	for i, c := range cands {
		targets, err := topSimilar(ctx, store, c, perCandidate)
		if err != nil {
			return nil, fmt.Errorf("recall candidate %d: %w", i, err)
		}
		if len(targets) > 0 {
			out[i] = targets
		}
	}
	return out, nil
}
