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

	stats := s.cache.Stats()
	return &CacheStatsResponse{
		Hits:   int64(stats.Hits),
		Misses: int64(stats.Misses),
		Size:   stats.EntryCount,
	}, nil
}

// ClearRequest contains clear parameters.
type ClearCacheRequest struct {
	Prefix string `json:"prefix,omitempty"`
}

// Clear removes cached entries.
func (s *CacheService) Clear(ctx context.Context, req ClearCacheRequest) error {
	if s.cache == nil {
		return nil
	}

	s.cache.Clear()
	return nil
}
