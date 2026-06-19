// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheConfig holds caching parameters.
type CacheConfig struct {
	// RefreshMode determines when to refresh cached data.
	// "auto" - invalidates on file mtime change (default)
	// "manual" - only refreshes on explicit invalidation
	// "files" - refreshes when file list changes
	// "always" - never uses cache, always recomputes
	RefreshMode string

	// CacheDir is the directory for disk-based cache storage.
	// Default: "~/.meept/repomap_cache"
	CacheDir string

	// MaxCacheSize is the maximum size of the disk cache in bytes.
	// Default: 500MB
	MaxCacheSize int64

	// EnableMemoryCache enables in-memory caching.
	// Default: true
	EnableMemoryCache bool

	// MemoryCacheSize is the max number of entries in memory cache.
	// Default: 100
	MemoryCacheSize int

	// Logger for cache operations.
	Logger *slog.Logger
}

// DefaultCacheConfig returns a CacheConfig with default values.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		RefreshMode:       "auto",
		MaxCacheSize:      500 * 1024 * 1024, // 500 MB
		EnableMemoryCache: true,
		MemoryCacheSize:   100,
	}
}

// ValidateCacheConfig validates the cache configuration.
func ValidateCacheConfig(config CacheConfig) error {
	validModes := map[string]bool{
		"auto":   true,
		"manual": true,
		"files":  true,
		"always": true,
	}

	if !validModes[config.RefreshMode] {
		return fmt.Errorf("invalid RefreshMode: %s (must be one of: auto, manual, files, always)", config.RefreshMode)
	}

	if config.MaxCacheSize <= 0 {
		return fmt.Errorf("MaxCacheSize must be positive, got %d", config.MaxCacheSize)
	}

	if config.MemoryCacheSize <= 0 {
		return fmt.Errorf("MemoryCacheSize must be positive, got %d", config.MemoryCacheSize)
	}

	return nil
}

// TagCache is disk-based cache for tags with mtime validation.
type TagCache struct {
	cacheDir string
	config   CacheConfig
	logger   *slog.Logger
}

// NewTagCache creates a new TagCache with the given configuration.
func NewTagCache(config CacheConfig) (*TagCache, error) {
	// Expand tilde in cache directory
	cacheDir := config.CacheDir
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = os.TempDir()
		}
		cacheDir = filepath.Join(homeDir, ".meept", "repomap_cache")
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Apply defaults
	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = 500 * 1024 * 1024
	}

	return &TagCache{
		cacheDir: cacheDir,
		config:   config,
		logger:   config.Logger,
	}, nil
}

// cacheKey generates a cache key from a file path.
func (c *TagCache) cacheKey(filePath string) string {
	hash := sha256.Sum256([]byte(filePath))
	return hex.EncodeToString(hash[:])
}

// cacheFilePath returns the full path to the cache file for a given key.
func (c *TagCache) cacheFilePath(key string) string {
	// Use first 2 chars as subdirectory for better filesystem performance
	subDir := key[:2]
	return filepath.Join(c.cacheDir, subDir, key+".json")
}

// Get retrieves cached tags for a file if they exist and are valid.
func (c *TagCache) Get(filePath string, mtime time.Time) ([]Tag, bool, error) {
	// Handle "always" refresh mode
	if c.config.RefreshMode == "always" {
		return nil, false, nil
	}

	key := c.cacheKey(filePath)
	cachePath := c.cacheFilePath(key)

	// Check if cache file exists
	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read cache file: %w", err)
	}

	// Unmarshal the cache entry
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal cache entry: %w", err)
	}

	// Validate mtime if in auto mode
	if c.config.RefreshMode == "auto" {
		if entry.Mtime != mtime.Unix() {
			// Cache is stale
			if c.logger != nil {
				c.logger.Debug("cache invalidated due to mtime change", "file", filePath)
			}
			return nil, false, nil
		}
	}

	// Unmarshal tags
	var tags []Tag
	if err := json.Unmarshal(entry.Data, &tags); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal tags: %w", err)
	}

	return tags, true, nil
}

// Set stores tags in the cache for a given file.
func (c *TagCache) Set(filePath string, mtime time.Time, tags []Tag) error {
	key := c.cacheKey(filePath)
	cachePath := c.cacheFilePath(key)

	// Ensure subdirectory exists
	subDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache subdirectory: %w", err)
	}

	// Serialize tags
	data, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	// Create cache entry
	entry := cacheEntry{
		Mtime: mtime.Unix(),
		Data:  data,
	}

	// Write to cache file
	entryData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	if err := os.WriteFile(cachePath, entryData, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	if c.logger != nil {
		c.logger.Debug("cached tags", "file", filePath, "count", len(tags))
	}

	return nil
}

// Invalidate removes cached data for a specific file.
func (c *TagCache) Invalidate(filePath string) error {
	key := c.cacheKey(filePath)
	cachePath := c.cacheFilePath(key)

	if err := os.Remove(cachePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	if c.logger != nil {
		c.logger.Debug("invalidated cache", "file", filePath)
	}

	return nil
}

// Clear removes all cached data.
func (c *TagCache) Clear() error {
	if err := os.RemoveAll(c.cacheDir); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// Recreate the cache directory
	if err := os.MkdirAll(c.cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate cache directory: %w", err)
	}

	if c.logger != nil {
		c.logger.Info("cleared tag cache")
	}

	return nil
}

// Size returns the current size of the disk cache in bytes.
func (c *TagCache) Size() (int64, error) {
	var totalSize int64

	err := filepath.Walk(c.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to calculate cache size: %w", err)
	}

	return totalSize, nil
}

// Stats returns cache statistics.
func (c *TagCache) Stats() (numFiles int64, sizeBytes int64, err error) {
	numFiles = 0
	sizeBytes = 0

	err = filepath.Walk(c.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			numFiles++
			sizeBytes += info.Size()
		}
		return nil
	})

	return numFiles, sizeBytes, err
}

// CachedMap represents a cached rendered map with its parameters.
type CachedMap struct {
	Content     string
	Tokens      int
	FileSetHash string
	Timestamp   time.Time
	ChatFiles   []string
	Identifiers []string
}

// MapCache is an in-memory cache for complete rendered maps.
type MapCache struct {
	mu     sync.RWMutex
	cache  map[string]*CachedMap
	config CacheConfig
}

// NewMapCache creates a new MapCache with the given configuration.
func NewMapCache(config CacheConfig) *MapCache {
	if config.MemoryCacheSize == 0 {
		config.MemoryCacheSize = 100
	}

	return &MapCache{
		cache:  make(map[string]*CachedMap, config.MemoryCacheSize),
		config: config,
	}
}

// mapCacheKey generates a key for the map cache based on file list and identifiers.
func (m *MapCache) mapCacheKey(files []string, identifiers []string) string {
	// Combine all inputs into a single string
	keyData := fmt.Sprintf("%v|%v", files, identifiers)
	hash := sha256.Sum256([]byte(keyData))
	return hex.EncodeToString(hash[:])
}

// Get retrieves a cached map if it exists and is still valid.
func (m *MapCache) Get(files []string, identifiers []string, maxAge time.Duration) (*CachedMap, bool) {
	if !m.config.EnableMemoryCache {
		return nil, false
	}

	key := m.mapCacheKey(files, identifiers)

	m.mu.RLock()
	defer m.mu.RUnlock()

	cached, ok := m.cache[key]
	if !ok {
		return nil, false
	}

	// Check if cache is expired
	if maxAge > 0 && time.Since(cached.Timestamp) > maxAge {
		return nil, false
	}

	return cached, true
}

// Set stores a rendered map in the cache.
func (m *MapCache) Set(files []string, identifiers []string, rendered RenderedMap) {
	if !m.config.EnableMemoryCache {
		return
	}

	key := m.mapCacheKey(files, identifiers)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Evict oldest if at capacity
	if len(m.cache) >= m.config.MemoryCacheSize {
		m.evictOldest()
	}

	m.cache[key] = &CachedMap{
		Content:     rendered.Content,
		Tokens:      rendered.Tokens,
		FileSetHash: key,
		Timestamp:   time.Now(),
		ChatFiles:   files,
		Identifiers: identifiers,
	}
}

// evictOldest removes the oldest entry from the cache.
func (m *MapCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range m.cache {
		if oldestTime.IsZero() || entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
		}
	}

	if oldestKey != "" {
		delete(m.cache, oldestKey)
	}
}

// Clear removes all entries from the cache.
func (m *MapCache) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]*CachedMap, m.config.MemoryCacheSize)
}

// Size returns the number of entries in the cache.
func (m *MapCache) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.cache)
}

// RenderCache is an in-memory cache for rendered tree output.
type RenderCache struct {
	mu     sync.RWMutex
	cache  map[string]string // Key: file:line hash -> rendered content
	config CacheConfig
}

// NewRenderCache creates a new RenderCache with the given configuration.
func NewRenderCache(config CacheConfig) *RenderCache {
	if config.MemoryCacheSize == 0 {
		config.MemoryCacheSize = 100
	}

	return &RenderCache{
		cache:  make(map[string]string, config.MemoryCacheSize),
		config: config,
	}
}

// renderCacheKey generates a key for render cache based on file and line.
func (r *RenderCache) renderCacheKey(filePath string, line int) string {
	keyData := fmt.Sprintf("%s:%d", filePath, line)
	hash := sha256.Sum256([]byte(keyData))
	return hex.EncodeToString(hash[:])
}

// Get retrieves cached rendered content for a file and line.
func (r *RenderCache) Get(filePath string, line int) (string, bool) {
	if !r.config.EnableMemoryCache {
		return "", false
	}

	key := r.renderCacheKey(filePath, line)

	r.mu.RLock()
	defer r.mu.RUnlock()

	content, ok := r.cache[key]
	return content, ok
}

// Set stores rendered content in the cache.
func (r *RenderCache) Set(filePath string, line int, content string) {
	if !r.config.EnableMemoryCache {
		return
	}

	key := r.renderCacheKey(filePath, line)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Simple eviction: clear half when full
	if len(r.cache) >= r.config.MemoryCacheSize {
		r.evictHalf()
	}

	r.cache[key] = content
}

// evictHalf removes approximately half of the cache entries.
func (r *RenderCache) evictHalf() {
	targetSize := max(len(r.cache)/2, 10)

	// Create new map with target size
	newCache := make(map[string]string, targetSize)

	// Copy random entries (approximate eviction)
	count := 0
	for key, value := range r.cache {
		if count < targetSize {
			newCache[key] = value
			count++
		}
	}

	r.cache = newCache
}

// Clear removes all entries from the cache.
func (r *RenderCache) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]string, r.config.MemoryCacheSize)
}

// Size returns the number of entries in the cache.
func (r *RenderCache) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}

// CacheManager manages all three cache layers and provides a unified interface.
type CacheManager struct {
	tagCache    *TagCache
	mapCache    *MapCache
	renderCache *RenderCache
	config      CacheConfig
	logger      *slog.Logger
}

// NewCacheManager creates a new CacheManager with all cache layers.
func NewCacheManager(config CacheConfig, logger *slog.Logger) (*CacheManager, error) {
	// Validate config
	if err := ValidateCacheConfig(config); err != nil {
		return nil, err
	}

	// Create tag cache (disk-based)
	tagCache, err := NewTagCache(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create tag cache: %w", err)
	}

	// Create map cache (memory-based)
	mapCache := NewMapCache(config)

	// Create render cache (memory-based)
	renderCache := NewRenderCache(config)

	return &CacheManager{
		tagCache:    tagCache,
		mapCache:    mapCache,
		renderCache: renderCache,
		config:      config,
		logger:      logger,
	}, nil
}

// GetTags retrieves cached tags for a file.
func (cm *CacheManager) GetTags(filePath string, mtime time.Time) ([]Tag, bool, error) {
	return cm.tagCache.Get(filePath, mtime)
}

// SetTags stores tags in the cache for a file.
func (cm *CacheManager) SetTags(filePath string, mtime time.Time, tags []Tag) error {
	return cm.tagCache.Set(filePath, mtime, tags)
}

// GetRenderedMap retrieves a cached rendered map.
func (cm *CacheManager) GetRenderedMap(files []string, identifiers []string, maxAge time.Duration) (*CachedMap, bool) {
	return cm.mapCache.Get(files, identifiers, maxAge)
}

// SetRenderedMap stores a rendered map in the cache.
func (cm *CacheManager) SetRenderedMap(files []string, identifiers []string, rendered RenderedMap) {
	cm.mapCache.Set(files, identifiers, rendered)
}

// GetRenderedContext retrieves cached rendered context for a symbol.
func (cm *CacheManager) GetRenderedContext(filePath string, line int) (string, bool) {
	return cm.renderCache.Get(filePath, line)
}

// SetRenderedContext stores rendered context in the cache.
func (cm *CacheManager) SetRenderedContext(filePath string, line int, content string) {
	cm.renderCache.Set(filePath, line, content)
}

// InvalidateTags removes cached tags for a specific file.
func (cm *CacheManager) InvalidateTags(filePath string) error {
	return cm.tagCache.Invalidate(filePath)
}

// ClearTagCache clears the disk-based tag cache.
func (cm *CacheManager) ClearTagCache() error {
	return cm.tagCache.Clear()
}

// ClearAll clears all caches (both disk and memory).
func (cm *CacheManager) ClearAll() error {
	// Clear memory caches
	cm.mapCache.Clear()
	cm.renderCache.Clear()

	// Clear disk cache
	return cm.tagCache.Clear()
}

// Stats returns statistics for all caches.
func (cm *CacheManager) Stats() (tagFiles int64, tagSize int64, mapEntries int, renderEntries int, err error) {
	tagFiles, tagSize, err = cm.tagCache.Stats()
	if err != nil {
		return 0, 0, 0, 0, err
	}

	mapEntries = cm.mapCache.Size()
	renderEntries = cm.renderCache.Size()

	return tagFiles, tagSize, mapEntries, renderEntries, nil
}

// getFileMtime returns the modification time of a file.
// Helper function for compatibility with the plan.
func getFileMtime(filePath string) time.Time {
	info, err := os.Stat(filePath)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
