package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/llm"
)

// CacheHandler handles cache RPC methods.
type CacheHandler struct {
	cache  *llm.TokenCacheCoordinator
	logger *slog.Logger
}

// NewCacheHandler creates a new cache handler.
func NewCacheHandler(cache *llm.TokenCacheCoordinator, logger *slog.Logger) *CacheHandler {
	return &CacheHandler{
		cache:  cache,
		logger: logger.With("component", "cache_handler"),
	}
}

// RegisterCacheMethods registers all cache RPC methods.
func (h *CacheHandler) RegisterCacheMethods(server *Server) {
	server.RegisterHandler("cache.stats", h.handleStats)
	server.RegisterHandler("cache.clear", h.handleClear)
	server.RegisterHandler("cache.invalidate", h.handleInvalidate)
}

// handleStats returns cache statistics.
func (h *CacheHandler) handleStats(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cache == nil {
		return nil, fmt.Errorf("cache not enabled")
	}

	stats := h.cache.Stats()

	return map[string]any{
		"l1_entries":  stats.EntryCount, // L1 count (L2 count would require separate query)
		"l1_hits":     stats.L1Hits,
		"l1_misses":   stats.L1Misses,
		"evictions":   stats.Evictions,
		"l2_entries":  0, // Would need L2 cache access for accurate count
		"l2_hits":     stats.L2Hits,
		"l2_misses":   stats.L2Misses,
		"total_hits":  stats.L1Hits + stats.L2Hits,
		"hit_rate":    stats.HitRate,
	}, nil
}

// handleClear clears all cache entries.
func (h *CacheHandler) handleClear(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cache == nil {
		return nil, fmt.Errorf("cache not enabled")
	}

	h.cache.Clear()
	h.logger.Info("cache cleared")

	return map[string]any{
		"status": "cleared",
	}, nil
}

// handleInvalidate invalidates cache entries for a specific file.
func (h *CacheHandler) handleInvalidate(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cache == nil {
		return nil, fmt.Errorf("cache not enabled")
	}

	var req struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if req.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	h.cache.InvalidateByFile(ctx, req.FilePath)
	h.logger.Info("cache entries invalidated", "file", req.FilePath)

	return map[string]any{
		"status":   "invalidated",
		"file":     req.FilePath,
	}, nil
}
