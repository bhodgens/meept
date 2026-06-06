package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSource_Name(t *testing.T) {
	src := NewClaudeSource(nil)
	if got := src.Name(); got != "claude" {
		t.Errorf("ClaudeSource.Name() = %q, want %q", got, "claude")
	}
}

func TestClaudeSource_Discover_NonexistentDirectory(t *testing.T) {
	src := NewClaudeSourceWithPath("/nonexistent/claude/skills", nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover should not fail for nonexistent dir: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}

func TestClaudeSource_Discover_FlatAndDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-discover-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create flat skill
	flatContent := `---
name: claude-flat
description: A flat Claude skill
trigger: graphify
---
Instructions for graphify.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "claude-flat.md"), []byte(flatContent), 0o644); err != nil {
		t.Fatalf("Failed to write flat skill: %v", err)
	}

	// Create directory skill
	dirSkill := filepath.Join(tmpDir, "claude-dir")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(dirSkill, 0o755); err != nil {
		t.Fatalf("Failed to create skill subdir: %v", err)
	}

	dirContent := `---
name: claude-dir
description: A directory Claude skill
---
Instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(dirSkill, "SKILL.md"), []byte(dirContent), 0o644); err != nil {
		t.Fatalf("Failed to write dir skill: %v", err)
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("Expected 2 skills, got %d", len(skills))
	}

	// Check that the flat skill was adapted (trigger -> tags)
	foundFlat := false
	for _, s := range skills {
		if s.Name == "claude-flat" {
			foundFlat = true
			if s.Priority != PriorityClaude {
				t.Errorf("Priority = %d, want %d", s.Priority, PriorityClaude)
			}
			// Trigger should be mapped to Tags by the parser
			hasGraphify := false
			for _, tag := range s.Tags {
				if tag == "graphify" {
					hasGraphify = true
					break
				}
			}
			if !hasGraphify {
				t.Error("Expected 'graphify' tag from trigger field")
			}
		}
	}
	if !foundFlat {
		t.Error("Missing claude-flat skill")
	}
}

func TestClaudeSource_Discover_AdapterApplied(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-adapter-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillContent := `---
name: test-adapter
description: Test skill for adapter
---
Body content.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "test-adapter.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	if skills[0].Name != "test-adapter" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "test-adapter")
	}

	// The adapter should have been applied (currently a no-op, but the path is exercised)
	if skills[0].Priority != PriorityClaude {
		t.Errorf("Priority = %d, want %d (ClaudeSource should set Claude priority)", skills[0].Priority, PriorityClaude)
	}
}

func TestClaudeSource_Discover_ExcludesReadme(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-readme-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	readme := `---
name: readme-skill
description: Should be excluded
---
Not a real skill.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	realSkill := `---
name: claude-real
description: A real Claude skill
---
Instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "claude-real.md"), []byte(realSkill), 0o644); err != nil {
		t.Fatalf("Failed to write real skill: %v", err)
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("Expected 1 skill (README excluded), got %d", len(skills))
	}
	if skills[0].Name != "claude-real" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "claude-real")
	}
}

func TestClaudeSource_Discover_Sorted(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-sort-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, name := range []string{"zebra", "alpha", "mike"} {
		content := `---
name: ` + name + `
description: Test
---
Body.
`
		//nolint:gosec // test directory/file
		if err := os.WriteFile(filepath.Join(tmpDir, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write skill: %v", err)
		}
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 3 {
		t.Fatalf("Expected 3 skills, got %d", len(skills))
	}

	// Should be sorted by name
	if skills[0].Name != "alpha" || skills[1].Name != "mike" || skills[2].Name != "zebra" {
		t.Errorf("Skills not sorted: got %v", []string{skills[0].Name, skills[1].Name, skills[2].Name})
	}
}

func TestClaudeSource_NilLogger(t *testing.T) {
	src := NewClaudeSource(nil)
	if src == nil {
		t.Fatal("NewClaudeSource returned nil")
	}
	if src.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", src.Name(), "claude")
	}
}

func TestClaudeSource_Discover_PathIsFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "claudesource-file-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	src := NewClaudeSourceWithPath(tmpFile.Name(), nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover should not fail when path is a file: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills when path is a file, got %d", len(skills))
	}
}

func TestClaudeSource_Discover_EmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}
