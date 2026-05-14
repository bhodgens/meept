package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func createTestSkillFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	skillDir := filepath.Join(dir, name)
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	return skillFile
}

func TestLazySkillLoader_Load(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create a test skill file
	skillContent := `---
name: test-skill
description: A test skill
requires:
  - code
tags:
  - testing
---

# Test Skill

This is the skill body.
`
	skillPath := createTestSkillFile(t, tmpDir, "test-skill", skillContent)

	// Create index with the skill
	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{
		Name:        "test-skill",
		Description: "A test skill",
		Requires:    []string{"code"},
		Tags:        []string{"testing"},
		Path:        skillPath,
	})

	// Create loader
	loader := NewLazySkillLoader(idx, WithCacheSize(10))

	// Load skill
	ctx := context.Background()
	skill, err := loader.Load(ctx, "test-skill")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("Loaded skill.Name = %q, want %q", skill.Name, "test-skill")
	}

	if skill.Body == "" {
		t.Error("Loaded skill.Body is empty")
	}

	// Check stats
	stats := loader.Stats()
	if stats.Loads != 1 {
		t.Errorf("Stats.Loads = %d, want 1", stats.Loads)
	}
	if stats.Misses != 1 {
		t.Errorf("Stats.Misses = %d, want 1", stats.Misses)
	}
}

func TestLazySkillLoader_CacheHit(t *testing.T) {
	tmpDir := t.TempDir()

	skillContent := `---
name: cached-skill
description: A cached skill
---

Body content.
`
	skillPath := createTestSkillFile(t, tmpDir, "cached-skill", skillContent)

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{
		Name: "cached-skill",
		Path: skillPath,
	})

	loader := NewLazySkillLoader(idx, WithCacheSize(10))
	ctx := context.Background()

	// First load - cache miss
	skill1, err := loader.Load(ctx, "cached-skill")
	if err != nil {
		t.Fatalf("First Load() error: %v", err)
	}

	// Second load - cache hit
	skill2, err := loader.Load(ctx, "cached-skill")
	if err != nil {
		t.Fatalf("Second Load() error: %v", err)
	}

	// Should be the same object
	if skill1 != skill2 {
		t.Error("Second Load() returned different object than first (cache miss)")
	}

	// Check stats
	stats := loader.Stats()
	if stats.Hits != 1 {
		t.Errorf("Stats.Hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Stats.Misses = %d, want 1", stats.Misses)
	}
}

func TestLazySkillLoader_CacheEviction(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple skill files
	for i := range 5 {
		name := filepath.Join("skill", string(rune('a'+i)))
		skillContent := `---
name: skill-` + string(rune('a'+i)) + `
description: Skill ` + string(rune('a'+i)) + `
---

Body.
`
		createTestSkillFile(t, tmpDir, name, skillContent)
	}

	// Create index
	idx := NewSkillIndex()
	for i := range 5 {
		name := "skill-" + string(rune('a'+i))
		skillPath := filepath.Join(tmpDir, "skill", string(rune('a'+i)), "SKILL.md")
		idx.Index(&SkillIndexEntry{
			Name: name,
			Path: skillPath,
		})
	}

	// Create loader with small cache
	loader := NewLazySkillLoader(idx, WithCacheSize(3))
	ctx := context.Background()

	// Load more skills than cache size
	for i := range 5 {
		name := "skill-" + string(rune('a'+i))
		_, err := loader.Load(ctx, name)
		if err != nil {
			t.Fatalf("Load(%s) error: %v", name, err)
		}
	}

	// Should have evicted some skills
	stats := loader.Stats()
	if stats.Evicts != 2 { // loaded 5, cache size 3, so 2 evictions
		t.Errorf("Stats.Evicts = %d, want 2", stats.Evicts)
	}

	if loader.CachedCount() != 3 {
		t.Errorf("CachedCount() = %d, want 3", loader.CachedCount())
	}
}

func TestLazySkillLoader_NotFound(t *testing.T) {
	idx := NewSkillIndex()
	loader := NewLazySkillLoader(idx)
	ctx := context.Background()

	_, err := loader.Load(ctx, "nonexistent-skill")
	if err == nil {
		t.Error("Load(nonexistent) should return error")
	}

	stats := loader.Stats()
	if stats.Errors != 1 {
		t.Errorf("Stats.Errors = %d, want 1", stats.Errors)
	}
}

func TestLazySkillLoader_Invalidate(t *testing.T) {
	tmpDir := t.TempDir()

	skillContent := `---
name: invalidate-test
description: Test invalidation
---

Body.
`
	skillPath := createTestSkillFile(t, tmpDir, "invalidate-test", skillContent)

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{
		Name: "invalidate-test",
		Path: skillPath,
	})

	loader := NewLazySkillLoader(idx)
	ctx := context.Background()

	// Load skill
	_, err := loader.Load(ctx, "invalidate-test")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !loader.IsCached("invalidate-test") {
		t.Error("Skill should be cached")
	}

	// Invalidate
	loader.Invalidate("invalidate-test")

	if loader.IsCached("invalidate-test") {
		t.Error("Skill should not be cached after invalidation")
	}
}

func TestLazySkillLoader_Clear(t *testing.T) {
	tmpDir := t.TempDir()

	for i := range 3 {
		name := "skill-" + string(rune('a'+i))
		skillContent := `---
name: ` + name + `
description: Skill
---

Body.
`
		createTestSkillFile(t, tmpDir, name, skillContent)
	}

	idx := NewSkillIndex()
	for i := range 3 {
		name := "skill-" + string(rune('a'+i))
		skillPath := filepath.Join(tmpDir, name, "SKILL.md")
		idx.Index(&SkillIndexEntry{
			Name: name,
			Path: skillPath,
		})
	}

	loader := NewLazySkillLoader(idx)
	ctx := context.Background()

	// Load all skills
	for i := range 3 {
		name := "skill-" + string(rune('a'+i))
		_, _ = loader.Load(ctx, name)
	}

	if loader.CachedCount() != 3 {
		t.Fatalf("CachedCount() = %d, want 3", loader.CachedCount())
	}

	// Clear
	loader.Clear()

	if loader.CachedCount() != 0 {
		t.Errorf("CachedCount() after Clear() = %d, want 0", loader.CachedCount())
	}
}

func TestLazySkillLoader_Get(t *testing.T) {
	tmpDir := t.TempDir()

	skillContent := `---
name: get-test
description: Test Get
---

Body.
`
	skillPath := createTestSkillFile(t, tmpDir, "get-test", skillContent)

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{
		Name: "get-test",
		Path: skillPath,
	})

	loader := NewLazySkillLoader(idx)
	ctx := context.Background()

	// Get before load should return nil
	if loader.Get("get-test") != nil {
		t.Error("Get() before Load() should return nil")
	}

	// Load
	_, _ = loader.Load(ctx, "get-test")

	// Get after load should return skill
	if loader.Get("get-test") == nil {
		t.Error("Get() after Load() should return skill")
	}
}

func TestLazySkillLoader_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	skillContent := `---
name: cancel-test
description: Test cancellation
---

Body.
`
	skillPath := createTestSkillFile(t, tmpDir, "cancel-test", skillContent)

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{
		Name: "cancel-test",
		Path: skillPath,
	})

	loader := NewLazySkillLoader(idx)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := loader.Load(ctx, "cancel-test")
	if err == nil {
		t.Error("Load() with cancelled context should return error")
	}
}
