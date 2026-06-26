package memory

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func setupDualStoreRead(t *testing.T) *DualStore {
	t.Helper()
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-a", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	t.Cleanup(func() { ds.Close() })
	return ds
}

func TestGetMemoriesEmpty(t *testing.T) {
	ds := setupDualStoreRead(t)
	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty store, got %d results", len(results))
	}
}

func TestGetMemoriesLocalOnly(t *testing.T) {
	ds := setupDualStoreRead(t)

	for i := 0; i < 3; i++ {
		mem := &Memory{
			ID:        fmt.Sprintf("local-read-%d", i),
			Type:      MemoryTypeEpisodic,
			Category:  "read-test",
			Content:   fmt.Sprintf("local read memory %d", i),
			CreatedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}
		if err := ds.StoreMemory(context.Background(), mem); err != nil {
			t.Fatalf("StoreMemory: %v", err)
		}
	}

	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	// Results are ordered created_at DESC, so IDs appear in reverse.
	for i, r := range results {
		want := fmt.Sprintf("local-read-%d", 2-i)
		if r.Memory.ID != want {
			t.Errorf("result[%d].ID = %q, want %q (DESC order, i=%d)", i, r.Memory.ID, want, i)
		}
	}
}

func TestGetMemoriesGossipOnly(t *testing.T) {
	ds := setupDualStoreRead(t)

	for i := 0; i < 2; i++ {
		mem := &Memory{
			ID:        fmt.Sprintf("gossip-read-%d", i),
			Type:      MemoryTypeTask,
			Category:  "read-test",
			Content:   fmt.Sprintf("gossip read memory %d", i),
			CreatedAt: time.Date(2026, 2, i+1, 0, 0, 0, 0, time.UTC),
			Metadata:  map[string]any{"source_node": "node-b"},
		}
		if err := ds.StoreRemoteMemory(context.Background(), mem, "node-b"); err != nil {
			t.Fatalf("StoreRemoteMemory: %v", err)
		}
	}

	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// Results are ordered created_at DESC, so IDs appear in reverse.
	for i, r := range results {
		want := fmt.Sprintf("gossip-read-%d", len(results)-1-i)
		if r.Memory.ID != want {
			t.Errorf("result[%d].ID = %q, want %q (DESC order)", i, r.Memory.ID, want)
		}
		if src, ok := r.Memory.Metadata["source_node"]; !ok || src != "node-b" {
			t.Errorf("result[%d] missing source_node metadata", i)
		}
	}
}

func TestGetMemoriesMergedLocalFirst(t *testing.T) {
	ds := setupDualStoreRead(t)

	// 2 local memories.
	for i := 0; i < 2; i++ {
		mem := &Memory{
			ID:        fmt.Sprintf("merged-local-%d", i),
			Type:      MemoryTypeEpisodic,
			Category:  "read-test",
			Content:   fmt.Sprintf("merged local %d", i),
			CreatedAt: time.Date(2026, 3, i+1, 0, 0, 0, 0, time.UTC),
		}
		if err := ds.StoreMemory(context.Background(), mem); err != nil {
			t.Fatalf("StoreMemory: %v", err)
		}
	}

	// 2 gossip memories (same category to ensure merge).
	for i := 0; i < 2; i++ {
		mem := &Memory{
			ID:        fmt.Sprintf("merged-gossip-%d", i),
			Type:      MemoryTypeEpisodic,
			Category:  "read-test",
			Content:   fmt.Sprintf("merged gossip %d", i),
			CreatedAt: time.Date(2026, 3, i+1, 0, 0, 0, 0, time.UTC),
			Metadata:  map[string]any{"source_node": "node-c"},
		}
		if err := ds.StoreRemoteMemory(context.Background(), mem, "node-c"); err != nil {
			t.Fatalf("StoreRemoteMemory: %v", err)
		}
	}

	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("got %d merged results, want 4", len(results))
	}

	// First 2 should be local (most recent first).
	for i := 0; i < 2; i++ {
		wantID := fmt.Sprintf("merged-local-%d", 1-i)
		if results[i].Memory.ID != wantID {
			t.Errorf("result[%d].ID = %q, want %q (local first, DESC order)", i, results[i].Memory.ID, wantID)
		}
	}

	// Last 2 should be gossip (most recent first).
	for i := 0; i < 2; i++ {
		wantID := fmt.Sprintf("merged-gossip-%d", 1-i)
		if results[2+i].Memory.ID != wantID {
			t.Errorf("result[2+%d].ID = %q, want %q (gossip after local, DESC order)", i, results[2+i].Memory.ID, wantID)
		}
	}
}

func TestGetMemoriesTypeFilter(t *testing.T) {
	ds := setupDualStoreRead(t)

	// Store episodic and task memories.
	memE := &Memory{
		ID:        "filter-ep",
		Type:      MemoryTypeEpisodic,
		Category:  "read-test",
		Content:   "episodic filter test",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := ds.StoreMemory(context.Background(), memE); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	memT := &Memory{
		ID:        "filter-t",
		Type:      MemoryTypeTask,
		Category:  "code",
		Content:   "task filter test",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := ds.StoreMemory(context.Background(), memT); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	// Filter by episodic.
	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Type: MemoryTypeEpisodic, Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Type filter: got %d results, want 1", len(results))
	}
	if results[0].Memory.Type != MemoryTypeEpisodic {
		t.Errorf("Type filter: got type %q, want %q", results[0].Memory.Type, MemoryTypeEpisodic)
	}
}

func TestGetMemoriesDedupLocalWins(t *testing.T) {
	ds := setupDualStoreRead(t)

	// Same memory ID in both local and gossip.
	memLocal := &Memory{
		ID:        "dedup-shared",
		Type:      MemoryTypeEpisodic,
		Category:  "read-test",
		Content:   "local winner",
		CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := ds.StoreMemory(context.Background(), memLocal); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	memGossip := &Memory{
		ID:        "dedup-shared",
		Type:      MemoryTypeEpisodic,
		Category:  "read-test",
		Content:   "gossip loser",
		CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Metadata:  map[string]any{"source_node": "node-d"},
	}
	if err := ds.StoreRemoteMemory(context.Background(), memGossip, "node-d"); err != nil {
		t.Fatalf("StoreRemoteMemory: %v", err)
	}

	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}

	// Should only have 1 result (deduped).
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 after dedup", len(results))
	}
	if results[0].Memory.Content != "local winner" {
		t.Errorf("content = %q, want %q (local wins)", results[0].Memory.Content, "local winner")
	}
}

func TestGetMemoryCountByOwner(t *testing.T) {
	ds := setupDualStoreRead(t)

	// 3 local memories.
	for i := 0; i < 3; i++ {
		_ = ds.StoreMemory(context.Background(), &Memory{
			ID:        fmt.Sprintf("count-local-%d", i),
			Type:      MemoryTypeEpisodic,
			Category:  "r",
			Content:   "count test",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		})
	}

	// 2 gossip memories.
	for i := 0; i < 2; i++ {
		_ = ds.StoreRemoteMemory(context.Background(), &Memory{
			ID:        fmt.Sprintf("count-gossip-%d", i),
			Type:      MemoryTypeEpisodic,
			Category:  "r",
			Content:   "count test",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}, "peer")
	}

	local, gossip, err := ds.GetMemoryCountByOwner(context.Background())
	if err != nil {
		t.Fatalf("GetMemoryCountByOwner: %v", err)
	}
	if local != 3 {
		t.Errorf("local count = %d, want 3", local)
	}
	if gossip != 2 {
		t.Errorf("gossip count = %d, want 2", gossip)
	}
}
