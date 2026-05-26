package ast

import (
	"os"
	"sync"
	"time"
)

// cacheEntry holds a cached parse result with metadata.
type cacheEntry struct {
	result   *ParseResult
	modTime  time.Time
	cachedAt time.Time
}

// ParseCache caches parse results keyed by file path.
// It validates entries against file modification time and TTL.
type ParseCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	maxSize int
	ttl     time.Duration
	order   []string // LRU order tracking
}

// NewParseCache creates a new parse cache.
func NewParseCache(maxSize int, ttl time.Duration) *ParseCache {
	return &ParseCache{
		entries: make(map[string]*cacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
		order:   make([]string, 0, maxSize),
	}
}

// Get retrieves a cached parse result if valid.
// Returns nil if not cached or if the cache entry is stale.
func (c *ParseCache) Get(filePath string) *ParseResult {
	c.mu.RLock()
	entry, exists := c.entries[filePath]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check TTL
	if time.Since(entry.cachedAt) > c.ttl {
		c.Invalidate(filePath)
		return nil
	}

	// Check if file was modified
	info, err := os.Stat(filePath)
	if err != nil {
		c.Invalidate(filePath)
		return nil
	}

	if !info.ModTime().Equal(entry.modTime) {
		c.Invalidate(filePath)
		return nil
	}

	// Move to front of LRU
	c.touchLRU(filePath)

	return entry.result
}

// Put stores a parse result in the cache.
func (c *ParseCache) Put(filePath string, result *ParseResult) {
	info, err := os.Stat(filePath)
	if err != nil {
		return // Don't cache if we can't stat the file
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[filePath] = &cacheEntry{
		result:   result,
		modTime:  info.ModTime(),
		cachedAt: time.Now(),
	}

	c.addToLRU(filePath)
}

// Invalidate removes a file from the cache.
func (c *ParseCache) Invalidate(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, filePath)
	c.removeFromLRU(filePath)
}

// Clear removes all entries from the cache.
func (c *ParseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
	c.order = make([]string, 0, c.maxSize)
}

// Size returns the number of cached entries.
func (c *ParseCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// touchLRU moves an entry to the front of the LRU list.
func (c *ParseCache) touchLRU(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.removeFromLRU(filePath)
	c.addToLRU(filePath)
}

// addToLRU adds an entry to the front of the LRU list.
// Caller must hold the write lock.
func (c *ParseCache) addToLRU(filePath string) {
	c.order = append([]string{filePath}, c.order...)
}

// removeFromLRU removes an entry from the LRU list.
// Caller must hold the write lock.
func (c *ParseCache) removeFromLRU(filePath string) {
	for i, path := range c.order {
		if path == filePath {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// evictOldest removes the oldest entry from the cache.
// Caller must hold the write lock.
func (c *ParseCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}

	oldest := c.order[len(c.order)-1]
	c.order = c.order[:len(c.order)-1]
	delete(c.entries, oldest)
}

// Stats returns cache statistics.
func (c *ParseCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Size:    len(c.entries),
		MaxSize: c.maxSize,
		TTL:     c.ttl,
	}
}

// CacheStats holds cache statistics.
type CacheStats struct {
	Size    int
	MaxSize int
	TTL     time.Duration
}
