package memory

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/caimlas/meept/internal/memory/vector"
)

// ShardManagerVectorSearcher wraps vector.ShardManager to implement the VectorSearcher interface.
// This adapter handles type conversion between vector.SearchResult and memory.VectorSearchResult.
type ShardManagerVectorSearcher struct {
	manager    *vector.ShardManager
	shardTypes []vector.ShardType
	// bridgeCap is the balanced-selection k_b (memory.bridges.kb, default
	// vector.DefaultBridgeKB). Atomic so SetBridgeCap can be called at
	// runtime after daemon startup wires config.
	bridgeCap atomic.Int64
	// bridgeCapSet distinguishes an explicit cap from the zero value so kb=0
	// (and any non-positive value) still resolves to the package default.
	bridgeCapSet atomic.Bool
	logger       *slog.Logger
}

// SearcherOption customises a ShardManagerVectorSearcher.
type SearcherOption func(*ShardManagerVectorSearcher)

// WithBridgeCap sets the balanced-selection bridge cap (memory.bridges.kb).
// Non-positive values fall back to vector.DefaultBridgeKB at selection time.
func WithBridgeCap(kb int) SearcherOption {
	return func(s *ShardManagerVectorSearcher) {
		s.SetBridgeCap(kb)
	}
}

// NewShardManagerVectorSearcher creates a new vector searcher backed by ShardManager.
// If shardTypes is nil or empty, defaults to consolidated and recent shards.
func NewShardManagerVectorSearcher(manager *vector.ShardManager, shardTypes []vector.ShardType, opts ...SearcherOption) *ShardManagerVectorSearcher {
	s := &ShardManagerVectorSearcher{
		manager:    manager,
		shardTypes: shardTypes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// SetBridgeCap updates the balanced-selection bridge cap (memory.bridges.kb)
// at runtime. Nil-receiver safe: a nil searcher has nothing to configure.
// Non-positive values fall back to vector.DefaultBridgeKB at selection time.
func (s *ShardManagerVectorSearcher) SetBridgeCap(kb int) {
	if s == nil {
		return
	}
	s.bridgeCap.Store(int64(kb))
	s.bridgeCapSet.Store(true)
	if s.logger != nil {
		s.logger.Debug("bridge cap updated", "kb", kb)
	}
}

// bridgeCapValue resolves the effective k_b: the configured cap when one was
// set, else vector.DefaultBridgeKB.
func (s *ShardManagerVectorSearcher) bridgeCapValue() int {
	if s != nil && s.bridgeCapSet.Load() {
		return int(s.bridgeCap.Load())
	}
	return vector.DefaultBridgeKB
}

// selectBalanced applies post-retrieval balanced selection (B04) to search
// results using the searcher's configured bridge cap.
func (s *ShardManagerVectorSearcher) selectBalanced(results []vector.SearchResult) ([]vector.SearchResult, vector.SelectionStats) {
	filtered, stats := vector.SelectBalanced(results, s.bridgeCapValue())
	if s.logger != nil && (stats.BridgesIncluded > 0 || stats.BridgesExcluded > 0) {
		s.logger.Debug("balanced selection",
			"included", stats.BridgesIncluded,
			"excluded", stats.BridgesExcluded,
			"sources", stats.Sources,
			"total", stats.Total,
		)
	}
	return filtered, stats
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

	// B04 balanced selection: cap the bridge share AFTER retrieval/merge
	// (ConsolidateResults), BEFORE prompt assembly. Source units pass
	// through; order is preserved. An empty bridge store makes this a no-op.
	results, _ = s.selectBalanced(results)

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
