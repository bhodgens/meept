package services

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
)

// CacheService handles token cache operations.
type CacheService struct {
	cache *llm.TokenCacheCoordinator
}

// NewCacheService creates a cache service.
func NewCacheService(c *llm.TokenCacheCoordinator) *CacheService {
	return &CacheService{cache: c}
}

// CacheStatsResponse contains cache statistics.
type CacheStatsResponse struct {
	Hits   int64 `json:"hits"`
	Misses int64 `json:"misses"`
	Size   int   `json:"size"`
}

// Stats returns cache statistics.
func (s *CacheService) Stats(ctx context.Context) (*CacheStatsResponse, error) {
	if s.cache == nil {
		return &CacheStatsResponse{}, nil
	}

	// TODO: Get actual stats from cache coordinator
	return &CacheStatsResponse{}, nil
}

// ClearRequest contains clear parameters.
type ClearCacheRequest struct {
	Prefix string `json:"prefix,omitempty"`
}

// Clear removes cached entries.
func (s *CacheService) Clear(ctx context.Context, req ClearCacheRequest) error {
	// TODO: Implement cache clear
	return nil
}
