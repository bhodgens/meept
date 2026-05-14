package templates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscovery_SingleTier(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-discovery-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create templates directory.
	templatesDir := filepath.Join(tmpDir, "templates")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("Failed to create templates dir: %v", err)
	}

	// Create a template in a subdirectory with TEMPLATE.md.
	subDir := filepath.Join(templatesDir, "code-review")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create template subdir: %v", err)
	}

	templateContent := `---
name: code-review
description: Review code for quality
scope: turn
---

Review the provided code.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(subDir, "TEMPLATE.md"), []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to write template file: %v", err)
	}

	// Create a flat template file.
	flatTemplate := `---
name: flat-template
description: A flat template file
---

Flat template instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(templatesDir, "flat-template.md"), []byte(flatTemplate), 0644); err != nil {
		t.Fatalf("Failed to write flat template: %v", err)
	}

	// Create discovery with custom tier.
	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: templatesDir, Priority: PriorityProject},
		}),
	)

	templates, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(templates) != 2 {
		t.Errorf("Expected 2 templates, got %d", len(templates))
	}

	// Check code-review template.
	codeReview := discovery.GetTemplate("code-review")
	if codeReview == nil {
		t.Fatal("code-review template not found")
	}
	if codeReview.Description != "Review code for quality" {
		t.Errorf("Description = %q, want 'Review code for quality'", codeReview.Description)
	}

	// Check flat template.
	flatFound := discovery.GetTemplate("flat-template")
	if flatFound == nil {
		t.Fatal("flat-template not found")
	}
}

func TestDiscovery_Shadowing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-shadow-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	projectDir := filepath.Join(tmpDir, "project-templates")
	userDir := filepath.Join(tmpDir, "user-templates")

	for _, dir := range []string{projectDir, userDir} {
		//nolint:gosec // test directory/file
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
	}

	// Create same-named template in both tiers with different descriptions.
	projectTemplate := `---
name: shared-template
description: Project version
---

Project instructions.
`
	userTemplate := `---
name: shared-template
description: User version
---

User instructions.
`

	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(projectDir, "shared-template.md"), []byte(projectTemplate), 0644); err != nil {
		t.Fatalf("Failed to write project template: %v", err)
	}
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(userDir, "shared-template.md"), []byte(userTemplate), 0644); err != nil {
		t.Fatalf("Failed to write user template: %v", err)
	}

	// Discovery with project tier having higher priority.
	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: projectDir, Priority: PriorityProject}, // Higher priority (0)
			{Path: userDir, Priority: PriorityUser},       // Lower priority (1)
		}),
	)

	templates, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(templates) != 1 {
		t.Errorf("Expected 1 template (shadowed), got %d", len(templates))
	}

	tmpl := discovery.GetTemplate("shared-template")
	if tmpl == nil {
		t.Fatal("shared-template not found")
	}

	// Project version should shadow user version.
	if tmpl.Description != "Project version" {
		t.Errorf("Description = %q, want 'Project version' (project should shadow user)", tmpl.Description)
	}

	if tmpl.Priority != PriorityProject {
		t.Errorf("Priority = %d, want %d", tmpl.Priority, PriorityProject)
	}
}

func TestDiscovery_NonexistentDirectory(t *testing.T) {
	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: "/nonexistent/path/templates", Priority: PriorityProject},
		}),
	)

	templates, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover should not fail for nonexistent dir: %v", err)
	}

	if len(templates) != 0 {
		t.Errorf("Expected 0 templates, got %d", len(templates))
	}
}

func TestDiscovery_CaseInsensitiveLookup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-case-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateContent := `---
name: MixedCase-Template
description: Template with mixed case name
---

Instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "mixed.md"), []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to write template: %v", err)
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

	// Should find template with any case.
	tests := []string{
		"MixedCase-Template",
		"mixedcase-template",
		"MIXEDCASE-TEMPLATE",
		"mixedCase-template",
	}

	for _, name := range tests {
		tmpl := discovery.GetTemplate(name)
		if tmpl == nil {
			t.Errorf("GetTemplate(%q) = nil, want non-nil", name)
		}
	}
}

func TestDiscovery_ExcludesReadme(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-readme-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create README.md (should be excluded).
	readme := `---
name: readme-template
description: This should be excluded
---

Not a real template.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	// Create actual template.
	tmpl := `---
name: real-template
description: A real template
---

Real instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "real-template.md"), []byte(tmpl), 0644); err != nil {
		t.Fatalf("Failed to write template: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: tmpDir, Priority: PriorityProject},
		}),
	)

	templates, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(templates) != 1 {
		t.Errorf("Expected 1 template (README excluded), got %d", len(templates))
	}

	if discovery.GetTemplate("readme-template") != nil {
		t.Error("README.md should be excluded")
	}

	if discovery.GetTemplate("real-template") == nil {
		t.Error("real-template should be found")
	}
}

func TestDiscovery_ListTemplates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-list-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateNames := []string{"alpha-template", "beta-template", "gamma-template"}
	for _, name := range templateNames {
		content := `---
name: ` + name + `
description: Test template
---

Body.
`
		//nolint:gosec // test directory/file
		if err := os.WriteFile(filepath.Join(tmpDir, name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write template: %v", err)
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

	names := discovery.ListTemplates()
	if len(names) != 3 {
		t.Errorf("Expected 3 names, got %d", len(names))
	}

	// Should be sorted.
	if names[0] != "alpha-template" {
		t.Errorf("First name = %q, want alpha-template (should be sorted)", names[0])
	}
}

func TestDiscovery_Count(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-count-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for i := range 5 {
		content := `---
name: template-` + string(rune('a'+i)) + `
description: Test
---
Body.
`
		//nolint:gosec // test directory/file
		if err := os.WriteFile(filepath.Join(tmpDir, "template"+string(rune('a'+i))+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write template: %v", err)
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

func TestIsTemplateFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"template.md", true},
		{"TEMPLATE.md", true},
		{"summarize.md", true},
		{"README.md", false},
		{"readme.md", false},
		{"CHANGELOG.md", false},
		{"LICENSE.md", false},
		{"CONTRIBUTING.md", false},
		{"template.txt", false},
		{"template", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTemplateFile(tt.name)
			if got != tt.want {
				t.Errorf("isTemplateFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDiscovery_SubdirTemplate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-subdir-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory with TEMPLATE.md.
	subDir := filepath.Join(tmpDir, "explain")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	content := `---
name: explain
description: Explain code step by step
scope: turn
---

Explain this code step by step.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(subDir, "TEMPLATE.md"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write TEMPLATE.md: %v", err)
	}

	// Also create a non-TEMPLATE.md file in another subdir (should be skipped).
	otherDir := filepath.Join(tmpDir, "other")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatalf("Failed to create other dir: %v", err)
	}
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(otherDir, "notes.md"), []byte("just notes"), 0644); err != nil {
		t.Fatalf("Failed to write notes: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: tmpDir, Priority: PriorityProject},
		}),
	)

	templates, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(templates) != 1 {
		t.Errorf("Expected 1 template, got %d", len(templates))
	}

	tmpl := discovery.GetTemplate("explain")
	if tmpl == nil {
		t.Fatal("explain template not found")
	}
}
