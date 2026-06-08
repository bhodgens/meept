package memory

import (
	"context"

	"github.com/caimlas/meept/internal/memory/vector"
)

// ShardManagerVectorSearcher wraps vector.ShardManager to implement the VectorSearcher interface.
// This adapter handles type conversion between vector.SearchResult and memory.VectorSearchResult.
type ShardManagerVectorSearcher struct {
	manager    *vector.ShardManager
	shardTypes []vector.ShardType
}

// NewShardManagerVectorSearcher creates a new vector searcher backed by ShardManager.
// If shardTypes is nil or empty, defaults to consolidated and recent shards.
func NewShardManagerVectorSearcher(manager *vector.ShardManager, shardTypes []vector.ShardType) *ShardManagerVectorSearcher {
	return &ShardManagerVectorSearcher{
		manager:    manager,
		shardTypes: shardTypes,
	}
}

// Search implements the VectorSearcher interface.
func (s *ShardManagerVectorSearcher) Search(ctx context.Context, query string, limit int) ([]VectorSearchResult, error) {
	shardTypes := s.shardTypes
	if len(shardTypes) == 0 {
		shardTypes = []vector.ShardType{vector.ConsolidatedShard, vector.RecentShard}
	}

	results, err := s.manager.Search(ctx, query, limit, shardTypes)
	if err != nil {
		return nil, err
	}

	// Convert vector.SearchResult to memory.VectorSearchResult
	converted := make([]VectorSearchResult, len(results))
	for i, r := range results {
		converted[i] = VectorSearchResult{
			MemoryID:         r.MemoryID,
			Content:          r.Content,
			Metadata:         r.Metadata,
			RelevanceScore:   r.RelevanceScore,
			VectorSimilarity: r.VectorSimilarity,
		}
	}
	return converted, nil
}

// Manager returns the underlying ShardManager for advanced operations.
func (s *ShardManagerVectorSearcher) Manager() *vector.ShardManager {
	return s.manager
}
