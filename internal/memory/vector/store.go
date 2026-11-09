// Package vector provides semantic memory search using vector embeddings.
package vector

// SearchResult represents a vector similarity search result.
//
// Shared result shape of the wired search paths: VectorShard.Search,
// ShardManager.Search/ConsolidateResults/SortByRelevance, and
// SelectBalanced (bridges-eval arm B). The legacy Store that introduced
// it was removed as dead code; the type itself stays with the wired
// consumers.
type SearchResult struct {
	MemoryID         string
	Content          string
	Metadata         map[string]any
	RelevanceScore   float64
	VectorSimilarity float64
	RawRowID         int64
}
