package memory

import (
	"context"
	"fmt"
	"log/slog"
)

// ScopedMemoryManager wraps a Manager and scopes all memory operations
// to a specific botID. Stores are tagged with the bot_id, and read/query
// operations filter results to only return memories belonging to that bot.
type ScopedMemoryManager struct {
	manager *Manager
	botID   string
	logger  *slog.Logger
}

// BotID returns the bot ID this scoped manager is bound to.
func (s *ScopedMemoryManager) BotID() string {
	return s.botID
}

// Store persists a memory tagged with the bot ID.
// The bot_id is injected into the Memory struct so downstream storage
// backends include it in metadata.
func (s *ScopedMemoryManager) Store(ctx context.Context, mem Memory) (string, error) {
	mem.BotID = s.botID
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]any)
	}
	mem.Metadata["bot_id"] = s.botID

	if s.logger != nil {
		s.logger.Debug("scoped store", "type", mem.Type, "category", mem.Category)
	}
	return s.manager.Store(ctx, mem)
}

// Search finds memories matching the query, filtering to only those
// belonging to this bot.
// It fetches a larger batch from the underlying store so that filtering
// by bot_id does not silently truncate results (e.g. requesting limit=10
// but all 10 rows belong to a different bot would previously return
// an empty slice with no indication).
func (s *ScopedMemoryManager) Search(ctx context.Context, query MemoryQuery) ([]MemoryResult, error) {
	expandedQuery := query
	if expandedQuery.Limit < 100 {
		expandedQuery.Limit = expandedQuery.Limit * 5
	}
	results, err := s.manager.Search(ctx, expandedQuery)
	if err != nil {
		return nil, err
	}
	filtered := s.filterResults(results)
	if len(filtered) > query.Limit {
		filtered = filtered[:query.Limit]
	}
	return filtered, nil
}

// GetRecent retrieves the most recent memories belonging to this bot.
func (s *ScopedMemoryManager) GetRecent(ctx context.Context, limit int) ([]MemoryResult, error) {
	expandedLimit := limit * 5
	if expandedLimit < limit+5 {
		expandedLimit = limit + 5
	}
	results, err := s.manager.GetRecent(ctx, expandedLimit)
	if err != nil {
		return nil, err
	}
	filtered := s.filterResults(results)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// GetRelevantContext retrieves memories relevant to a query, scoped to this bot.
func (s *ScopedMemoryManager) GetRelevantContext(ctx context.Context, query string, maxItems int) ([]MemoryResult, error) {
	expandedMax := maxItems * 5
	if expandedMax < maxItems+5 {
		expandedMax = maxItems + 5
	}
	results, err := s.manager.GetRelevantContext(ctx, query, expandedMax)
	if err != nil {
		return nil, err
	}
	filtered := s.filterResults(results)
	if len(filtered) > maxItems {
		filtered = filtered[:maxItems]
	}
	return filtered, nil
}

// GetByID retrieves a memory by ID. It returns the memory only if it
// belongs to this bot; otherwise returns ErrNotFound.
func (s *ScopedMemoryManager) GetByID(ctx context.Context, id string) (*Memory, error) {
	mem, err := s.manager.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.ownsMemory(mem) {
		return nil, ErrNotFound
	}
	return mem, nil
}

// GetStats returns aggregate statistics scoped to this bot.
// Because the underlying backends do not natively filter by bot_id at the
// count level, this performs a best-effort estimate by fetching recent
// memories and counting. For exact counts, use Search or GetRecent.
func (s *ScopedMemoryManager) GetStats(ctx context.Context) (*MemoryStats, error) {
	// Fetch a large batch and count by type.
	results, err := s.manager.Search(ctx, MemoryQuery{
		Query: "bot_id:" + s.botID,
		Limit: 10000,
	})
	if err != nil {
		return nil, fmt.Errorf("scoped stats: %w", err)
	}

	stats := &MemoryStats{}
	for _, r := range results {
		if s.ownsMemory(&r.Memory) {
			stats.TotalCount++
			switch r.Memory.Type {
			case MemoryTypeEpisodic:
				stats.EpisodicCount++
			case MemoryTypeTask:
				stats.TaskCount++
			}
		}
	}
	return stats, nil
}

// Delete removes a memory by ID, but only if it belongs to this bot.
func (s *ScopedMemoryManager) Delete(ctx context.Context, id string) error {
	// Verify ownership before deletion.
	mem, err := s.manager.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !s.ownsMemory(mem) {
		return ErrNotFound
	}
	return s.manager.Delete(ctx, id)
}

// SearchSemantic performs vector similarity search, scoped to this bot.
func (s *ScopedMemoryManager) SearchSemantic(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	expandedLimit := limit * 5
	if expandedLimit < limit+5 {
		expandedLimit = limit + 5
	}
	results, err := s.manager.SearchSemantic(ctx, query, expandedLimit)
	if err != nil {
		return nil, err
	}
	filtered := s.filterResults(results)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// SearchHybrid performs hybrid search, scoped to this bot.
func (s *ScopedMemoryManager) SearchHybrid(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	expandedLimit := limit * 5
	if expandedLimit < limit+5 {
		expandedLimit = limit + 5
	}
	results, err := s.manager.SearchHybrid(ctx, query, expandedLimit)
	if err != nil {
		return nil, err
	}
	filtered := s.filterResults(results)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// SearchWithGraph performs graph-aware search, scoped to this bot.
func (s *ScopedMemoryManager) SearchWithGraph(ctx context.Context, query MemoryQuery, alpha float64) ([]MemoryResult, error) {
	expandedQuery := query
	if expandedQuery.Limit < 100 {
		expandedQuery.Limit = expandedQuery.Limit * 5
	}
	results, err := s.manager.SearchWithGraph(ctx, expandedQuery, alpha)
	if err != nil {
		return nil, err
	}
	filtered := s.filterResults(results)
	if len(filtered) > query.Limit {
		filtered = filtered[:query.Limit]
	}
	return filtered, nil
}

// IsInitialized returns true if the underlying manager is initialized.
func (s *ScopedMemoryManager) IsInitialized() bool {
	return s.manager.IsInitialized()
}

// Manager returns the underlying unscoped Manager.
func (s *ScopedMemoryManager) Manager() *Manager {
	return s.manager
}

// filterResults filters a slice of MemoryResult to only include memories
// belonging to the scoped bot ID. It checks both the BotID field and the
// bot_id metadata key for compatibility with memories stored before the
// BotID struct field was wired in.
func (s *ScopedMemoryManager) filterResults(results []MemoryResult) []MemoryResult {
	if len(results) == 0 {
		return results
	}
	filtered := make([]MemoryResult, 0, len(results))
	for _, r := range results {
		if s.ownsMemory(&r.Memory) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// ownsMemory returns true if the given memory belongs to the scoped bot ID.
// It checks both the BotID struct field and the "bot_id" metadata key,
// since the SQLite backend stores attribution only in metadata JSON.
func (s *ScopedMemoryManager) ownsMemory(mem *Memory) bool {
	if mem.BotID == s.botID {
		return true
	}
	if mem.Metadata != nil {
		if botID, ok := mem.Metadata["bot_id"].(string); ok && botID == s.botID {
			return true
		}
	}
	return false
}
