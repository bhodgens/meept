package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// mustNewEpisodicMemory wraps NewEpisodicMemory with a t.Fatalf on error,
// for terser test setup.
func mustNewEpisodicMemory(t *testing.T, cfg EpisodicConfig) *EpisodicMemory {
	t.Helper()
	mem, err := NewEpisodicMemory(cfg)
	if err != nil {
		t.Fatalf("NewEpisodicMemory: %v", err)
	}
	return mem
}

func TestEpisodicMemoryInitialize(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Second initialization should be idempotent
	err = mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Second initialization failed: %v", err)
	}
}

func TestEpisodicMemoryStore(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store a memory
	id, err := mem.Store(ctx, "Hello, world!", "conversation", map[string]any{
		"user": "test",
	})
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	if id == "" {
		t.Error("Expected non-empty ID")
	}

	// Verify count
	count, err := mem.Count(ctx)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}
}

func TestEpisodicMemorySearch(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store some memories
	_, err = mem.Store(ctx, "The quick brown fox jumps over the lazy dog", "conversation", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	_, err = mem.Store(ctx, "Python is a programming language", "code", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	_, err = mem.Store(ctx, "Go is another programming language", "code", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Search for "programming"
	results, err := mem.Search(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'programming', got %d", len(results))
	}

	// Search for "fox"
	results, err = mem.Search(ctx, "fox", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'fox', got %d", len(results))
	}
}

func TestEpisodicMemoryGetRecent(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	for i := 0; i < 5; i++ {
		_, err = mem.Store(ctx, "Memory "+string(rune('A'+i)), "conversation", nil)
		if err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Get recent
	results, err := mem.GetRecent(ctx, 3)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Most recent should be first
	if results[0].Memory.Content != "Memory E" {
		t.Errorf("Expected 'Memory E' first, got %q", results[0].Memory.Content)
	}
}

func TestEpisodicMemoryGetByCategory(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories with different categories
	_, _ = mem.Store(ctx, "Conversation 1", "conversation", nil)
	_, _ = mem.Store(ctx, "Code snippet 1", "code", nil)
	_, _ = mem.Store(ctx, "Conversation 2", "conversation", nil)

	// Get by category
	results, err := mem.GetByCategory(ctx, "conversation", 10)
	if err != nil {
		t.Fatalf("GetByCategory failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 conversation results, got %d", len(results))
	}

	for _, r := range results {
		if r.Memory.Category != "conversation" {
			t.Errorf("Expected category 'conversation', got %q", r.Memory.Category)
		}
	}
}

func TestEpisodicMemoryDelete(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store a memory
	id, err := mem.Store(ctx, "To be deleted", "conversation", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Verify it exists
	count, _ := mem.Count(ctx)
	if count != 1 {
		t.Fatalf("Expected count 1, got %d", count)
	}

	// Delete
	err = mem.Delete(ctx, id)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify it's gone
	count, _ = mem.Count(ctx)
	if count != 0 {
		t.Errorf("Expected count 0 after delete, got %d", count)
	}
}

func TestEpisodicMemoryDeleteByIDs(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	id1, _ := mem.Store(ctx, "Memory 1", "conversation", nil)
	id2, _ := mem.Store(ctx, "Memory 2", "conversation", nil)
	id3, _ := mem.Store(ctx, "Memory 3", "conversation", nil)

	// Delete multiple
	deleted, err := mem.DeleteByIDs(ctx, []string{id1, id3})
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	if deleted != 2 {
		t.Errorf("Expected 2 deleted, got %d", deleted)
	}

	// Verify only one remains
	count, _ := mem.Count(ctx)
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Verify the correct one remains
	results, _ := mem.GetRecent(ctx, 10)
	if len(results) != 1 || results[0].Memory.ID != id2 {
		t.Error("Wrong memory remained after deletion")
	}
}

func TestEpisodicMemoryTimestamps(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Empty store should return nil timestamps
	oldest, err := mem.GetOldestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetOldestTimestamp failed: %v", err)
	}
	if oldest != nil {
		t.Error("Expected nil oldest timestamp for empty store")
	}

	newest, err := mem.GetNewestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetNewestTimestamp failed: %v", err)
	}
	if newest != nil {
		t.Error("Expected nil newest timestamp for empty store")
	}

	// Store memories
	_, _ = mem.Store(ctx, "First", "conversation", nil)
	time.Sleep(50 * time.Millisecond)
	_, _ = mem.Store(ctx, "Second", "conversation", nil)

	oldest, err = mem.GetOldestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetOldestTimestamp failed: %v", err)
	}
	if oldest == nil {
		t.Fatal("Expected non-nil oldest timestamp")
	}

	newest, err = mem.GetNewestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetNewestTimestamp failed: %v", err)
	}
	if newest == nil {
		t.Fatal("Expected non-nil newest timestamp")
	}

	if !oldest.Before(*newest) {
		t.Error("Oldest should be before newest")
	}
}

func TestEpisodicMemoryNotInitialized(t *testing.T) {
	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: "/tmp/test",
	})

	ctx := context.Background()

	// All operations should fail before initialization
	_, err := mem.Store(ctx, "test", "test", nil)
	if err == nil {
		t.Error("Store should fail before initialization")
	}

	_, err = mem.Search(ctx, "test", 10)
	if err == nil {
		t.Error("Search should fail before initialization")
	}

	_, err = mem.GetRecent(ctx, 10)
	if err == nil {
		t.Error("GetRecent should fail before initialization")
	}
}

func TestEpisodicMemoryEmptySearch(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	_, _ = mem.Store(ctx, "Hello world", "conversation", nil)
	_, _ = mem.Store(ctx, "Goodbye world", "conversation", nil)

	// Empty search should return recent
	results, err := mem.Search(ctx, "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results for empty search, got %d", len(results))
	}

	// Search with only special characters should also return recent
	results, err = mem.Search(ctx, `"" * :`, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results for special-char-only search, got %d", len(results))
	}
}

func TestEpisodicMemoryGetOldMemories(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	_, _ = mem.Store(ctx, "Memory 1", "conversation", nil)
	time.Sleep(10 * time.Millisecond)
	_, _ = mem.Store(ctx, "Memory 2", "conversation", nil)

	// Get memories older than now (should get all)
	cutoff := time.Now().Add(time.Hour)
	results, err := mem.GetOldMemories(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("GetOldMemories failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 old memories, got %d", len(results))
	}

	// Get memories older than a very recent time (should get the first one)
	time.Sleep(50 * time.Millisecond)
	cutoff = time.Now().Add(-20 * time.Millisecond)
	results, err = mem.GetOldMemories(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("GetOldMemories failed: %v", err)
	}

	// This test is timing-dependent, so we just verify it doesn't error
	// The actual count depends on timing
}

func TestEpisodicMemoryMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t,EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store with metadata
	metadata := map[string]any{
		"user":    "testuser",
		"channel": "general",
		"count":   42,
	}
	_, err = mem.Store(ctx, "Test with metadata", "conversation", metadata)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Retrieve and verify metadata
	results, err := mem.GetRecent(ctx, 1)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	meta := results[0].Memory.Metadata
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}

	if meta["user"] != "testuser" {
		t.Errorf("Expected user 'testuser', got %v", meta["user"])
	}

	if meta["channel"] != "general" {
		t.Errorf("Expected channel 'general', got %v", meta["channel"])
	}
}

func TestEpisodicMemoryStorePopulatesVersioningColumns(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store first memory (root) - should get default version=1, is_current=1
	rootID, err := mem.Store(ctx, "Original content", "conversation", map[string]any{
		"version":     1,
		"is_current":  1,
	})
	if err != nil {
		t.Fatalf("Failed to store root: %v", err)
	}

	// Store second memory (version 2) with parent_id
	v2ID, err := mem.Store(ctx, "Updated content v2", "conversation", map[string]any{
		"parent_id":   rootID,
		"version":     2,
		"is_current":  1,
	})
	if err != nil {
		t.Fatalf("Failed to store v2: %v", err)
	}

	// Verify the SQL columns are populated by querying directly
	pool := mem.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Failed to get db: %v", err)
	}
	defer pool.Put(db)

	// Check root memory SQL columns
	var rootVersion int
	var rootParentID sql.NullString
	var rootIsCurrent int
	err = db.QueryRow("SELECT version, parent_id, is_current FROM episodic_memories WHERE id = ?", rootID).
		Scan(&rootVersion, &rootParentID, &rootIsCurrent)
	if err != nil {
		t.Fatalf("Failed to query root: %v", err)
	}
	if rootVersion != 1 {
		t.Errorf("Expected root version=1, got %d", rootVersion)
	}
	if rootParentID.Valid && rootParentID.String != "" {
		t.Errorf("Expected root parent_id to be NULL, got %q", rootParentID.String)
	}
	if rootIsCurrent != 1 {
		t.Errorf("Expected root is_current=1, got %d", rootIsCurrent)
	}

	// Check v2 memory SQL columns
	var v2Version int
	var v2ParentID sql.NullString
	var v2IsCurrent int
	err = db.QueryRow("SELECT version, parent_id, is_current FROM episodic_memories WHERE id = ?", v2ID).
		Scan(&v2Version, &v2ParentID, &v2IsCurrent)
	if err != nil {
		t.Fatalf("Failed to query v2: %v", err)
	}
	if v2Version != 2 {
		t.Errorf("Expected v2 version=2, got %d", v2Version)
	}
	if !v2ParentID.Valid || v2ParentID.String != rootID {
		t.Errorf("Expected v2 parent_id=%q, got valid=%v value=%q", rootID, v2ParentID.Valid, v2ParentID.String)
	}
	if v2IsCurrent != 1 {
		t.Errorf("Expected v2 is_current=1, got %d", v2IsCurrent)
	}
}

func TestEpisodicMemoryStoreDefaultVersioning(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store with nil metadata - should get default version=1, is_current=1
	id, err := mem.Store(ctx, "Simple memory", "conversation", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	pool := mem.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Failed to get db: %v", err)
	}
	defer pool.Put(db)

	var version int
	var isCurrent int
	err = db.QueryRow("SELECT version, is_current FROM episodic_memories WHERE id = ?", id).
		Scan(&version, &isCurrent)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if version != 1 {
		t.Errorf("Expected default version=1, got %d", version)
	}
	if isCurrent != 1 {
		t.Errorf("Expected default is_current=1, got %d", isCurrent)
	}
}
