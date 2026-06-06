package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mockSource is a test-only SkillSource that returns pre-configured skills.
type mockSource struct {
	name   string
	skills []*Skill
	err    error
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) Discover(ctx context.Context) ([]*Skill, error) {
	return m.skills, m.err
}

func TestDiscovery_WithSources(t *testing.T) {
	projectDir, err := os.MkdirTemp("", "discovery-withsources-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(projectDir)

	skillContent := `---
name: project-skill
description: A project skill
---
Project body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "project-skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	discovery := NewDiscovery(
		WithSources(
			NewFileSource([]DiscoveryTier{
				{Path: projectDir, Priority: PriorityProject},
			}, nil),
		),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "project-skill" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "project-skill")
	}
}

func TestDiscovery_Sources(t *testing.T) {
	discovery := NewDiscovery()

	sources := discovery.Sources()
	if len(sources) != 2 {
		t.Errorf("Expected 2 default sources (filesystem + claude), got %d", len(sources))
	}

	names := map[string]bool{}
	for _, s := range sources {
		names[s.Name()] = true
	}
	if !names["filesystem"] {
		t.Error("Missing 'filesystem' source")
	}
	if !names["claude"] {
		t.Error("Missing 'claude' source")
	}
}

func TestDiscovery_DefaultSources(t *testing.T) {
	// NewDiscovery with no options should have filesystem + claude sources
	discovery := NewDiscovery()

	sources := discovery.Sources()
	if len(sources) != 2 {
		t.Fatalf("Expected 2 default sources, got %d", len(sources))
	}

	// filesystem source should have default tiers
	fsFound := false
	for _, s := range sources {
		if s.Name() == "filesystem" {
			fsFound = true
		}
	}
	if !fsFound {
		t.Error("No filesystem source in defaults")
	}
}

func TestDiscovery_WithTiers_CreatesFileSource(t *testing.T) {
	projectDir, err := os.MkdirTemp("", "discovery-tiers-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(projectDir)

	skillContent := `---
name: tier-skill
description: Skill from tier
---
Body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "tier-skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: projectDir, Priority: PriorityProject},
		}),
	)

	// WithTiers should create a FileSource
	sources := discovery.Sources()
	if len(sources) != 1 {
		t.Fatalf("WithTiers should create exactly 1 source, got %d", len(sources))
	}
	if sources[0].Name() != "filesystem" {
		t.Errorf("WithTiers should create a FileSource (name=%q), got %q", "filesystem", sources[0].Name())
	}

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}
}

func TestDiscovery_MultipleSources_Shadowing(t *testing.T) {
	projectDir, err := os.MkdirTemp("", "discovery-multi-shadow-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(projectDir)

	claudeDir := filepath.Join(projectDir, "claude")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("Failed to create claude dir: %v", err)
	}

	// Same-named skill in both project and claude directories
	projectSkill := `---
name: shared-skill
description: Project version
---
Project body.
`
	claudeSkill := `---
name: shared-skill
description: Claude version
---
Claude body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "shared-skill.md"), []byte(projectSkill), 0o644); err != nil {
		t.Fatalf("Failed to write project skill: %v", err)
	}
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(claudeDir, "shared-skill.md"), []byte(claudeSkill), 0o644); err != nil {
		t.Fatalf("Failed to write claude skill: %v", err)
	}

	discovery := NewDiscovery(
		WithSources(
			NewFileSource([]DiscoveryTier{
				{Path: projectDir, Priority: PriorityProject},
			}, nil),
			NewClaudeSourceWithPath(claudeDir, nil),
		),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill (shadowed), got %d", len(skills))
	}

	// Project (priority 0) should shadow Claude (priority 2)
	if skills[0].Description != "Project version" {
		t.Errorf("Description = %q, want %q (project should shadow claude)", skills[0].Description, "Project version")
	}
}

func TestDiscovery_MockSource(t *testing.T) {
	mock := &mockSource{
		name: "test-mock",
		skills: []*Skill{
			{
				Name:        "mock-skill",
				Description: "From mock source",
				Priority:    5,
			},
		},
	}

	discovery := NewDiscovery(WithSources(mock))

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "mock-skill" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "mock-skill")
	}
}

func TestDiscovery_SourceError_Continue(t *testing.T) {
	// One source returns an error, other should still work
	projectDir, err := os.MkdirTemp("", "discovery-error-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(projectDir)

	skillContent := `---
name: surviving-skill
description: Should survive other source's error
---
Body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "surviving-skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	errorSource := &mockSource{
		name: "error-source",
		err:  os.ErrPermission,
	}

	discovery := NewDiscovery(
		WithSources(
			errorSource,
			NewFileSource([]DiscoveryTier{
				{Path: projectDir, Priority: PriorityProject},
			}, nil),
		),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover should not fail when one source errors: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill from surviving source, got %d", len(skills))
	}
	if skills[0].Name != "surviving-skill" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "surviving-skill")
	}
}

func TestDiscovery_DiscoverMetadataOnly_WithFileSource(t *testing.T) {
	projectDir, err := os.MkdirTemp("", "discovery-meta-only-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(projectDir)

	skillContent := `---
name: meta-skill
description: Has metadata
requires: [code]
tags: [test]
---
Body content.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "meta-skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: projectDir, Priority: PriorityProject},
		}),
	)

	entries, err := discovery.DiscoverMetadataOnly()
	if err != nil {
		t.Fatalf("DiscoverMetadataOnly failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "meta-skill" {
		t.Errorf("Name = %q, want %q", entries[0].Name, "meta-skill")
	}
}

func TestDiscovery_DiscoverMetadataOnly_WithClaudeSource(t *testing.T) {
	claudeDir, err := os.MkdirTemp("", "discovery-meta-claude-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(claudeDir)

	skillContent := `---
name: claude-meta
description: Claude meta skill
---
Body.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(claudeDir, "claude-meta.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	discovery := NewDiscovery(
		WithSources(
			NewClaudeSourceWithPath(claudeDir, nil),
		),
	)

	entries, err := discovery.DiscoverMetadataOnly()
	if err != nil {
		t.Fatalf("DiscoverMetadataOnly failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "claude-meta" {
		t.Errorf("Name = %q, want %q", entries[0].Name, "claude-meta")
	}
}

func TestDiscovery_SkillSource_Interface(t *testing.T) {
	// Verify that FileSource and ClaudeSource satisfy SkillSource
	var _ SkillSource = (*FileSource)(nil)
	var _ SkillSource = (*ClaudeSource)(nil)
	var _ SkillSource = (*mockSource)(nil)
}
