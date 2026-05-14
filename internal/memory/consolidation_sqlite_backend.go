package memory

import (
	"context"
	"fmt"
	"time"
)

// SQLiteConsolidationBackend implements ConsolidationBackend using SQLite stores.
type SQLiteConsolidationBackend struct {
	episodic *EpisodicMemory
	task     *TaskMemory
	manager  *Manager // For GetExpiredMemories
}

// NewSQLiteConsolidationBackend creates a backend wrapping SQLite stores.
func NewSQLiteConsolidationBackend(episodic *EpisodicMemory, task *TaskMemory, manager *Manager) *SQLiteConsolidationBackend {
	return &SQLiteConsolidationBackend{
		episodic: episodic,
		task:     task,
		manager:  manager,
	}
}

func (b *SQLiteConsolidationBackend) GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
	if b.episodic == nil {
		return nil, fmt.Errorf("episodic memory not available")
	}
	return b.episodic.GetOldMemories(ctx, olderThan, limit)
}

func (b *SQLiteConsolidationBackend) GetExpiredMemories(ctx context.Context, notAccessedSinceDays int) ([]Memory, error) {
	return b.manager.GetExpiredMemories(ctx, notAccessedSinceDays)
}

func (b *SQLiteConsolidationBackend) StoreSummary(ctx context.Context, content, category string, metadata map[string]any) (string, error) {
	if b.episodic == nil {
		return "", fmt.Errorf("episodic memory not available")
	}
	return b.episodic.Store(ctx, content, category, metadata)
}

func (b *SQLiteConsolidationBackend) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	if b.episodic == nil {
		return 0, fmt.Errorf("episodic memory not available")
	}
	return b.episodic.DeleteByIDs(ctx, ids)
}

func (b *SQLiteConsolidationBackend) FindDuplicates(ctx context.Context, threshold int) ([][]string, error) {
	if b.task == nil {
		return nil, fmt.Errorf("task memory not available")
	}
	return b.task.FindDuplicates(ctx, threshold)
}

func (b *SQLiteConsolidationBackend) StoreExpiredSummary(ctx context.Context, mem Memory, category string) (string, error) {
	summaryContent := fmt.Sprintf("Summary of expired memory: %s", mem.Content)
	if len(summaryContent) > 500 {
		summaryContent = summaryContent[:500] + "..."
	}

	summary := Memory{
		Content:   summaryContent,
		Type:      mem.Type,
		Category:  category,
		Metadata:  mem.Metadata,
		CreatedAt: time.Now(),
	}
	return b.manager.Store(ctx, summary)
}

func (b *SQLiteConsolidationBackend) DeleteSingle(ctx context.Context, id string) error {
	return b.manager.Delete(ctx, id)
}
