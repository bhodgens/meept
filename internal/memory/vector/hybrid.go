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

// HybridSearcher combines keyword (FTS) and vector similarity search.
type HybridSearcher struct {
	vectorStore     *Store
	keywordSearcher KeywordSearcher
	alpha           float64 // Weight for vector similarity (0-1)
}

// HybridSearcherConfig holds configuration for the hybrid searcher.
type HybridSearcherConfig struct {
	VectorStore     *Store
	KeywordSearcher KeywordSearcher
	Alpha           float64 // Weight for vector similarity: 0 = pure keyword, 1 = pure vector
}

// NewHybridSearcher creates a new hybrid searcher.
func NewHybridSearcher(cfg HybridSearcherConfig) (*HybridSearcher, error) {
	if cfg.VectorStore == nil {
		return nil, fmt.Errorf("vector store is required")
	}
	if cfg.KeywordSearcher == nil {
		return nil, fmt.Errorf("keyword searcher is required")
	}
	if cfg.Alpha < 0 || cfg.Alpha > 1 {
		cfg.Alpha = 0.5 // Default to equal weight
	}

	return &HybridSearcher{
		vectorStore:     cfg.VectorStore,
		keywordSearcher: cfg.KeywordSearcher,
		alpha:           cfg.Alpha,
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
func (h *HybridSearcher) Search(ctx context.Context, query string, limit int) ([]HybridResult, error) {
	// Get keyword search results
	keywordResults, err := h.keywordSearcher.SearchKeyword(ctx, query, limit*2)
	if err != nil {
		keywordResults = []KeywordResult{}
	}

	// Get vector search results
	vectorResults, err := h.vectorStore.Search(ctx, query, limit*2)
	if err != nil {
		vectorResults = []SearchResult{}
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
	return h.vectorStore.Search(ctx, query, limit)
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
