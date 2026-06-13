package services

import (
	"context"
	"time"

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

// Clear removes cached entries. If req.Prefix is non-empty, only entries whose
// model ID starts with that prefix are removed. Otherwise all entries are cleared.
func (s *CacheService) Clear(ctx context.Context, req ClearCacheRequest) error {
	if s.cache == nil {
		return nil
	}

	if req.Prefix != "" {
		s.cache.ClearByModelPrefix(req.Prefix)
	} else {
		s.cache.Clear()
	}
	return nil
}

// InvalidateRequest contains invalidation parameters.
type InvalidateRequest struct {
	FilePath string `json:"path,omitempty"`
}

// Invalidate removes cache entries for a given file path.
func (s *CacheService) Invalidate(ctx context.Context, req InvalidateRequest) error {
	if s.cache == nil {
		return nil
	}
	if req.FilePath != "" {
		s.cache.InvalidateByFile(ctx, req.FilePath)
	}
	return nil
}

// CacheInspectResult mirrors llm.InspectResult for HTTP serialization.
type CacheInspectResult struct {
	PromptHash string            `json:"prompt_hash"`
	ModelID    string            `json:"model_id"`
	CreatedAt  time.Time         `json:"created_at"`
	ExpiresAt  time.Time         `json:"expires_at"`
	HitCount   int               `json:"hit_count"`
	FileHashes map[string]string `json:"file_hashes,omitempty"`
	Source     string            `json:"source"`
}

// Inspect searches cache entries matching the given prompt hash.
func (s *CacheService) Inspect(ctx context.Context, hash string) ([]CacheInspectResult, error) {
	if s.cache == nil {
		return nil, nil
	}

	results := s.cache.Inspect(hash)
	out := make([]CacheInspectResult, len(results))
	for i, r := range results {
		out[i] = CacheInspectResult{
			PromptHash: r.PromptHash,
			ModelID:    r.ModelID,
			CreatedAt:  r.CreatedAt,
			ExpiresAt:  r.ExpiresAt,
			HitCount:   r.HitCount,
			FileHashes: r.FileHashes,
			Source:     r.Source,
		}
	}
	return out, nil
}
