package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// L1CacheConfig holds configuration for the L1 in-memory cache.
type L1CacheConfig struct {
	MaxEntries  int
	DefaultTTL  time.Duration
	CleanupFreq time.Duration
}

// l1CacheEntry wraps CacheEntry with a composite key.
type l1CacheEntry struct {
	Key     string
	Entry   *CacheEntry
	FileMap map[string]string // For file-based invalidation
}

// L1Cache is an in-memory exact-match cache.
type L1Cache struct {
	entries map[string]*l1CacheEntry
	config  L1CacheConfig
	mu      sync.RWMutex
	stats   CacheStats
	logger  *slog.Logger
	stopCh  chan struct{}
}

// NewL1Cache creates a new L1 in-memory cache.
func NewL1Cache(config L1CacheConfig) *L1Cache {
	if config.MaxEntries <= 0 {
		config.MaxEntries = 10000
	}
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 30 * time.Minute
	}
	if config.CleanupFreq <= 0 {
		config.CleanupFreq = 2 * time.Minute
	}

	return &L1Cache{
		entries: make(map[string]*l1CacheEntry),
		config:  config,
		logger:  slog.Default().With("component", "token_cache_l1"),
		stopCh:  make(chan struct{}),
	}
}

// buildKey creates a cache key from a CacheKey.
func (c *L1Cache) buildKey(key CacheKey) string {
	// Include file hashes in the key for file-aware caching
	fileHashStr := ""
	if len(key.FileHashes) > 0 {
		keys := make([]string, 0, len(key.FileHashes))
		for k := range key.FileHashes {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		hashInput := make(map[string]string)
		for _, k := range keys {
			hashInput[k] = key.FileHashes[k]
		}

		jsonBytes, _ := json.Marshal(hashInput)
		h := sha256.New()
		h.Write(jsonBytes)
		fileHashStr = ":" + hex.EncodeToString(h.Sum(nil)[:8])
	}

	return key.ModelID + ":" + key.PromptHash + fileHashStr
}

// Get retrieves an entry from the cache.
func (c *L1Cache) Get(key CacheKey) (*CacheEntry, bool) {
	cacheKey := c.buildKey(key)

	c.mu.RLock()
	entry, exists := c.entries[cacheKey]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check if expired
	if entry.Entry.IsExpired() {
		c.mu.Lock()
		delete(c.entries, cacheKey)
		c.mu.Unlock()
		return nil, false
	}

	// Increment hit count
	c.mu.Lock()
	entry.Entry.HitCount++
	c.stats.L1Hits++
	c.mu.Unlock()

	c.logger.Debug("L1 cache hit", "key", cacheKey[:min(16, len(cacheKey))], "hit_count", entry.Entry.HitCount)
	return entry.Entry, true
}

// Put stores an entry in the cache.
func (c *L1Cache) Put(key CacheKey, entry *CacheEntry) {
	cacheKey := c.buildKey(key)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity and new key
	if len(c.entries) >= c.config.MaxEntries {
		if _, exists := c.entries[cacheKey]; !exists {
			c.evictOldest()
		}
	}

	c.entries[cacheKey] = &l1CacheEntry{
		Key:     cacheKey,
		Entry:   entry,
		FileMap: key.FileHashes,
	}

	c.logger.Debug("L1 cache stored", "key", cacheKey[:min(16, len(cacheKey))], "expires_at", entry.ExpiresAt)
}

// Invalidate removes a specific entry.
func (c *L1Cache) Invalidate(key CacheKey) {
	cacheKey := c.buildKey(key)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[cacheKey]; exists {
		delete(c.entries, cacheKey)
		c.logger.Debug("L1 cache invalidated", "key", cacheKey[:min(16, len(cacheKey))])
	}
}

// InvalidateByFile removes all entries referencing the given file path.
func (c *L1Cache) InvalidateByFile(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	for key, entry := range c.entries {
		if entry.FileMap != nil {
			if _, hasFile := entry.FileMap[filePath]; hasFile {
				delete(c.entries, key)
				evicted++
			}
		}
	}

	if evicted > 0 {
		c.logger.Debug("L1 cache invalidated by file", "file", filePath, "evicted", evicted)
	}
}

// Clear removes all entries.
func (c *L1Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*l1CacheEntry)
	c.logger.Debug("L1 cache cleared")
}

// Count returns the number of entries.
func (c *L1Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// l1InspectEntry is a lightweight result for inspection without the composite cache key.
type l1InspectEntry struct {
	ModelID    string
	Response   *Response
	CreatedAt  time.Time
	ExpiresAt  time.Time
	HitCount   int
	FileHashes map[string]string
}

// Inspect searches for entries matching the given prompt hash.
// It scans all entries and returns those whose composite key contains the prompt hash.
func (c *L1Cache) Inspect(promptHash string) []l1InspectEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var results []l1InspectEntry
	for _, entry := range c.entries {
		// The composite key format is: modelID:promptHash[:fileHashSuffix]
		// Check if this entry's key contains the prompt hash
		if !containsPromptHash(entry.Key, promptHash) {
			continue
		}
		// Skip expired entries
		if entry.Entry.IsExpired() {
			continue
		}
		// Extract model ID from the key (everything before the first colon)
		modelID := entry.Key
		if idx := strings.Index(entry.Key, ":"); idx >= 0 {
			modelID = entry.Key[:idx]
		}
		results = append(results, l1InspectEntry{
			ModelID:    modelID,
			Response:   entry.Entry.Response,
			CreatedAt:  entry.Entry.CreatedAt,
			ExpiresAt:  entry.Entry.ExpiresAt,
			HitCount:   entry.Entry.HitCount,
			FileHashes: entry.FileMap,
		})
	}
	return results
}

// containsPromptHash checks if a composite key contains the given prompt hash.
func containsPromptHash(key, promptHash string) bool {
	// Keys are format: modelID:promptHash[:fileHashSuffix]
	// We need to find promptHash after the first colon
	idx := strings.Index(key, ":")
	if idx < 0 {
		return false
	}
	rest := key[idx+1:]
	// The prompt hash is 64 hex chars (SHA256)
	if len(promptHash) > 16 {
		// Match truncated form if provided
		if len(rest) >= len(promptHash) && rest[:len(promptHash)] == promptHash {
			return true
		}
	}
	// Match full hash
	if len(rest) >= 64 && rest[:64] == promptHash {
		return true
	}
	// Check if the full key segment matches
	if rest == promptHash {
		return true
	}
	return false
}

// evictOldest removes the oldest entry.
func (c *L1Cache) evictOldest() {
	if len(c.entries) == 0 {
		return
	}

	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range c.entries {
		if first || entry.Entry.CreatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Entry.CreatedAt
			first = false
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
		c.stats.Evictions++
		c.logger.Debug("L1 cache eviction", "key", oldestKey[:min(16, len(oldestKey))])
	}
}

// Start starts the background cleanup goroutine.
func (c *L1Cache) Start() {
	go func() {
		ticker := time.NewTicker(c.config.CleanupFreq)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.cleanupExpired()
			case <-c.stopCh:
				return
			}
		}
	}()
}

// Stop stops the background cleanup.
func (c *L1Cache) Stop() {
	close(c.stopCh)
}

// cleanupExpired removes all expired entries.
func (c *L1Cache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expired := 0

	for key, entry := range c.entries {
		if now.After(entry.Entry.ExpiresAt) {
			delete(c.entries, key)
			expired++
		}
	}

	if expired > 0 {
		c.logger.Debug("L1 cache cleanup", "expired", expired)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
