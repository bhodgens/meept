package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/metrics"
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
	Response       *Response
	CreatedAt      time.Time
	LastAccessedAt time.Time `json:"last_accessed_at"`
	ExpiresAt      time.Time
	HitCount       int
	FileHashes     map[string]string
}

// IsExpired checks if the cache entry has expired.
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// CacheStats holds statistics about cache performance.
type CacheStats struct {
	Hits       int
	Misses     int
	Evictions  int
	EntryCount int
	HitRate    float64
	L1Hits     int
	L1Misses   int
	L2Hits     int
	L2Misses   int
	L1Entries  int
	L2Entries  int
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
	l1Cache      *L1Cache
	l2Cache      *L2Cache
	config       CacheConfig
	mu           sync.RWMutex
	stats        CacheStats
	metricsStore *metrics.Store
}

// NewTokenCacheCoordinator creates a new token cache coordinator.
func NewTokenCacheCoordinator(config CacheConfig) (*TokenCacheCoordinator, error) {
	return NewTokenCacheCoordinatorWithMetrics(config, nil)
}

// NewTokenCacheCoordinatorWithMetrics creates a new token cache coordinator with optional metrics recording.
func NewTokenCacheCoordinatorWithMetrics(config CacheConfig, metricsStore *metrics.Store) (*TokenCacheCoordinator, error) {
	// Create L1 cache
	l1Config := L1CacheConfig{
		MaxEntries:  config.L1MaxEntries,
		DefaultTTL:  config.DefaultTTL,
		CleanupFreq: config.CleanupFreq,
	}
	l1 := NewL1Cache(l1Config)

	// Wire metrics store to L1 for eviction metrics
	if metricsStore != nil {
		l1.SetMetricsStore(metricsStore)
	}

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
		// Wire metrics store to L2 for eviction metrics
		if metricsStore != nil {
			l2.SetMetricsStore(metricsStore)
		}
	}

	coordinator := &TokenCacheCoordinator{
		l1Cache:      l1,
		l2Cache:      l2,
		config:       config,
		metricsStore: metricsStore,
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
	if !c.config.Enabled {
		c.mu.Lock()
		c.stats.Misses++
		c.mu.Unlock()
		c.recordMetric("cache.miss", 1, key)
		return nil, false
	}

	// S3-1 FIX: Use write lock for L1 check because the hit path mutates stats.
	c.mu.Lock()
	if entry, found := c.l1Cache.Get(key); found { //nolint:mutexio // in-memory LRU Get, not I/O
		c.stats.Hits++
		c.stats.L1Hits++
		c.mu.Unlock()
		c.recordMetric("cache.hit", 1, key)
		return entry, true
	}
	l2 := c.l2Cache
	c.mu.Unlock()

	// L2 lookup outside the lock (may hit SQLite I/O).
	if l2 != nil {
		if entry, found := l2.Get(ctx, key); found {
			// Promote to L1 under write lock; re-check L1 to avoid duplicate work.
			c.mu.Lock()
			if _, already := c.l1Cache.Get(key); !already { //nolint:mutexio // in-memory LRU Get, not I/O
				c.l1Cache.Put(key, entry)
			}
			c.stats.Hits++
			c.stats.L2Hits++
			c.mu.Unlock()
			c.recordMetric("cache.hit", 1, key)
			return entry, true
		}
		c.mu.Lock()
		c.stats.L2Misses++
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.stats.L1Misses++
	c.stats.Misses++
	c.mu.Unlock()
	c.recordMetric("cache.miss", 1, key)
	return nil, false
}

// Put stores a response in both L1 and L2 caches.
func (c *TokenCacheCoordinator) Put(ctx context.Context, key CacheKey, response *Response) {
	if !c.config.Enabled {
		return
	}

	// Create cache entry
	now := time.Now()
	entry := &CacheEntry{
		Response:       response,
		CreatedAt:      now,
		LastAccessedAt: now,
		ExpiresAt:      now.Add(c.config.DefaultTTL),
		HitCount:       0,
		FileHashes:     key.FileHashes,
	}

	// Snapshot L2 handle under lock, then perform I/O without holding it.
	c.mu.Lock()
	l2 := c.l2Cache
	c.l1Cache.Put(key, entry)
	c.mu.Unlock()

	if l2 != nil {
		l2.Put(ctx, key, entry)
	}
}

// Invalidate removes a specific entry from both caches.
func (c *TokenCacheCoordinator) Invalidate(ctx context.Context, key CacheKey) {
	c.mu.Lock()
	l2 := c.l2Cache
	c.l1Cache.Invalidate(key)
	c.mu.Unlock()

	if l2 != nil {
		l2.Invalidate(ctx, key)
	}
}

// InvalidateByFile removes all entries referencing the given file path.
func (c *TokenCacheCoordinator) InvalidateByFile(ctx context.Context, filePath string) {
	c.mu.Lock()
	l2 := c.l2Cache
	// L1 invalidation by file path is handled by checking FileHashes
	c.l1Cache.InvalidateByFile(filePath)
	c.mu.Unlock()

	if l2 != nil {
		l2.InvalidateByFile(ctx, filePath)
	}
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

// ClearByModelPrefix removes all cache entries whose model ID starts with the
// given prefix. For example, prefix "gpt-4" removes entries for "gpt-4",
// "gpt-4-turbo", etc. Returns the number of entries removed.
func (c *TokenCacheCoordinator) ClearByModelPrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := c.l1Cache.ClearByKeyPrefix(prefix)
	if c.l2Cache != nil {
		removed += c.l2Cache.ClearByModelPrefix(prefix)
	}
	return removed
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
	stats.L1Entries = l1Count
	stats.L2Entries = l2Count

	return stats
}

// SetMetricsStore sets the metrics store for recording cache metrics.
// This allows the metrics store to be wired after coordinator creation
// when the store is not yet available at construction time.
func (c *TokenCacheCoordinator) SetMetricsStore(store *metrics.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metricsStore = store
	c.l1Cache.SetMetricsStore(store)
	if c.l2Cache != nil {
		c.l2Cache.SetMetricsStore(store)
	}
}

//nolint:unparam // value is intentionally parameterized for future metric variations
func (c *TokenCacheCoordinator) recordMetric(name string, value float64, key CacheKey) {
	// Snapshot metricsStore under lock to avoid racing with SetMetricsStore.
	// The actual Record call is then performed without holding the lock so that
	// concurrent cache operations are not blocked by metrics I/O.
	c.mu.RLock()
	store := c.metricsStore
	c.mu.RUnlock()
	if store == nil {
		return
	}
	tags := map[string]string{
		"model_id": key.ModelID,
	}
	if key.AgentID != "" {
		tags["agent_id"] = key.AgentID
	}
	store.Record(name, value, tags)
}

// InspectResult holds the details of a single inspected cache entry.
type InspectResult struct {
	PromptHash string
	ModelID    string
	Response   *Response
	CreatedAt  time.Time
	ExpiresAt  time.Time
	HitCount   int
	FileHashes map[string]string
	Source     string // "l1", "l2", or "l1+l2"
}

// Inspect searches both L1 and L2 caches for entries matching the given prompt hash.
// It returns all matching entries across models and file hash combinations.
func (c *TokenCacheCoordinator) Inspect(promptHash string) []InspectResult {
	// S3-6 FIX: snapshot the cache handles under RLock, then release the lock
	// before performing any SQLite I/O so concurrent writers aren't blocked.
	c.mu.RLock()
	l1 := c.l1Cache
	l2 := c.l2Cache
	c.mu.RUnlock()

	var results []InspectResult
	seen := make(map[string]bool) // modelID+fileHashKey -> already added

	// Search L2 first (authoritative, has all entries)
	if l2 != nil {
		for _, entry := range l2.Inspect(promptHash) {
			key := entry.ModelID + ":" + fileHashKey(entry.FileHashes)
			if !seen[key] {
				seen[key] = true
				results = append(results, InspectResult{
					PromptHash: promptHash,
					ModelID:    entry.ModelID,
					Response:   entry.Response,
					CreatedAt:  entry.CreatedAt,
					ExpiresAt:  entry.ExpiresAt,
					HitCount:   entry.HitCount,
					FileHashes: entry.FileHashes,
					Source:     "l2",
				})
			}
		}
	}

	// Search L1 for entries not already found in L2
	for _, entry := range l1.Inspect(promptHash) {
		key := entry.ModelID + ":" + fileHashKey(entry.FileHashes)
		if !seen[key] {
			seen[key] = true
			results = append(results, InspectResult{
				PromptHash: promptHash,
				ModelID:    entry.ModelID,
				Response:   entry.Response,
				CreatedAt:  entry.CreatedAt,
				ExpiresAt:  entry.ExpiresAt,
				HitCount:   entry.HitCount,
				FileHashes: entry.FileHashes,
				Source:     "l1",
			})
		} else {
			// Entry exists in both; update source label
			for i := range results {
				if results[i].ModelID == entry.ModelID &&
					fileHashKey(results[i].FileHashes) == fileHashKey(entry.FileHashes) {
					results[i].Source = "l1+l2"
					break
				}
			}
		}
	}

	return results
}

// fileHashKey produces a deterministic key from a file hash map for deduplication.
func fileHashKey(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte(';')
	}
	return b.String()
}

// Close closes the cache and releases resources.
func (c *TokenCacheCoordinator) Close() error {
	c.l1Cache.Stop()
	if c.l2Cache != nil {
		return c.l2Cache.Close()
	}
	return nil
}
