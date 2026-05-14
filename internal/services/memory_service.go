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
