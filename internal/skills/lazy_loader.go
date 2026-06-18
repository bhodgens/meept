package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// LazySkillLoader loads skill bodies on-demand with LRU caching.
type LazySkillLoader struct {
	mu        sync.RWMutex
	index     *SkillIndex
	cache     map[string]*cacheEntry
	order     []string // LRU order: most recently used at end
	cacheSize int
	logger    *slog.Logger
	stats     LoaderStats
}

// cacheEntry holds a cached skill.
type cacheEntry struct {
	skill *Skill
}

// LoaderStats tracks loader statistics.
type LoaderStats struct {
	Hits   int64
	Misses int64
	Evicts int64
	Loads  int64
	Errors int64
}

// LazyLoaderOption is a functional option for configuring LazySkillLoader.
type LazyLoaderOption func(*LazySkillLoader)

// WithLoaderLogger sets the logger for the loader.
func WithLoaderLogger(logger *slog.Logger) LazyLoaderOption {
	return func(l *LazySkillLoader) {
		l.logger = logger
	}
}

// WithCacheSize sets the maximum number of skills to cache.
func WithCacheSize(size int) LazyLoaderOption {
	return func(l *LazySkillLoader) {
		if size > 0 {
			l.cacheSize = size
		}
	}
}

// NewLazySkillLoader creates a new lazy loader.
func NewLazySkillLoader(index *SkillIndex, opts ...LazyLoaderOption) *LazySkillLoader {
	l := &LazySkillLoader{
		index:     index,
		cache:     make(map[string]*cacheEntry),
		order:     make([]string, 0),
		cacheSize: 50, // Default cache size
		logger:    slog.Default(),
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Load loads a skill by name, using cache if available.
func (l *LazySkillLoader) Load(ctx context.Context, name string) (*Skill, error) {
	key := normalizeName(name)

	// Check cache first (read lock)
	l.mu.RLock()
	if entry, ok := l.cache[key]; ok {
		l.mu.RUnlock()
		l.recordHit(key)
		return entry.skill, nil
	}
	l.mu.RUnlock()

	// Cache miss - need to load
	return l.loadAndCache(ctx, key)
}

// recordHit updates LRU order for a cache hit.
func (l *LazySkillLoader) recordHit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stats.Hits++

	// Move to end of LRU order
	for i, k := range l.order {
		if k == key {
			l.order = append(l.order[:i], l.order[i+1:]...)
			l.order = append(l.order, key)
			break
		}
	}
}

// loadAndCache loads a skill from disk and caches it.
func (l *LazySkillLoader) loadAndCache(ctx context.Context, key string) (*Skill, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check cache after acquiring write lock
	if entry, ok := l.cache[key]; ok {
		l.stats.Hits++
		return entry.skill, nil
	}

	l.stats.Misses++

	// Get path from index
	indexEntry := l.index.Get(key) //nolint:mutexio // in-memory index map lookup, not I/O
	if indexEntry == nil {
		l.stats.Errors++
		return nil, fmt.Errorf("skill not found in index: %s", key)
	}

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Parse full skill from file
	skill, err := ParseSkillFile(indexEntry.Path)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			// Missing frontmatter — still usable, just warn.
			l.logger.Warn("Skill file has no frontmatter, using slug as name",
				"name", key,
				"path", indexEntry.Path,
			)
		} else {
			l.stats.Errors++
			l.logger.Warn("Failed to load skill",
				"name", key,
				"path", indexEntry.Path,
				"error", err,
			)
			return nil, fmt.Errorf("failed to load skill %s: %w", key, err)
		}
	}

	l.stats.Loads++

	// Evict if at capacity
	l.evictIfNeeded()

	// Add to cache
	l.cache[key] = &cacheEntry{skill: skill}
	l.order = append(l.order, key)

	l.logger.Debug("Loaded skill into cache",
		"name", skill.Name,
		"path", skill.Path,
		"cache_size", len(l.cache),
	)

	return skill, nil
}

// evictIfNeeded removes the least recently used entry if at capacity.
func (l *LazySkillLoader) evictIfNeeded() {
	for len(l.cache) >= l.cacheSize && len(l.order) > 0 {
		// Remove oldest (first in order)
		oldest := l.order[0]
		l.order = l.order[1:]
		delete(l.cache, oldest)
		l.stats.Evicts++

		l.logger.Debug("Evicted skill from cache", "name", oldest)
	}
}

// Preload loads specific skills into the cache.
func (l *LazySkillLoader) Preload(ctx context.Context, names []string) error {
	for _, name := range names {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := l.Load(ctx, name); err != nil {
			l.logger.Warn("Preload failed for skill", "name", name, "error", err)
			// Continue with other skills
		}
	}
	return nil
}

// Get retrieves a skill from cache only (no loading).
func (l *LazySkillLoader) Get(name string) *Skill {
	key := normalizeName(name)

	l.mu.RLock()
	defer l.mu.RUnlock()

	if entry, ok := l.cache[key]; ok {
		return entry.skill
	}
	return nil
}

// IsCached checks if a skill is currently in the cache.
func (l *LazySkillLoader) IsCached(name string) bool {
	key := normalizeName(name)

	l.mu.RLock()
	defer l.mu.RUnlock()

	_, ok := l.cache[key]
	return ok
}

// Invalidate removes a skill from the cache.
func (l *LazySkillLoader) Invalidate(name string) {
	key := normalizeName(name)

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.cache[key]; ok {
		delete(l.cache, key)

		// Remove from LRU order
		for i, k := range l.order {
			if k == key {
				l.order = append(l.order[:i], l.order[i+1:]...)
				break
			}
		}

		l.logger.Debug("Invalidated skill from cache", "name", name)
	}
}

// Clear removes all skills from the cache.
func (l *LazySkillLoader) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cache = make(map[string]*cacheEntry)
	l.order = make([]string, 0)

	l.logger.Debug("Cleared skill cache")
}

// CachedCount returns the number of skills currently cached.
func (l *LazySkillLoader) CachedCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.cache)
}

// Stats returns current loader statistics.
func (l *LazySkillLoader) Stats() LoaderStats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.stats
}

// CacheSize returns the configured cache size.
func (l *LazySkillLoader) CacheSize() int {
	return l.cacheSize
}

// Index returns the underlying skill index.
func (l *LazySkillLoader) Index() *SkillIndex {
	return l.index
}

// CachedNames returns the names of currently cached skills.
func (l *LazySkillLoader) CachedNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, 0, len(l.cache))
	for _, entry := range l.cache {
		names = append(names, entry.skill.Name)
	}
	return names
}

// SetIndex updates the skill index (useful for reloading).
func (l *LazySkillLoader) SetIndex(index *SkillIndex) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index != nil {
		l.index = index
	}
	// Clear cache since index changed
	l.cache = make(map[string]*cacheEntry)
	l.order = make([]string, 0)
}
