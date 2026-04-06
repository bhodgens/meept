package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/pathutil"
)

func TestDiscovery_SingleTier(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "skills-discovery-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create skills directory
	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("Failed to create skills dir: %v", err)
	}

	// Create a skill in a subdirectory
	subDir := filepath.Join(skillsDir, "code-review")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create skill subdir: %v", err)
	}

	skillContent := `---
name: code-review
description: Review code for quality
requires: [code]
tags: [development]
---

Review the provided code.
`
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	// Create a flat skill file
	flatSkill := `---
name: flat-skill
description: A flat skill file
---

Flat skill instructions.
`
	if err := os.WriteFile(filepath.Join(skillsDir, "flat-skill.md"), []byte(flatSkill), 0644); err != nil {
		t.Fatalf("Failed to write flat skill: %v", err)
	}

	// Create discovery with custom tier
	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: skillsDir, Priority: PriorityProject},
		}),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 2 {
		t.Errorf("Expected 2 skills, got %d", len(skills))
	}

	// Check code-review skill
	codeReview := discovery.GetSkill("code-review")
	if codeReview == nil {
		t.Fatal("code-review skill not found")
	}
	if codeReview.Description != "Review code for quality" {
		t.Errorf("Description = %q, want 'Review code for quality'", codeReview.Description)
	}

	// Check flat skill
	flatFound := discovery.GetSkill("flat-skill")
	if flatFound == nil {
		t.Fatal("flat-skill not found")
	}
}

func TestDiscovery_Shadowing(t *testing.T) {
	// Create two temp directories for different tiers
	tmpDir, err := os.MkdirTemp("", "skills-shadow-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	projectDir := filepath.Join(tmpDir, "project-skills")
	userDir := filepath.Join(tmpDir, "user-skills")

	for _, dir := range []string{projectDir, userDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
	}

	// Create same-named skill in both tiers with different descriptions
	projectSkill := `---
name: shared-skill
description: Project version
---

Project instructions.
`
	userSkill := `---
name: shared-skill
description: User version
---

User instructions.
`

	if err := os.WriteFile(filepath.Join(projectDir, "shared-skill.md"), []byte(projectSkill), 0644); err != nil {
		t.Fatalf("Failed to write project skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "shared-skill.md"), []byte(userSkill), 0644); err != nil {
		t.Fatalf("Failed to write user skill: %v", err)
	}

	// Discovery with project tier having higher priority
	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: projectDir, Priority: PriorityProject}, // Higher priority (0)
			{Path: userDir, Priority: PriorityUser},       // Lower priority (1)
		}),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("Expected 1 skill (shadowed), got %d", len(skills))
	}

	skill := discovery.GetSkill("shared-skill")
	if skill == nil {
		t.Fatal("shared-skill not found")
	}

	// Project version should shadow user version
	if skill.Description != "Project version" {
		t.Errorf("Description = %q, want 'Project version' (project should shadow user)", skill.Description)
	}

	if skill.Priority != PriorityProject {
		t.Errorf("Priority = %d, want %d", skill.Priority, PriorityProject)
	}
}

func TestDiscovery_NonexistentDirectory(t *testing.T) {
	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: "/nonexistent/path/skills", Priority: PriorityProject},
		}),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover should not fail for nonexistent dir: %v", err)
	}

	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}

func TestDiscovery_CaseInsensitiveLookup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-case-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillContent := `---
name: MixedCase-Skill
description: Skill with mixed case name
---

Instructions.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "mixed.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: tmpDir, Priority: PriorityProject},
		}),
	)

	_, err = discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Should find skill with any case
	tests := []string{
		"MixedCase-Skill",
		"mixedcase-skill",
		"MIXEDCASE-SKILL",
		"mixedCase-skill",
	}

	for _, name := range tests {
		skill := discovery.GetSkill(name)
		if skill == nil {
			t.Errorf("GetSkill(%q) = nil, want non-nil", name)
		}
	}
}

func TestDiscovery_ExcludesReadme(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-readme-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create README.md (should be excluded)
	readme := `---
name: readme-skill
description: This should be excluded
---

Not a real skill.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	// Create actual skill
	skill := `---
name: real-skill
description: A real skill
---

Real instructions.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "real-skill.md"), []byte(skill), 0644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: tmpDir, Priority: PriorityProject},
		}),
	)

	skills, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("Expected 1 skill (README excluded), got %d", len(skills))
	}

	if discovery.GetSkill("readme-skill") != nil {
		t.Error("README.md should be excluded")
	}

	if discovery.GetSkill("real-skill") == nil {
		t.Error("real-skill should be found")
	}
}

func TestDiscovery_ListSkills(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-list-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skills := []string{"alpha-skill", "beta-skill", "gamma-skill"}
	for _, name := range skills {
		content := `---
name: ` + name + `
description: Test skill
---

Body.
`
		if err := os.WriteFile(filepath.Join(tmpDir, name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write skill: %v", err)
		}
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: tmpDir, Priority: PriorityProject},
		}),
	)

	_, err = discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	names := discovery.ListSkills()
	if len(names) != 3 {
		t.Errorf("Expected 3 names, got %d", len(names))
	}

	// Should be sorted
	if names[0] != "alpha-skill" {
		t.Errorf("First name = %q, want alpha-skill (should be sorted)", names[0])
	}
}

func TestDiscovery_Count(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-count-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for i := 0; i < 5; i++ {
		content := `---
name: skill-` + string(rune('a'+i)) + `
description: Test
---
Body.
`
		if err := os.WriteFile(filepath.Join(tmpDir, "skill"+string(rune('a'+i))+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write skill: %v", err)
		}
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: tmpDir, Priority: PriorityProject},
		}),
	)

	_, _ = discovery.Discover()

	if discovery.Count() != 5 {
		t.Errorf("Count() = %d, want 5", discovery.Count())
	}
}

func TestIsSkillFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"skill.md", true},
		{"SKILL.md", true},
		{"code-review.md", true},
		{"README.md", false},
		{"readme.md", false},
		{"CHANGELOG.md", false},
		{"LICENSE.md", false},
		{"CONTRIBUTING.md", false},
		{"skill.txt", false},
		{"skill", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSkillFile(tt.name)
			if got != tt.want {
				t.Errorf("isSkillFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		input string
		want  string
	}{
		{"~", homeDir},
		{"~/skills", filepath.Join(homeDir, "skills")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"}, // Cleaned but not made absolute
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := pathutil.ExpandPath(tt.input)
			if got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
