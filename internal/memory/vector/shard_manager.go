package vector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// ShardType identifies the type/purpose of a vector shard.
type ShardType string

const (
	// ConsolidatedShard - ALWAYS in RAM, 768-dim, critical knowledge fragments
	ConsolidatedShard ShardType = "consolidated"
	// RecentShard - ALWAYS in RAM, 512-dim, current conversation context
	RecentShard ShardType = "recent"
	// ProjectShard - LAZY loaded, 256-dim, project-specific memories
	ProjectShard ShardType = "project"
	// CodeShard - LAZY loaded, 256-dim, code snippets
	CodeShard ShardType = "code"
	// ArchiveShard - DISK only, 128-dim, cold storage (90+ days)
	ArchiveShard ShardType = "archive"
)

// ShardDimension returns the recommended dimension for a shard type.
func (s ShardType) Dimension() int {
	switch s {
	case ConsolidatedShard:
		return Dim768
	case RecentShard:
		return Dim512
	case ProjectShard, CodeShard:
		return Dim256
	case ArchiveShard:
		return Dim128
	default:
		return Dim256
	}
}

// IsAlwaysLoaded returns true if this shard type should always be in RAM.
func (s ShardType) IsAlwaysLoaded() bool {
	return s == ConsolidatedShard || s == RecentShard
}

// ShardManager orchestrates multiple vector shards with LRU-based eviction.
type ShardManager struct {
	mu          sync.RWMutex
	shards      map[ShardType]*VectorShard
	lru         *LRUCache
	basePath    string
	logger      *slog.Logger
	embedder    Provider
	maxRAMShards int
}

// ShardManagerConfig holds configuration for the shard manager.
type ShardManagerConfig struct {
	BasePath       string // Base directory for shard files
	MaxRAMShards   int    // Max shards to keep in RAM (LRU eviction)
	Embedder       Provider
	Logger         *slog.Logger
}

// NewShardManager creates a new shard manager.
func NewShardManager(cfg ShardManagerConfig) (*ShardManager, error) {
	if cfg.BasePath == "" {
		cfg.BasePath = filepath.Join(os.Getenv("HOME"), ".meept", "memory", "shards")
	}
	if cfg.MaxRAMShards <= 0 {
		cfg.MaxRAMShards = 5 // Default: consolidated + recent + 3 project shards
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Ensure base directory exists
	if err := os.MkdirAll(cfg.BasePath, 0o755); err != nil {
		return nil, fmt.Errorf("create shard directory: %w", err)
	}

	m := &ShardManager{
		shards:       make(map[ShardType]*VectorShard),
		lru:          NewLRUCache(cfg.MaxRAMShards),
		basePath:     cfg.BasePath,
		logger:       cfg.Logger,
		embedder:     cfg.Embedder,
		maxRAMShards: cfg.MaxRAMShards,
	}

	// Pre-load always-loaded shards
	for _, st := range []ShardType{ConsolidatedShard, RecentShard} {
		if err := m.loadShard(st); err != nil {
			m.logger.Warn("failed to pre-load shard", "type", st, "error", err)
		}
	}

	return m, nil
}

// loadShard loads or creates a shard of the given type.
func (m *ShardManager) loadShard(shardType ShardType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Already loaded?
	if _, ok := m.shards[shardType]; ok {
		return nil
	}

	shardPath := filepath.Join(m.basePath, string(shardType)+".vec.db")

	shard, err := NewVectorShard(shardPath, shardType.Dimension(), DefaultM, DefaultEFConstruction)
	if err != nil {
		return fmt.Errorf("create shard %s: %w", shardType, err)
	}

	shard.WithProvider(m.embedder)
	m.shards[shardType] = shard
	m.lru.Access(string(shardType))

	m.logger.Info("loaded shard", "type", shardType, "path", shardPath, "dimension", shardType.Dimension())
	return nil
}

// unloadShard closes and removes a shard from RAM.
func (m *ShardManager) unloadShard(shardType ShardType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	shard, ok := m.shards[shardType]
	if !ok {
		return nil // not loaded
	}

	if err := shard.Close(); err != nil {
		m.logger.Warn("failed to close shard", "type", shardType, "error", err)
	}

	delete(m.shards, shardType)
	m.logger.Info("unloaded shard", "type", shardType)
	return nil
}

// GetShard returns a shard by type, loading it if necessary.
// For LAZY shards, this may trigger LRU eviction.
func (m *ShardManager) GetShard(shardType ShardType) (*VectorShard, error) {
	m.mu.Lock()

	// Load if not present
	if _, ok := m.shards[shardType]; !ok {
		if err := m.loadShard(shardType); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}

	// Update LRU
	m.lru.Access(string(shardType))
	shard := m.shards[shardType]

	// Evict if over capacity (skip always-loaded shards)
	if !shardType.IsAlwaysLoaded() {
		maybeEvict := m.lru.Len() > m.maxRAMShards
		if maybeEvict {
			// Find least-recently-used non-always-loaded shard
			for _, lruKey := range m.lru.Keys() {
				lruType := ShardType(lruKey)
				if !lruType.IsAlwaysLoaded() {
					_ = m.unlockAndUnloadShard(lruType)
					break
				}
			}
		}
	}

	m.mu.Unlock()
	return shard, nil
}

// unlockAndUnloadShard unloads a shard (caller must hold lock).
func (m *ShardManager) unlockAndUnloadShard(shardType ShardType) error {
	m.mu.Unlock()
	err := m.unloadShard(shardType)
	m.mu.Lock()
	return err
}

// GetProjectShard returns a project-specific shard by project ID.
func (m *ShardManager) GetProjectShard(projectID string) (*VectorShard, error) {
	// For project shards, we use a naming convention
	// In a full implementation, you'd have a separate map for project shards
	// For now, return the generic project shard
	return m.GetShard(ProjectShard)
}

// Stats returns statistics about all managed shards.
func (m *ShardManager) Stats() ShardManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := ShardManagerStats{
		LoadedShards:  len(m.shards),
		MaxRAMShards:  m.maxRAMShards,
		LRUHits:       m.lru.Hits(),
		LRUMisses:     m.lru.Misses(),
		LRUEvictions:  m.lru.Evictions(),
		ShardDetails:  make(map[ShardType]ShardStats),
	}

	for shardType, shard := range m.shards {
		stats.ShardDetails[shardType] = shard.Stats()
	}

	return stats
}

// ShardManagerStats holds statistics about the shard manager.
type ShardManagerStats struct {
	LoadedShards  int
	MaxRAMShards  int
	LRUHits       int64
	LRUMisses     int64
	LRUEvictions  int64
	ShardDetails  map[ShardType]ShardStats
}

// Search performs a multi-shard search across specified shard types.
func (m *ShardManager) Search(ctx context.Context, query string, k int, shardTypes []ShardType) ([]SearchResult, error) {
	// Generate query embedding
	queryEmb, err := m.embedder.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	var allResults []SearchResult
	for _, shardType := range shardTypes {
		shard, err := m.GetShard(shardType)
		if err != nil {
			m.logger.Warn("failed to get shard for search", "type", shardType, "error", err)
			continue
		}

		results, err := shard.Search(ctx, queryEmb, k/len(shardTypes), 0)
		if err != nil {
			m.logger.Warn("shard search failed", "type", shardType, "error", err)
			continue
		}

		allResults = append(allResults, results...)
	}

	// Sort by relevance and deduplicate
	return ConsolidateResults(allResults, k), nil
}

// ConsolidateResults merges and deduplicates search results.
func ConsolidateResults(results []SearchResult, limit int) []SearchResult {
	// Sort by relevance descending
	SortByRelevance(results)

	// Deduplicate by MemoryID
	seen := make(map[string]bool)
	deduped := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.MemoryID == "" {
			continue
		}
		if seen[r.MemoryID] {
			continue
		}
		seen[r.MemoryID] = true
		deduped = append(deduped, r)
		if len(deduped) >= limit {
			break
		}
	}

	return deduped
}

// SortByRelevance sorts results by RelevanceScore descending.
func SortByRelevance(results []SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].RelevanceScore > results[i].RelevanceScore {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// VectorSearcherAdapter wraps ShardManager to implement the memory.VectorSearcher interface.
// This adapter bridges the signature difference between ShardManager.Search and VectorSearcher.Search.
type VectorSearcherAdapter struct {
	manager    *ShardManager
	shardTypes []ShardType
}

// NewVectorSearcherAdapter creates a new adapter that wraps a ShardManager.
// If shardTypes is nil or empty, searches all available shard types.
func NewVectorSearcherAdapter(manager *ShardManager, shardTypes []ShardType) *VectorSearcherAdapter {
	return &VectorSearcherAdapter{
		manager:    manager,
		shardTypes: shardTypes,
	}
}

// Search implements the memory.VectorSearcher interface.
// It delegates to ShardManager.Search with the configured shard types.
func (a *VectorSearcherAdapter) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	shardTypes := a.shardTypes
	if len(shardTypes) == 0 {
		// Default to consolidated and recent shards if not specified
		shardTypes = []ShardType{ConsolidatedShard, RecentShard}
	}
	return a.manager.Search(ctx, query, limit, shardTypes)
}

// Manager returns the underlying ShardManager for advanced operations.
func (a *VectorSearcherAdapter) Manager() *ShardManager {
	return a.manager
}

// ShardTypes returns the configured shard types for search.
func (a *VectorSearcherAdapter) ShardTypes() []ShardType {
	return a.shardTypes
}
