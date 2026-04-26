package llm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CacheKey uniquely identifies a cached LLM response.
type CacheKey struct {
	// PromptHash is the SHA256 hash of the full prompt
	PromptHash string
	// ModelID identifies the model used
	ModelID string
	// FileHashes maps file paths to their content hashes (for file-aware caching)
	FileHashes map[string]string
	// AgentID is optional, for analytics
	AgentID string
}

// String returns a human-readable representation of a CacheKey.
func (k CacheKey) String() string {
	promptHash := k.PromptHash
	if len(promptHash) > 16 {
		promptHash = promptHash[:16]
	}
	if len(k.FileHashes) == 0 {
		return fmt.Sprintf("CacheKey(%s:%s)", k.ModelID, promptHash)
	}
	return fmt.Sprintf("CacheKey(%s:%s:%d-files)", k.ModelID, promptHash, len(k.FileHashes))
}

// CacheEntry represents a single cached response.
type CacheEntry struct {
	Response   *Response
	CreatedAt  time.Time
	ExpiresAt  time.Time
	HitCount   int
	FileHashes map[string]string
}

// IsExpired checks if the cache entry has expired.
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// CacheStats holds statistics about cache performance.
type CacheStats struct {
	Hits          int
	Misses        int
	Evictions     int
	EntryCount    int
	HitRate       float64
	L1Hits        int
	L1Misses      int
	L2Hits        int
	L2Misses      int
}

// ResponseCache defines the interface for LLM response token caching.
// Named differently from tokenizer.TokenCache to avoid conflicts.
type ResponseCache interface {
	// Get retrieves a cached response for the given key
	Get(ctx context.Context, key CacheKey) (*CacheEntry, bool)
	// Put stores a response in the cache
	Put(ctx context.Context, key CacheKey, response *Response)
	// Invalidate removes a specific entry from the cache
	Invalidate(ctx context.Context, key CacheKey)
	// InvalidateByFile removes all entries referencing a file path
	InvalidateByFile(ctx context.Context, filePath string)
	// Clear removes all entries from the cache
	Clear()
	// Stats returns current cache statistics
	Stats() CacheStats
	// Close closes the cache and releases resources
	Close() error
}

// TokenCacheCoordinator orchestrates L1 (in-memory) and L2 (SQLite) caches.
type TokenCacheCoordinator struct {
	l1Cache *L1Cache
	l2Cache *L2Cache
	config  CacheConfig
	mu      sync.RWMutex
	stats   CacheStats
}

// NewTokenCacheCoordinator creates a new token cache coordinator.
func NewTokenCacheCoordinator(config CacheConfig) (*TokenCacheCoordinator, error) {
	// Create L1 cache
	l1Config := L1CacheConfig{
		MaxEntries:  config.L1MaxEntries,
		DefaultTTL:  config.DefaultTTL,
		CleanupFreq: config.CleanupFreq,
	}
	l1 := NewL1Cache(l1Config)

	// Create L2 cache if enabled
	var l2 *L2Cache
	if config.L2Enabled {
		var err error
		l2Config := L2CacheConfig{
			DBPath:      config.L2DBPath,
			DefaultTTL:  config.DefaultTTL,
			CleanupFreq: config.CleanupFreq,
		}
		l2, err = NewL2Cache(l2Config)
		if err != nil {
			return nil, err
		}
	}

	coordinator := &TokenCacheCoordinator{
		l1Cache: l1,
		l2Cache: l2,
		config:  config,
	}

	// Start background cleanup
	l1.Start()
	if l2 != nil {
		l2.Start()
	}

	return coordinator, nil
}

// Get retrieves a cached response, checking L1 first, then L2.
func (c *TokenCacheCoordinator) Get(ctx context.Context, key CacheKey) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.config.Enabled {
		c.stats.Misses++
		return nil, false
	}

	// Check L1 first
	if entry, found := c.l1Cache.Get(key); found {
		c.stats.Hits++
		c.stats.L1Hits++
		return entry, true
	}
	c.stats.L1Misses++

	// Check L2 if enabled
	if c.l2Cache != nil {
		if entry, found := c.l2Cache.Get(ctx, key); found {
			// Promote to L1
			c.l1Cache.Put(key, entry)
			c.stats.Hits++
			c.stats.L2Hits++
			return entry, true
		}
		c.stats.L2Misses++
	}

	c.stats.Misses++
	return nil, false
}

// Put stores a response in both L1 and L2 caches.
func (c *TokenCacheCoordinator) Put(ctx context.Context, key CacheKey, response *Response) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.config.Enabled {
		return
	}

	// Create cache entry
	entry := &CacheEntry{
		Response:   response,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(c.config.DefaultTTL),
		HitCount:   0,
		FileHashes: key.FileHashes,
	}

	// Store in L1
	c.l1Cache.Put(key, entry)

	// Store in L2 if enabled
	if c.l2Cache != nil {
		c.l2Cache.Put(ctx, key, entry)
	}
}

// Invalidate removes a specific entry from both caches.
func (c *TokenCacheCoordinator) Invalidate(ctx context.Context, key CacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.l1Cache.Invalidate(key)
	if c.l2Cache != nil {
		c.l2Cache.Invalidate(ctx, key)
	}
}

// InvalidateByFile removes all entries referencing the given file path.
func (c *TokenCacheCoordinator) InvalidateByFile(ctx context.Context, filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.l2Cache != nil {
		c.l2Cache.InvalidateByFile(ctx, filePath)
	}
	// L1 invalidation by file path is handled by checking FileHashes
	c.l1Cache.InvalidateByFile(filePath)
}

// Clear removes all entries from both caches.
func (c *TokenCacheCoordinator) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.l1Cache.Clear()
	if c.l2Cache != nil {
		c.l2Cache.Clear()
	}
	c.stats = CacheStats{}
}

// Stats returns current cache statistics.
func (c *TokenCacheCoordinator) Stats() CacheStats {
	c.mu.RLock()
	// Copy stats to avoid holding lock during calculations
	stats := c.stats
	l1Count := c.l1Cache.Count()
	l2Count := 0
	if c.l2Cache != nil {
		l2Count = c.l2Cache.Count()
	}
	c.mu.RUnlock()

	// Compute derived values on local copy (no lock needed)
	total := stats.L1Hits + stats.L1Misses
	if total > 0 {
		stats.HitRate = float64(stats.L1Hits+stats.L2Hits) / float64(total) * 100
	}
	stats.EntryCount = l1Count + l2Count

	return stats
}

// Close closes the cache and releases resources.
func (c *TokenCacheCoordinator) Close() error {
	c.l1Cache.Stop()
	if c.l2Cache != nil {
		return c.l2Cache.Close()
	}
	return nil
}
