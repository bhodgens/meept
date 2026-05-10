package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/memory/memvid"
)

// MemvidConsolidationBackend implements ConsolidationBackend using the memvid service.
// Operations that memvid does not support return errors; the Consolidator handles
// these gracefully by skipping those phases.
type MemvidConsolidationBackend struct {
	client *memvid.Client
	zone   string
}

// NewMemvidConsolidationBackend creates a consolidation backend for the memvid service.
func NewMemvidConsolidationBackend(client *memvid.Client, zone string) *MemvidConsolidationBackend {
	return &MemvidConsolidationBackend{client: client, zone: zone}
}

func (b *MemvidConsolidationBackend) GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
	// Memvid doesn't have time-range queries. Use broad search and filter client-side.
	results, err := b.client.WithZone(b.zone).Search(ctx, "", limit)
	if err != nil {
		return nil, fmt.Errorf("memvid search failed: %w", err)
	}

	var filtered []MemoryResult
	for _, r := range results {
		if r.Memory.CreatedAt.Before(olderThan) {
			category, _ := r.Memory.Metadata["category"].(string)
			filtered = append(filtered, MemoryResult{
				Memory: Memory{
					ID:        r.Memory.ID,
					Content:   r.Memory.Content,
					Category:  category,
					CreatedAt: r.Memory.CreatedAt,
					Metadata:  r.Memory.Metadata,
				},
				RelevanceScore: r.RelevanceScore,
				Source:         fmt.Sprintf("memvid:%s", b.zone),
			})
		}
	}
	return filtered, nil
}

func (b *MemvidConsolidationBackend) GetExpiredMemories(_ context.Context, _ int) ([]Memory, error) {
	// Memvid doesn't track last_accessed_at
	return nil, fmt.Errorf("access-based expiration not supported by memvid backend")
}

func (b *MemvidConsolidationBackend) StoreSummary(ctx context.Context, content string, category string, metadata map[string]any) (string, error) {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["category"] = category
	metadata["type"] = "consolidation_summary"
	return b.client.WithZone(b.zone).Store(ctx, content, metadata)
}

func (b *MemvidConsolidationBackend) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	var deleted int
	for _, id := range ids {
		if err := b.client.Delete(ctx, id); err != nil {
			continue // Best-effort deletion
		}
		deleted++
	}
	return deleted, nil
}

func (b *MemvidConsolidationBackend) FindDuplicates(_ context.Context, _ int) ([][]string, error) {
	// Memvid doesn't have FTS5-style similarity search
	return nil, fmt.Errorf("duplicate finding not supported by memvid backend")
}

func (b *MemvidConsolidationBackend) StoreExpiredSummary(ctx context.Context, mem Memory, category string) (string, error) {
	summaryContent := fmt.Sprintf("Summary of expired memory: %s", mem.Content)
	if len(summaryContent) > 500 {
		summaryContent = summaryContent[:500] + "..."
	}

	metadata := make(map[string]any)
	for k, v := range mem.Metadata {
		metadata[k] = v
	}
	metadata["type"] = "expired_summary"
	metadata["original_id"] = mem.ID
	metadata["category"] = category

	return b.client.WithZone(b.zone).Store(ctx, summaryContent, metadata)
}

func (b *MemvidConsolidationBackend) DeleteSingle(ctx context.Context, id string) error {
	return b.client.Delete(ctx, id)
}
