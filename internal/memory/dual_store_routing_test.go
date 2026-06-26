package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func setupDualStoreRouting(t *testing.T) *DualStore {
	t.Helper()
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-a", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	t.Cleanup(func() { ds.Close() })
	return ds
}

func countMemoriesDB(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	if err != nil {
		t.Fatalf("countMemories: %v", err)
	}
	return count
}

func TestStoreMemoryLocal(t *testing.T) {
	ds := setupDualStoreRouting(t)
	mem := &Memory{
		ID:        "mem-local-1",
		Type:      MemoryTypeEpisodic,
		Category:  "test",
		Content:   "local memory content",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := ds.StoreMemory(context.Background(), mem); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	if countMemoriesDB(t, ds.localDB) != 1 {
		t.Errorf("local memory count = %d, want 1", countMemoriesDB(t, ds.localDB))
	}
	if countMemoriesDB(t, ds.gossipDB) != 0 {
		t.Errorf("gossip memory count = %d, want 0", countMemoriesDB(t, ds.gossipDB))
	}
}

func TestStoreMemoryGossip(t *testing.T) {
	ds := setupDualStoreRouting(t)
	mem := &Memory{
		ID:        "mem-gossip-1",
		Type:      MemoryTypeEpisodic,
		Category:  "test",
		Content:   "remote memory content",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Metadata:  map[string]any{"source_node": "node-b"},
	}

	if err := ds.StoreMemory(context.Background(), mem); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	if countMemoriesDB(t, ds.gossipDB) != 1 {
		t.Errorf("gossip memory count = %d, want 1", countMemoriesDB(t, ds.gossipDB))
	}
}

func TestStoreRemoteMemory(t *testing.T) {
	ds := setupDualStoreRouting(t)
	mem := &Memory{
		ID:        "mem-remote-1",
		Type:      MemoryTypeTask,
		Category:  "code",
		Content:   "remote task memory",
		CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := ds.StoreRemoteMemory(context.Background(), mem, "node-c"); err != nil {
		t.Fatalf("StoreRemoteMemory: %v", err)
	}

	if countMemoriesDB(t, ds.gossipDB) != 1 {
		t.Errorf("gossip memory count = %d, want 1", countMemoriesDB(t, ds.gossipDB))
	}

	var sourceNode string
	if err := ds.gossipDB.QueryRow(
		"SELECT source_node FROM memories WHERE id='mem-remote-1'",
	).Scan(&sourceNode); err != nil {
		t.Fatalf("query source_node: %v", err)
	}
	if sourceNode != "node-c" {
		t.Errorf("source_node = %q, want %q", sourceNode, "node-c")
	}
}

func TestStoreRemoteMemoryRejectsEmptySource(t *testing.T) {
	ds := setupDualStoreRouting(t)
	mem := &Memory{ID: "x"}
	err := ds.StoreRemoteMemory(context.Background(), mem, "")
	if err == nil {
		t.Error("expected error for empty sourceNode")
	}
}

func TestStoreMemoryOwnNodeWritesLocal(t *testing.T) {
	ds := setupDualStoreRouting(t)
	mem := &Memory{
		ID:        "mem-own-node",
		Type:      MemoryTypeEpisodic,
		Category:  "test",
		Content:   "own node",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Metadata:  map[string]any{"source_node": "node-a"},
	}

	if err := ds.StoreMemory(context.Background(), mem); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	if countMemoriesDB(t, ds.localDB) != 1 {
		t.Errorf("local count = %d, want 1 (self-node writes to local)", countMemoriesDB(t, ds.localDB))
	}
}

func TestConcurrentWriteRouting(t *testing.T) {
	ds := setupDualStoreRouting(t)
	ctx := context.Background()
	done := make(chan struct{}, 20)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			id := fmt.Sprintf("mem-local-conc-%d", idx)
			mem := &Memory{
				ID:        id,
				Type:      MemoryTypeEpisodic,
				Category:  "conc",
				Content:   "local concurrent",
				CreatedAt: time.Now().UTC(),
			}
			_ = ds.StoreMemory(ctx, mem)
		}(i)
	}
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			id := fmt.Sprintf("mem-gossip-conc-%d", idx)
			mem := &Memory{
				ID:        id,
				Type:      MemoryTypeEpisodic,
				Category:  "conc",
				Content:   "gossip concurrent",
				CreatedAt: time.Now().UTC(),
				Metadata:  map[string]any{"source_node": "peer-x"},
			}
			_ = ds.StoreMemory(ctx, mem)
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}

	localCount := countMemoriesDB(t, ds.localDB)
	gossipCount := countMemoriesDB(t, ds.gossipDB)
	if localCount != 10 {
		t.Errorf("local count = %d, want 10", localCount)
	}
	if gossipCount != 10 {
		t.Errorf("gossip count = %d, want 10", gossipCount)
	}
}

// TestWriteLockNoDeadlock verifies that the mutex doesn't deadlock under
// rapid concurrent access to both DBs.
func TestWriteLockNoDeadlock(t *testing.T) {
	ds := setupDualStoreRouting(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = ds.StoreMemory(ctx, &Memory{
					ID:        fmt.Sprintf("lock-test-%d-%d", i, j),
					Type:      MemoryTypeEpisodic,
					Category:  "lock",
					Content:   "locking test",
					CreatedAt: time.Now().UTC(),
				})
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = ds.StoreRemoteMemory(ctx, &Memory{
					ID:        fmt.Sprintf("lock-remote-%d-%d", i, j),
					Type:      MemoryTypeEpisodic,
					Category:  "lock",
					Content:   "locking remote",
					CreatedAt: time.Now().UTC(),
				}, "peer")
			}
		}()
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: writes did not complete within 10s")
	}
}
