package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/pathutil"
)

func TestFileSource_Name(t *testing.T) {
	src := NewFileSource(nil, nil)
	if got := src.Name(); got != "filesystem" {
		t.Errorf("FileSource.Name() = %q, want %q", got, "filesystem")
	}
}

func TestFileSource_Discover_SingleTier(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-single-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillsDir := filepath.Join(tmpDir, "skills")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("Failed to create skills dir: %v", err)
	}

	// Create a skill in a subdirectory
	subDir := filepath.Join(skillsDir, "my-skill")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create skill subdir: %v", err)
	}

	skillContent := `---
name: my-skill
description: A test skill
requires: [code]
tags: [development]
---
Skill body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	src := NewFileSource([]DiscoveryTier{
		{Path: skillsDir, Priority: PriorityProject},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	if skills[0].Name != "my-skill" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "my-skill")
	}
	if skills[0].Description != "A test skill" {
		t.Errorf("Description = %q, want %q", skills[0].Description, "A test skill")
	}
	if skills[0].Priority != PriorityProject {
		t.Errorf("Priority = %d, want %d", skills[0].Priority, PriorityProject)
	}
}

func TestFileSource_Discover_FlatAndDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-flat-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create flat skill
	flatContent := `---
name: flat-skill
description: Flat skill
---
Flat body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "flat-skill.md"), []byte(flatContent), 0o644); err != nil {
		t.Fatalf("Failed to write flat skill: %v", err)
	}

	// Create directory skill
	dirSkill := filepath.Join(tmpDir, "dir-skill")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(dirSkill, 0o755); err != nil {
		t.Fatalf("Failed to create skill subdir: %v", err)
	}

	dirContent := `---
name: dir-skill
description: Directory skill
---
Dir body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(dirSkill, "SKILL.md"), []byte(dirContent), 0o644); err != nil {
		t.Fatalf("Failed to write dir skill: %v", err)
	}

	src := NewFileSource([]DiscoveryTier{
		{Path: tmpDir, Priority: PriorityUser},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("Expected 2 skills, got %d", len(skills))
	}

	names := map[string]bool{}
	for _, s := range skills {
		names[s.Name] = true
	}
	if !names["flat-skill"] {
		t.Error("Missing flat-skill")
	}
	if !names["dir-skill"] {
		t.Error("Missing dir-skill")
	}
}

func TestFileSource_Discover_Shadowing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-shadow-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	projectDir := filepath.Join(tmpDir, "project")
	userDir := filepath.Join(tmpDir, "user")

	for _, dir := range []string{projectDir, userDir} {
		//nolint:gosec // test directory/file
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
	}

	projectSkill := `---
name: shared-skill
description: Project version
---
Project body.
`
	userSkill := `---
name: shared-skill
description: User version
---
User body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "shared-skill.md"), []byte(projectSkill), 0o644); err != nil {
		t.Fatalf("Failed to write project skill: %v", err)
	}
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(userDir, "shared-skill.md"), []byte(userSkill), 0o644); err != nil {
		t.Fatalf("Failed to write user skill: %v", err)
	}

	src := NewFileSource([]DiscoveryTier{
		{Path: projectDir, Priority: PriorityProject},
		{Path: userDir, Priority: PriorityUser},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill (shadowed), got %d", len(skills))
	}

	if skills[0].Description != "Project version" {
		t.Errorf("Description = %q, want %q (project should shadow user)", skills[0].Description, "Project version")
	}
}

func TestFileSource_Discover_NonexistentDirectory(t *testing.T) {
	src := NewFileSource([]DiscoveryTier{
		{Path: "/nonexistent/path", Priority: PrioritySystem},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover should not fail for nonexistent dir: %v", err)
	}

	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}

func TestFileSource_Discover_ExcludesReadme(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-readme-*")
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
name: real-skill
description: A real skill
---
Real instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "real-skill.md"), []byte(realSkill), 0o644); err != nil {
		t.Fatalf("Failed to write real skill: %v", err)
	}

	src := NewFileSource([]DiscoveryTier{
		{Path: tmpDir, Priority: PriorityProject},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("Expected 1 skill (README excluded), got %d", len(skills))
	}
	if skills[0].Name != "real-skill" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "real-skill")
	}
}

func TestFileSource_DiscoverMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-meta-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillContent := `---
name: meta-skill
description: Has metadata
requires: [reasoning]
tags: [test]
---
Body content.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	src := NewFileSource([]DiscoveryTier{
		{Path: tmpDir, Priority: PriorityProject},
	}, nil)

	entries, err := src.DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatalf("DiscoverMetadata failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].Name != "meta-skill" {
		t.Errorf("Name = %q, want %q", entries[0].Name, "meta-skill")
	}
	if entries[0].Description != "Has metadata" {
		t.Errorf("Description = %q, want %q", entries[0].Description, "Has metadata")
	}
	// Metadata should not include the body
	if len(entries[0].Requires) != 1 || entries[0].Requires[0] != "reasoning" {
		t.Errorf("Requires = %v, want [reasoning]", entries[0].Requires)
	}
}

func TestFileSource_Discover_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-cancel-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a skill so the source has something to scan
	skillContent := `---
name: cancel-skill
description: Test
---
Body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "cancel-skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	src := NewFileSource([]DiscoveryTier{
		{Path: tmpDir, Priority: PriorityProject},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = src.Discover(ctx)
	if err == nil {
		t.Error("Expected error from cancelled context, got nil")
	}
}

func TestFileSource_Discover_Sorted(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesource-sort-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, name := range []string{"charlie", "alpha", "bravo"} {
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

	src := NewFileSource([]DiscoveryTier{
		{Path: tmpDir, Priority: PriorityProject},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 3 {
		t.Fatalf("Expected 3 skills, got %d", len(skills))
	}

	// Should be sorted by name
	if skills[0].Name != "alpha" || skills[1].Name != "bravo" || skills[2].Name != "charlie" {
		t.Errorf("Skills not sorted: got %v", []string{skills[0].Name, skills[1].Name, skills[2].Name})
	}
}

func TestFileSource_NilLogger(t *testing.T) {
	// Ensure nil logger is handled gracefully
	src := NewFileSource([]DiscoveryTier{
		{Path: "/nonexistent", Priority: PriorityProject},
	}, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}

func TestFileSource_Discover_TildeExpansion(t *testing.T) {
	// Verify that tilde paths are expanded correctly
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory")
	}

	expanded := pathutil.ExpandPath("~/skills")
	want := filepath.Join(homeDir, "skills")
	if expanded != want {
		t.Errorf("ExpandPath('~/skills') = %q, want %q", expanded, want)
	}
}
