// Package vector provides semantic memory search using vector embeddings.
package vector

import (
	"context"
	"fmt"
	"maps"
)

// KeywordSearcher is an interface for keyword-based memory search.
// This avoids an import cycle with the memory package.
type KeywordSearcher interface {
	SearchKeyword(ctx context.Context, query string, limit int) ([]KeywordResult, error)
}

// KeywordResult represents a result from keyword search.
type KeywordResult struct {
	ID             string
	Content        string
	Metadata       map[string]any
	RelevanceScore float64
}

// VectorSearcher is the vector-search seam the HybridSearcher needs. The
// legacy *Store that satisfied it structurally was removed as dead code;
// the wired *ShardManagerVectorSearcher (internal/memory/vector_searcher.go)
// satisfies this shape and is the intended production implementation when
// hybrid search gets wired (docs/plans/2026-06-07-sharded-vector-memory.md).
type VectorSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// HybridSearcher combines keyword (FTS) and vector similarity search.
type HybridSearcher struct {
	vectorSearcher  VectorSearcher
	keywordSearcher KeywordSearcher
	alpha           float64 // Weight for vector similarity (0-1)
	// scope is the C2 predicate over raw row metadata (nil = unfiltered
	// legacy behavior). Applied to BOTH lanes' results before merge, so
	// a scoped caller can never see a row its purview excludes.
	scope ScopeFilter
}

// HybridSearcherConfig holds configuration for the hybrid searcher.
type HybridSearcherConfig struct {
	VectorSearcher  VectorSearcher
	KeywordSearcher KeywordSearcher
	Alpha           float64 // Weight for vector similarity: 0 = pure keyword, 1 = pure vector
	// Scope, when non-nil, filters BOTH lanes' results through the
	// committed C2 predicate (bughunt D11/C35: the searcher used to
	// return every row it saw — cross-user/project rows leaked to any
	// caller). The predicate type lives in internal/memory (the C2
	// implementation is single-sourced there); internal/memory/vector
	// cannot import internal/memory (the memory package imports THIS
	// package), so the seam is a function over raw row metadata.
	Scope ScopeFilter
}

// ScopeFilter is the C2 predicate seam: reports whether one raw row
// (identified by its metadata) is recallable for the bound caller.
// Wired from memory.VisibleScopeRow at construction; nil = unfiltered
// legacy behavior.
type ScopeFilter func(meta map[string]any) bool

// MaxSearchLimit caps the per-request limit (bughunt D11: the RPC path
// forwarded the caller's limit verbatim — limit=1e9 meant an unbounded
// FTS5 scan plus an embedding fan-out over every row). Matches the
// sibling surfaces' cap (ledger.LogQuery's max is 1000; the Manager
// search path bounds the same way).
const MaxSearchLimit = 1000

// clampLimit bounds a caller-supplied limit to MaxSearchLimit; <= 0
// resolves to the default. Exported as ClampLimit so the daemon's RPC
// registration site clamps with the SAME rule the searcher applies
// internally (one constant, one helper).
func ClampLimit(limit, def int) int {
	if limit <= 0 {
		return def
	}
	if limit > MaxSearchLimit {
		return MaxSearchLimit
	}
	return limit
}

// clampLimit is the internal alias used inside the package.
func clampLimit(limit, def int) int { return ClampLimit(limit, def) }

// NewHybridSearcher creates a new hybrid searcher.
func NewHybridSearcher(cfg HybridSearcherConfig) (*HybridSearcher, error) {
	if cfg.VectorSearcher == nil {
		return nil, fmt.Errorf("vector searcher is required")
	}
	if cfg.KeywordSearcher == nil {
		return nil, fmt.Errorf("keyword searcher is required")
	}
	if cfg.Alpha < 0 || cfg.Alpha > 1 {
		cfg.Alpha = 0.5 // Default to equal weight
	}

	return &HybridSearcher{
		vectorSearcher:  cfg.VectorSearcher,
		keywordSearcher: cfg.KeywordSearcher,
		alpha:           cfg.Alpha,
		scope:           cfg.Scope,
	}, nil
}

// HybridResult represents a combined search result.
type HybridResult struct {
	MemoryID      string
	Content       string
	Metadata      map[string]any
	KeywordScore  float64
	VectorScore   float64
	CombinedScore float64
}

// Search performs a hybrid search combining keyword and vector similarity.
// The limit is clamped to MaxSearchLimit (bughunt D11: an unbounded
// request limit meant an unbounded FTS5 scan + embedding fan-out per
// call). Results from BOTH lanes pass the configured scope predicate
// (bughunt C35) before the merge, so a scoped caller sees only rows its
// purview permits.
func (h *HybridSearcher) Search(ctx context.Context, query string, limit int) ([]HybridResult, error) {
	limit = clampLimit(limit, 10)
	// Fetch limit*2 per lane (the merge re-ranks; the pre-D11 shape) but
	// bounded by the clamp above.
	// Get keyword search results
	keywordResults, err := h.keywordSearcher.SearchKeyword(ctx, query, limit*2)
	if err != nil {
		keywordResults = []KeywordResult{}
	}

	// Get vector search results
	vectorResults, err := h.vectorSearcher.Search(ctx, query, limit*2)
	if err != nil {
		vectorResults = []SearchResult{}
	}

	// Scope filter (bughunt C35): drop rows the caller's purview excludes
	// BEFORE the merge, on both lanes.
	if h.scope != nil {
		filteredKeyword := keywordResults[:0]
		for _, r := range keywordResults {
			if h.scope(r.Metadata) {
				filteredKeyword = append(filteredKeyword, r)
			}
		}
		keywordResults = filteredKeyword
		filteredVector := vectorResults[:0]
		for _, r := range vectorResults {
			if h.scope(r.Metadata) {
				filteredVector = append(filteredVector, r)
			}
		}
		vectorResults = filteredVector
	}

	// Build score maps
	keywordScores := make(map[string]float64)
	for _, r := range keywordResults {
		keywordScores[r.ID] = r.RelevanceScore
	}

	vectorScores := make(map[string]float64)
	for _, r := range vectorResults {
		vectorScores[r.MemoryID] = r.VectorSimilarity
	}

	// Combine results
	combined := make(map[string]*HybridResult)

	// Add keyword results
	for _, r := range keywordResults {
		combined[r.ID] = &HybridResult{
			MemoryID:     r.ID,
			Content:      r.Content,
			Metadata:     r.Metadata,
			KeywordScore: r.RelevanceScore,
			VectorScore:  0,
		}
	}

	// Add vector results and merge
	for _, r := range vectorResults {
		if existing, ok := combined[r.MemoryID]; ok {
			existing.VectorScore = r.VectorSimilarity
			if r.Content != "" {
				existing.Content = r.Content
			}
			if len(r.Metadata) > 0 {
				if existing.Metadata == nil {
					existing.Metadata = make(map[string]any)
				}
				maps.Copy(existing.Metadata, r.Metadata)
			}
		} else {
			combined[r.MemoryID] = &HybridResult{
				MemoryID:     r.MemoryID,
				Content:      r.Content,
				Metadata:     r.Metadata,
				KeywordScore: 0,
				VectorScore:  r.VectorSimilarity,
			}
		}
	}

	// Calculate combined scores
	for _, r := range combined {
		kwScore := r.KeywordScore
		vecScore := r.VectorScore
		r.CombinedScore = (1-h.alpha)*kwScore + h.alpha*vecScore
	}

	// Convert to slice and sort by combined score descending
	results := make([]HybridResult, 0, len(combined))
	for _, r := range combined {
		results = append(results, *r)
	}
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].CombinedScore > results[i].CombinedScore {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Apply limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// SemanticOnly performs vector-only semantic search.
func (h *HybridSearcher) SemanticOnly(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return h.vectorSearcher.Search(ctx, query, limit)
}

// KeywordOnly performs keyword-only search (FTS).
func (h *HybridSearcher) KeywordOnly(ctx context.Context, query string, limit int) ([]KeywordResult, error) {
	return h.keywordSearcher.SearchKeyword(ctx, query, limit)
}

// SetAlpha changes the weighting factor for hybrid search.
func (h *HybridSearcher) SetAlpha(alpha float64) {
	if alpha < 0 {
		alpha = 0
	} else if alpha > 1 {
		alpha = 1
	}
	h.alpha = alpha
}

// GetAlpha returns the current weighting factor.
func (h *HybridSearcher) GetAlpha() float64 {
	return h.alpha
}
