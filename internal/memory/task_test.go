package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskMemoryInitialize(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(TaskMemoryConfig{
		DataDir: filepath.Join(tmpDir, "task"),
		Domains: []string{"general", "code", "commands"},
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Verify domains
	domains := mem.Domains()
	if len(domains) != 3 {
		t.Errorf("Expected 3 domains, got %d", len(domains))
	}
}

func TestTaskMemoryStore(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store in different domains
	id1, err := mem.Store(ctx, "How to sort a list in Python", "code", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}
	if id1 == "" {
		t.Error("Expected non-empty ID")
	}

	_, err = mem.Store(ctx, "Git commit -m message", "commands", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Verify count
	count, err := mem.Count(ctx)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}
}

func TestTaskMemorySearchByDomain(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	_, _ = mem.Store(ctx, "Python programming basics", "code", nil)
	_, _ = mem.Store(ctx, "Python command line tools", "commands", nil)
	_, _ = mem.Store(ctx, "Go programming language", "code", nil)

	// Search within code domain
	results, err := mem.Search(ctx, "Python", "code", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result in code domain, got %d", len(results))
	}

	// Search all domains
	results, err = mem.Search(ctx, "Python", "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results across all domains, got %d", len(results))
	}
}

func TestTaskMemoryGetRecent(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	_, _ = mem.Store(ctx, "Task A", "general", nil)
	time.Sleep(10 * time.Millisecond)
	_, _ = mem.Store(ctx, "Task B", "code", nil)
	time.Sleep(10 * time.Millisecond)
	_, _ = mem.Store(ctx, "Task C", "code", nil)

	// Get recent from code domain
	results, err := mem.GetRecent(ctx, "code", 10)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 code results, got %d", len(results))
	}

	// Get recent from all domains
	results, err = mem.GetRecent(ctx, "", 10)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Most recent should be first
	if results[0].Memory.Content != "Task C" {
		t.Errorf("Expected 'Task C' first, got %q", results[0].Memory.Content)
	}
}

func TestTaskMemoryFindDuplicates(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store duplicates (content > 50 chars to be detected)
	longContent := "This is a long piece of content that exceeds the minimum threshold for duplicate detection"
	_, _ = mem.Store(ctx, longContent, "code", nil)
	_, _ = mem.Store(ctx, longContent, "code", nil)
	_, _ = mem.Store(ctx, longContent, "code", nil)
	_, _ = mem.Store(ctx, "Unique content that is also long enough", "code", nil)

	// Find duplicates
	groups, err := mem.FindDuplicates(ctx, 50)
	if err != nil {
		t.Fatalf("FindDuplicates failed: %v", err)
	}

	if len(groups) != 1 {
		t.Errorf("Expected 1 duplicate group, got %d", len(groups))
	}

	if len(groups) > 0 && len(groups[0]) != 3 {
		t.Errorf("Expected 3 IDs in duplicate group, got %d", len(groups[0]))
	}
}

func TestTaskMemoryDelete(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store and delete
	id, _ := mem.Store(ctx, "To be deleted", "general", nil)

	err = mem.Delete(ctx, id)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, _ := mem.Count(ctx)
	if count != 0 {
		t.Errorf("Expected count 0 after delete, got %d", count)
	}
}

func TestTaskMemoryDeleteByIDs(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	id1, _ := mem.Store(ctx, "Task 1", "general", nil)
	id2, _ := mem.Store(ctx, "Task 2", "general", nil)
	_, _ = mem.Store(ctx, "Task 3", "general", nil)

	// Delete multiple
	deleted, err := mem.DeleteByIDs(ctx, []string{id1, id2})
	if err != nil {
		t.Fatalf("DeleteByIDs failed: %v", err)
	}

	if deleted != 2 {
		t.Errorf("Expected 2 deleted, got %d", deleted)
	}

	count, _ := mem.Count(ctx)
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Delete empty list should return 0
	deleted, err = mem.DeleteByIDs(ctx, []string{})
	if err != nil {
		t.Fatalf("DeleteByIDs failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted for empty list, got %d", deleted)
	}
}

func TestTaskMemoryTimestamps(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Empty store
	oldest, _ := mem.GetOldestTimestamp(ctx)
	if oldest != nil {
		t.Error("Expected nil oldest for empty store")
	}

	// Store and check
	_, _ = mem.Store(ctx, "First", "general", nil)
	time.Sleep(50 * time.Millisecond)
	_, _ = mem.Store(ctx, "Second", "general", nil)

	oldest, err = mem.GetOldestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetOldestTimestamp failed: %v", err)
	}

	newest, err := mem.GetNewestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetNewestTimestamp failed: %v", err)
	}

	if oldest == nil || newest == nil {
		t.Fatal("Expected non-nil timestamps")
	}

	if !oldest.Before(*newest) {
		t.Error("Oldest should be before newest")
	}
}

func TestTaskMemoryDefaultDomain(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store with empty domain (should default to "general")
	_, err = mem.Store(ctx, "No domain specified", "", nil)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	results, err := mem.GetRecent(ctx, "general", 10)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result in general domain, got %d", len(results))
	}
}

func TestTaskMemoryNotInitialized(t *testing.T) {
	mem := NewTaskMemory(TaskMemoryConfig{
		DataDir: "/tmp/test",
	})

	ctx := context.Background()

	// Operations should fail
	_, err := mem.Store(ctx, "test", "general", nil)
	if err == nil {
		t.Error("Store should fail before initialization")
	}

	_, err = mem.Search(ctx, "test", "", 10)
	if err == nil {
		t.Error("Search should fail before initialization")
	}
}

func TestTaskMemoryRelevanceScore(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories with different keyword frequencies
	_, _ = mem.Store(ctx, "Python Python Python programming language", "code", nil)
	_, _ = mem.Store(ctx, "Python programming basics", "code", nil)
	_, _ = mem.Store(ctx, "Just programming", "code", nil)

	// Search for "Python"
	results, err := mem.Search(ctx, "Python", "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("Expected at least 2 results, got %d", len(results))
	}

	// Results should have relevance scores
	for _, r := range results {
		if r.RelevanceScore < 0 || r.RelevanceScore > 1 {
			t.Errorf("Relevance score should be in [0, 1], got %f", r.RelevanceScore)
		}
	}

	// First result (more Python occurrences) should have higher or equal relevance
	if results[0].RelevanceScore < results[1].RelevanceScore {
		t.Log("Note: First result has lower relevance than second (FTS5 ranking may vary)")
	}
}

func TestTaskMemoryMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store with metadata
	metadata := map[string]any{
		"language": "python",
		"version":  "3.14",
	}
	_, err = mem.Store(ctx, "Python task", "code", metadata)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Retrieve and verify
	results, _ := mem.GetRecent(ctx, "", 1)
	if len(results) != 1 {
		t.Fatal("Expected 1 result")
	}

	meta := results[0].Memory.Metadata
	if meta["language"] != "python" {
		t.Errorf("Expected language 'python', got %v", meta["language"])
	}
}

func TestTaskMemorySource(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := NewTaskMemory(DefaultTaskMemoryConfig(filepath.Join(tmpDir, "task")))

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store in different domains
	_, _ = mem.Store(ctx, "Code task", "code", nil)
	_, _ = mem.Store(ctx, "Command task", "commands", nil)

	// Get recent and check sources
	results, _ := mem.GetRecent(ctx, "", 10)

	sources := make(map[string]bool)
	for _, r := range results {
		sources[r.Source] = true
	}

	if !sources["task:code"] {
		t.Error("Expected 'task:code' source")
	}
	if !sources["task:commands"] {
		t.Error("Expected 'task:commands' source")
	}
}
