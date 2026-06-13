package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/memory"
)

// MemoryService handles memory operations.
type MemoryService struct {
	manager *memory.Manager
}

// MemoryQueryRequest contains query parameters.
type MemoryQueryRequest struct {
	Query    string `json:"query"`
	Limit    int    `json:"limit,omitempty"`
	Category string `json:"category,omitempty"`
}

// MemoryResult wraps the memory type for service responses.
type MemoryResult struct {
	Memory         memory.Memory `json:"memory"`
	RelevanceScore float64       `json:"relevance_score"`
	Source         string        `json:"source"`
}

// NewMemoryService creates a memory service.
func NewMemoryService(mgr *memory.Manager) *MemoryService {
	return &MemoryService{manager: mgr}
}

// Query searches memories.
func (s *MemoryService) Query(ctx context.Context, req MemoryQueryRequest) ([]MemoryResult, error) {
	if req.Query == "" {
		return nil, wrapError("memory", "Query", ErrInvalidInput)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	memQuery := memory.MemoryQuery{
		Query:    req.Query,
		Category: req.Category,
		Limit:    limit,
	}

	results, err := s.manager.Search(ctx, memQuery)
	if err != nil {
		return nil, wrapError("memory", "Query", err)
	}

	// Convert to service result type
	serviceResults := make([]MemoryResult, len(results))
	for i, r := range results {
		serviceResults[i] = MemoryResult{
			Memory:         r.Memory,
			RelevanceScore: r.RelevanceScore,
			Source:         r.Source,
		}
	}
	return serviceResults, nil
}

// Recent gets recent memories.
func (s *MemoryService) Recent(ctx context.Context, limit int) ([]MemoryResult, error) {
	if limit <= 0 {
		limit = 10
	}

	results, err := s.manager.GetRecent(ctx, limit)
	if err != nil {
		return nil, wrapError("memory", "Recent", err)
	}

	serviceResults := make([]MemoryResult, len(results))
	for i, r := range results {
		serviceResults[i] = MemoryResult{
			Memory:         r.Memory,
			RelevanceScore: r.RelevanceScore,
			Source:         r.Source,
		}
	}
	return serviceResults, nil
}

// Export exports memories in JSON format.
func (s *MemoryService) Export(ctx context.Context, format, category string) ([]byte, error) {
	if format != "json" {
		return nil, wrapError("memory", "Export", fmt.Errorf("unsupported format: %s", format))
	}
	results, err := s.manager.GetRecent(ctx, 1000)
	if err != nil {
		return nil, wrapError("memory", "Export", err)
	}
	return json.MarshalIndent(results, "", "  ")
}

// VectorSearchRequest contains parameters for vector search.
type VectorSearchRequest struct {
	Query      string   `json:"query"`
	Limit      int      `json:"limit,omitempty"`
	ShardTypes []string `json:"shard_types,omitempty"`
}

// VectorSearchResult contains a vector search result.
type VectorSearchResult struct {
	MemoryID         string         `json:"memory_id"`
	Content          string         `json:"content"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	RelevanceScore   float64        `json:"relevance_score"`
	VectorSimilarity float64        `json:"vector_similarity"`
}

// VectorStoreRequest contains parameters for storing a vector.
type VectorStoreRequest struct {
	MemoryID string         `json:"memory_id"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// VectorStats contains vector shard statistics.
type VectorStats struct {
	LoadedShards int                    `json:"loaded_shards"`
	MaxRAMShards int                    `json:"max_ram_shards"`
	LRUHits      int64                  `json:"lru_hits"`
	LRUMisses    int64                  `json:"lru_misses"`
	LRUEvictions int64                  `json:"lru_evictions"`
	ShardDetails map[string]ShardDetail `json:"shard_details"`
}

// ShardDetail contains per-shard statistics.
type ShardDetail struct {
	Dimension      int   `json:"dimension"`
	M              int   `json:"m"`
	EFConstruction int   `json:"ef_construction"`
	EFSearch       int   `json:"ef_search"`
	VectorCount    int64 `json:"vector_count"`
	DatabaseSize   int64 `json:"database_size_bytes"`
	ShardID        string `json:"shard_id"`
}

// VectorSearch performs vector similarity search.
func (s *MemoryService) VectorSearch(ctx context.Context, req VectorSearchRequest) ([]VectorSearchResult, error) {
	if req.Query == "" {
		return nil, wrapError("memory", "VectorSearch", ErrInvalidInput)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	// Use the manager's semantic search which uses the vector store if configured
	results, err := s.manager.SearchSemantic(ctx, req.Query, limit)
	if err != nil {
		return nil, wrapError("memory", "VectorSearch", err)
	}

	// Convert to vector search result type
	serviceResults := make([]VectorSearchResult, len(results))
	for i, r := range results {
		serviceResults[i] = VectorSearchResult{
			MemoryID:         r.Memory.ID,
			Content:          r.Memory.Content,
			Metadata:         r.Memory.Metadata,
			RelevanceScore:   r.RelevanceScore,
			VectorSimilarity: r.RelevanceScore, // For semantic search, these are the same
		}
	}
	return serviceResults, nil
}

// VectorStore stores a memory with vector embedding.
func (s *MemoryService) VectorStore(ctx context.Context, req VectorStoreRequest) error {
	if req.MemoryID == "" || req.Content == "" {
		return wrapError("memory", "VectorStore", ErrInvalidInput)
	}

	// Store using episodic memory (which will use vector store if configured)
	_, err := s.manager.Store(ctx, memory.Memory{
		ID:       req.MemoryID,
		Content:  req.Content,
		Metadata: req.Metadata,
		Category: "episodic",
	})
	if err != nil {
		return wrapError("memory", "VectorStore", err)
	}
	return nil
}

// VectorDelete removes a memory by ID.
func (s *MemoryService) VectorDelete(ctx context.Context, memoryID string) error {
	if memoryID == "" {
		return wrapError("memory", "VectorDelete", ErrInvalidInput)
	}

	err := s.manager.Delete(ctx, memoryID)
	if err != nil {
		return wrapError("memory", "VectorDelete", err)
	}
	return nil
}

// VectorStats returns vector shard statistics.
func (s *MemoryService) VectorStats() (VectorStats, error) {
	if s.manager == nil {
		return VectorStats{}, wrapError("memory", "VectorStats", ErrUnavailable)
	}

	// Get the vector searcher from manager if available
	searcher := s.manager.GetVectorSearcher()
	if searcher == nil {
		return VectorStats{}, wrapError("memory", "VectorStats", fmt.Errorf("vector search not configured"))
	}

	// Type assert to get the underlying shard manager
	if adapter, ok := searcher.(*memory.ShardManagerVectorSearcher); ok {
		shardMgr := adapter.Manager()
		stats := shardMgr.Stats()

		// Convert shard details
		shardDetails := make(map[string]ShardDetail)
		for shardType, detail := range stats.ShardDetails {
			shardDetails[string(shardType)] = ShardDetail{
				Dimension:      detail.Dimension,
				M:              detail.M,
				EFConstruction: detail.EFConstruction,
				EFSearch:       detail.EFSearch,
				VectorCount:    detail.VectorCount,
				DatabaseSize:   detail.DatabaseSize,
				ShardID:        detail.ShardID,
			}
		}

		return VectorStats{
			LoadedShards:  stats.LoadedShards,
			MaxRAMShards:  stats.MaxRAMShards,
			LRUHits:       stats.LRUHits,
			LRUMisses:     stats.LRUMisses,
			LRUEvictions:  stats.LRUEvictions,
			ShardDetails:  shardDetails,
		}, nil
	}

	return VectorStats{}, wrapError("memory", "VectorStats", fmt.Errorf("vector searcher type not supported"))
}
