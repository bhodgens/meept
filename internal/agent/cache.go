package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Default cached tools - read-only, idempotent operations safe for caching
var defaultCachedTools = []string{
	"file_read",
	"list_directory",
	"memory_search",
	"memory_get_context",
	"platform_status",
	"platform_agents",
	"platform_tools",
}

// CacheConfig holds configuration for the result cache.
type CacheConfig struct {
	// MaxEntries is the maximum number of entries to keep in the cache.
	MaxEntries int
	// DefaultTTL is the default time-to-live for cache entries.
	DefaultTTL time.Duration
	// CleanupFreq is the frequency of background cleanup.
	CleanupFreq time.Duration
	// EnabledTools is the list of tools that are cacheable.
	EnabledTools []string
}

// DefaultCacheConfig returns a cache configuration with sensible defaults.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxEntries:   1000,
		DefaultTTL:   5 * time.Minute,
		CleanupFreq:  1 * time.Minute,
		EnabledTools: defaultCachedTools,
	}
}

// CacheEntry represents a single cached result.
type CacheEntry struct {
	ToolName  string
	ArgsHash  string
	Result    any
	CachedAt  time.Time
	HitCount  int
	ExpiresAt time.Time
}

// IsExpired checks if the cache entry has expired.
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// CacheStats holds statistics about cache performance.
type CacheStats struct {
	Hits      int
	Misses    int
	Evictions int
}

// ResultCache provides an in-memory cache for tool execution results.
type ResultCache struct {
	entries map[string]*CacheEntry
	config  CacheConfig
	mu      sync.RWMutex
	stats   CacheStats
	logger  *slog.Logger
	stopCh  chan struct{}
}

// NewResultCache creates a new result cache with the given configuration.
// If config.DefaultTTL is 0, defaults to 5 minutes.
// If config.MaxEntries is 0, defaults to 1000.
// If config.CleanupFreq is 0, defaults to 1 minute.
// If config.EnabledTools is nil/empty, uses default cached tools.
func NewResultCache(config CacheConfig, logger *slog.Logger) *ResultCache {
	if logger == nil {
		logger = slog.Default()
	}

	// Apply defaults
	if config.MaxEntries <= 0 {
		config.MaxEntries = 1000
	}
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}
	if config.CleanupFreq <= 0 {
		config.CleanupFreq = 1 * time.Minute
	}
	if len(config.EnabledTools) == 0 {
		config.EnabledTools = defaultCachedTools
	}

	return &ResultCache{
		entries: make(map[string]*CacheEntry),
		config:  config,
		logger:  logger.With("component", "result_cache"),
		stopCh:  make(chan struct{}),
	}
}

// generateCacheKey creates a cache key from tool name and arguments.
// Format: {toolName}:{argsHash}
func (c *ResultCache) generateCacheKey(toolName string, args map[string]any) string {
	argsHash := c.hashArgs(args)
	return toolName + ":" + argsHash
}

// hashArgs creates a deterministic hash of the arguments map.
// Returns the first 16 hex characters of the SHA256 hash.
// JSON keys are sorted before hashing to ensure consistency.
func (c *ResultCache) hashArgs(args map[string]any) string {
	if args == nil || len(args) == 0 {
		return "empty"
	}

	// Marshal args to JSON with sorted keys for deterministic hashing
	// Create a sorted map of keys
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sortedArgs := make(map[string]any, len(args))
	for _, k := range keys {
		sortedArgs[k] = args[k]
	}

	jsonBytes, err := json.Marshal(sortedArgs)
	if err != nil {
		c.logger.Warn("Failed to marshal args for hashing", "error", err)
		return "error"
	}

	// Generate SHA256 hash
	h := sha256.New()
	h.Write(jsonBytes)
	hashBytes := h.Sum(nil)

	// Return first 16 hex characters
	return hex.EncodeToString(hashBytes)[:16]
}

// isToolEnabled checks if a tool is enabled for caching.
func (c *ResultCache) isToolEnabled(toolName string) bool {
	for _, tool := range c.config.EnabledTools {
		if tool == toolName {
			return true
		}
	}
	return false
}

// Get retrieves a cached result for the given tool and arguments.
// Returns the cached result and true if found (and not expired).
// Returns nil and false if not found, expired, or tool not cacheable.
func (c *ResultCache) Get(toolName string, args map[string]any) (any, bool) {
	// Check if tool is enabled for caching
	if !c.isToolEnabled(toolName) {
		c.mu.Lock()
		c.stats.Misses++
		c.mu.Unlock()
		return nil, false
	}

	key := c.generateCacheKey(toolName, args)

	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		c.mu.Lock()
		c.stats.Misses++
		c.mu.Unlock()
		return nil, false
	}

	// Check if entry has expired
	if entry.IsExpired() {
		c.mu.Lock()
		delete(c.entries, key)
		c.stats.Misses++
		c.mu.Unlock()
		return nil, false
	}

	// Increment hit count and track hit
	c.mu.Lock()
	entry.HitCount++
	c.stats.Hits++
	c.mu.Unlock()

	c.logger.Debug("Cache hit", "tool", toolName, "key", key, "hit_count", entry.HitCount)
	return entry.Result, true
}

// Put stores a tool execution result in the cache.
// Does nothing if the tool is not enabled for caching.
func (c *ResultCache) Put(toolName string, args map[string]any, result any) {
	// Check if tool is enabled for caching
	if !c.isToolEnabled(toolName) {
		return
	}

	key := c.generateCacheKey(toolName, args)
	now := time.Now()

	entry := &CacheEntry{
		ToolName:  toolName,
		ArgsHash:  c.hashArgs(args),
		Result:    result,
		CachedAt:  now,
		HitCount:  0,
		ExpiresAt: now.Add(c.config.DefaultTTL),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.entries) >= c.config.MaxEntries {
		if _, exists := c.entries[key]; exists {
			// Updating existing entry - no eviction needed
			c.entries[key] = entry
			c.logger.Debug("Cache updated", "tool", toolName, "key", key)
			return
		}
		// New entry and at capacity - evict oldest
		c.evictIfNeeded()
	}

	c.entries[key] = entry
	c.logger.Debug("Cache stored", "tool", toolName, "key", key, "expires_at", entry.ExpiresAt)
}

// evictIfNeeded removes the oldest entry if the cache is at capacity.
// Must be called with write lock held.
func (c *ResultCache) evictIfNeeded() {
	if len(c.entries) < c.config.MaxEntries {
		return
	}

	// Find oldest entry
	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range c.entries {
		if first || entry.CachedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.CachedAt
			first = false
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
		c.stats.Evictions++
		c.logger.Debug("Cache eviction", "key", oldestKey, "cached_at", oldestTime)
	}
}

// Invalidate removes a specific cached entry.
// Does nothing if the entry doesn't exist.
func (c *ResultCache) Invalidate(toolName string, args map[string]any) {
	key := c.generateCacheKey(toolName, args)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		delete(c.entries, key)
		c.logger.Debug("Cache invalidated", "tool", toolName, "key", key)
	}
}

// InvalidateTool removes all cached entries for a specific tool.
func (c *ResultCache) InvalidateTool(toolName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefix := toolName + ":"
	evicted := 0

	for key := range c.entries {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.entries, key)
			evicted++
		}
	}

	if evicted > 0 {
		c.logger.Debug("Cache invalidated tool", "tool", toolName, "evicted", evicted)
	}
}

// Clear removes all entries from the cache and resets statistics.
func (c *ResultCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.stats = CacheStats{}
	c.logger.Debug("Cache cleared")
}

// Stats returns current cache statistics.
func (c *ResultCache) Stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.stats.Hits + c.stats.Misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.stats.Hits) / float64(total) * 100
	}

	return map[string]any{
		"hits":         c.stats.Hits,
		"misses":       c.stats.Misses,
		"evictions":    c.stats.Evictions,
		"entries":      len(c.entries),
		"max_entries":  c.config.MaxEntries,
		"hit_rate":     hitRate,
		"enabled_tools": c.config.EnabledTools,
	}
}

// Start starts the background cleanup goroutine.
func (c *ResultCache) Start() {
	if c == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(c.config.CleanupFreq)
		defer ticker.Stop()

		c.logger.Debug("Cache cleanup started", "frequency", c.config.CleanupFreq)

		for {
			select {
			case <-ticker.C:
				c.cleanupExpired()
			case <-c.stopCh:
				c.logger.Debug("Cache cleanup stopped")
				return
			}
		}
	}()
}

// Stop stops the background cleanup goroutine.
func (c *ResultCache) Stop() {
	if c == nil {
		return
	}

	close(c.stopCh)
	c.logger.Debug("Cache cleanup stopped")
}

// cleanupExpired removes all expired entries from the cache.
func (c *ResultCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	expired := 0
	now := time.Now()

	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
			expired++
		}
	}

	if expired > 0 {
		c.logger.Debug("Cache cleanup removed expired entries", "count", expired)
	}
}
