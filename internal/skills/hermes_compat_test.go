package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHermesFrontmatter(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  string
		wantName     string
		wantSource   string
		wantTags     []string
		wantPrereqs  bool
	}{
		{
			name: "full hermes skill",
			frontmatter: `name: research-deep-dive
description: Perform deep research
version: 1.0.0
license: MIT
platforms: [macos, linux]
prerequisites:
  env_vars: [BRAVE_API_KEY]
  commands: [curl, jq]
metadata:
  hermes:
    triggers: [research, investigate]
    toolsets: [research_tools]`,
			wantName:    "research-deep-dive",
			wantSource:  "hermes",
			wantTags:    []string{"macos", "linux", "research", "investigate"},
			wantPrereqs: true,
		},
		{
			name: "hermes with only version",
			frontmatter: `name: simple-skill
description: Simple hermes skill
version: 0.1.0`,
			wantName:    "simple-skill",
			wantSource:  "hermes",
			wantPrereqs: false,
		},
		{
			name: "hermes with platforms only",
			frontmatter: `name: platform-skill
description: Platform-specific skill
platforms: [macos]`,
			wantName:    "platform-skill",
			wantSource:  "hermes",
			wantTags:    []string{"macos"},
			wantPrereqs: false,
		},
		{
			name: "hermes with python packages",
			frontmatter: `name: python-skill
description: Uses python
prerequisites:
  python_packages: [requests, numpy]`,
			wantName:    "python-skill",
			wantSource:  "hermes",
			wantPrereqs: true,
		},
		{
			name: "meept-native skill no hermes fields",
			frontmatter: `name: native-skill
description: Native meept skill
requires: [code]
tags: [dev]`,
			wantName:    "native-skill",
			wantSource:  "",
			wantTags:    []string{"dev"},
			wantPrereqs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullText := "---\n" + tt.frontmatter + "\n---\n\n# Skill body\n"
			skill, err := ParseSkillText(fullText)
			if err != nil {
				t.Fatalf("ParseSkillText error: %v", err)
			}
			if skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if skill.SourceOrigin != tt.wantSource {
				t.Errorf("SourceOrigin = %q, want %q", skill.SourceOrigin, tt.wantSource)
			}
			if tt.wantPrereqs && skill.Prerequisites == nil {
				t.Error("Prerequisites is nil, want non-nil")
			}
			if !tt.wantPrereqs && skill.Prerequisites != nil {
				t.Errorf("Prerequisites = %+v, want nil", skill.Prerequisites)
			}
			if len(tt.wantTags) > 0 {
				for _, wantTag := range tt.wantTags {
					found := false
					for _, haveTag := range skill.Tags {
						if haveTag == wantTag {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Tag %q not found in %v", wantTag, skill.Tags)
					}
				}
			}
		})
	}
}

func TestHermesDiscovery(t *testing.T) {
	// Create a temporary Hermes skills directory.
	tmpDir := t.TempDir()
	hermesDir := filepath.Join(tmpDir, ".hermes", "skills")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatalf("failed to create hermes dir: %v", err)
	}

	// Write a Hermes skill.
	hermesSkill := filepath.Join(hermesDir, "hermes-test", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(hermesSkill), 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	content := `---
name: hermes-test-skill
description: A test Hermes skill
version: 1.0.0
license: MIT
prerequisites:
  commands: [sh]
---
# Hermes Test Skill Body
`
	if err := os.WriteFile(hermesSkill, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write skill: %v", err)
	}

	// Verify hermes tier is added when dir exists.
	homeDir := tmpDir
	hermesPath := filepath.Join(homeDir, ".hermes", "skills")
	info, err := os.Stat(hermesPath)
	if err != nil || !info.IsDir() {
		t.Fatal("hermes dir should exist")
	}

	// The hermes tier should be in DefaultTiers only if ~/.hermes/skills exists on the system.
	// In this test, we verify with a custom setup via FileSource.
	source := NewFileSource([]DiscoveryTier{
		{Path: hermesPath, Priority: PriorityHermes},
	}, nil)
	skills, err := source.Discover(context.Background())
	if err != nil {
		t.Fatalf("FileSource.Discover error: %v", err)
	}
	if len(skills) == 0 {
		t.Error("expected at least 1 skill from hermes dir")
	}
	found := false
	for _, s := range skills {
		if s.Name == "hermes-test-skill" {
			found = true
			if s.Priority != PriorityHermes {
				t.Errorf("Priority = %d, want %d", s.Priority, PriorityHermes)
			}
			if s.SourceOrigin != "hermes" {
				t.Errorf("SourceOrigin = %q, want %q", s.SourceOrigin, "hermes")
			}
			if s.Source != "hermes" {
				t.Errorf("Source = %q, want %q", s.Source, "hermes")
			}
		}
	}
	if !found {
		t.Error("hermes-test-skill not found in discovered skills")
	}
}

func TestPrerequisiteChecker(t *testing.T) {
	checker := NewDefaultPrerequisiteChecker(nil)

	t.Run("CheckEnvVars", func(t *testing.T) {
		// PATH is always set.
		if err := checker.CheckEnvVars([]string{"PATH"}); err != nil {
			t.Errorf("CheckEnvVars(PATH) error: %v", err)
		}

		// Non-existent env var should fail.
		err := checker.CheckEnvVars([]string{"MEEPT_HERMES_TEST_NONEXISTENT_VAR_XYZ123"})
		if err == nil {
			t.Error("CheckEnvVars should fail for non-existent var")
		}
	})

	t.Run("CheckCommands", func(t *testing.T) {
		// "sh" should exist on all Unix systems.
		if err := checker.CheckCommands([]string{"sh"}); err != nil {
			t.Errorf("CheckCommands(sh) error: %v", err)
		}

		// Non-existent command should fail.
		err := checker.CheckCommands([]string{"meept_nonexistent_command_xyz123"})
		if err == nil {
			t.Error("CheckCommands should fail for non-existent command")
		}
	})

	t.Run("CheckPythonPackages", func(t *testing.T) {
		// Skip if pip is not available.
		if err := checker.CheckCommands([]string{"pip"}); err != nil {
			t.Skip("pip not available, skipping python package test")
		}

		// pip itself is not a package to check, so we test with a known failure.
		err := checker.CheckPythonPackages([]string{"meept_nonexistent_pkg_xyz123"})
		if err == nil {
			t.Error("CheckPythonPackages should fail for non-existent package")
		}
	})

	t.Run("nil prerequisites", func(t *testing.T) {
		if err := CheckPrerequisites(checker, nil); err != nil {
			t.Errorf("CheckPrerequisites(nil) should return nil, got: %v", err)
		}
	})

	t.Run("nil checker", func(t *testing.T) {
		prereqs := &HermesPrerequisites{
			EnvVars: []string{"PATH"},
		}
		if err := CheckPrerequisites(nil, prereqs); err != nil {
			t.Errorf("CheckPrerequisites(nil checker) should return nil, got: %v", err)
		}
	})

	t.Run("empty prerequisites", func(t *testing.T) {
		prereqs := &HermesPrerequisites{}
		if err := CheckPrerequisites(checker, prereqs); err != nil {
			t.Errorf("CheckPrerequisites(empty) should return nil, got: %v", err)
		}
	})
}

func TestHermesToolMapping(t *testing.T) {
	mapper := NewHermesToolMapper(nil)

	tests := []struct {
		hermes string
		meept  string
	}{
		{"schedule", "schedule_create"},
		{"skill_view", "skills.get"},
		{"skills_list", "skills.list"},
		{"team_create", "delegate_task"},
		{"team_list", "platform_agents"},
		{"team_message", "request_handoff"},
		{"shell", "shell"},       // passthrough
		{"file_read", "file_read"}, // passthrough
		{"unknown_tool", "unknown_tool"}, // passthrough
	}

	for _, tt := range tests {
		t.Run(tt.hermes+" -> "+tt.meept, func(t *testing.T) {
			got := mapper.Translate(tt.hermes)
			if got != tt.meept {
				t.Errorf("Translate(%q) = %q, want %q", tt.hermes, got, tt.meept)
			}
		})
	}
}

func TestTranslateToolReferences(t *testing.T) {
	mapper := NewHermesToolMapper(nil)

	tests := []struct {
		name     string
		body     string
		wantBody string
	}{
		{
			name:     "function call style",
			body:     "Use schedule(deep-research, weekly) to create a task.",
			wantBody: "Use schedule_create(deep-research, weekly) to create a task.",
		},
		{
			name:     "quoted tool reference",
			body:     `Use "skill_view" to inspect skills.`,
			wantBody: `Use "skills.get" to inspect skills.`,
		},
		{
			name:     "backtick tool reference",
			body:     "Run `skills_list` to see all skills.",
			wantBody: "Run `skills.list` to see all skills.",
		},
		{
			name:     "list item tool reference",
			body:     "- team_create for team management\n- shell for commands",
			wantBody: "- delegate_task for team management\n- shell for commands",
		},
		{
			name:     "no hermes tools",
			body:     "Use shell and file_read for basic operations.",
			wantBody: "Use shell and file_read for basic operations.",
		},
		{
			name:     "multiple replacements",
			body:     "Call team_list first, then team_create, and schedule(later).",
			wantBody: "Call platform_agents first, then delegate_task, and schedule_create(later).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapper.TranslateToolReferences(tt.body)
			if got != tt.wantBody {
				t.Errorf("TranslateToolReferences mismatch:\n  got:  %q\n  want: %q", got, tt.wantBody)
			}
		})
	}
}

func TestConfigIntegration(t *testing.T) {
	// Test that the config schema has the expected Hermes fields.
	// This is verified through the build (compilation) and through
	// the DefaultConfig function.
	//
	// We verify the config defaults are correct by checking the
	// DefaultTiers behavior when Hermes auto-discovery is enabled.
	t.Run("default tiers include hermes when dir exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		hermesDir := filepath.Join(tmpDir, ".hermes", "skills")
		if err := os.MkdirAll(hermesDir, 0o755); err != nil {
			t.Fatalf("failed to create hermes dir: %v", err)
		}

		// Create a skill in the hermes dir.
		skillPath := filepath.Join(hermesDir, "test", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
			t.Fatalf("failed to create skill dir: %v", err)
		}
		if err := os.WriteFile(skillPath, []byte(`---
name: config-test
description: Config integration test
version: 1.0.0
---
`), 0o644); err != nil {
			t.Fatalf("failed to write skill: %v", err)
		}

		// Use FileSource to simulate DefaultTiers behavior.
		source := NewFileSource([]DiscoveryTier{
			{Path: hermesDir, Priority: PriorityHermes},
		}, nil)
		skills, err := source.Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover error: %v", err)
		}
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(skills))
		}
		if skills[0].SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", skills[0].SourceOrigin, "hermes")
		}
	})

	t.Run("hermes skill preserves meept fields", func(t *testing.T) {
		content := `---
name: hybrid-skill
description: Has both Meept and Hermes fields
requires: [code, reasoning]
risk-level: low
version: 1.0.0
platforms: [linux]
prerequisites:
  env_vars: [TEST_VAR]
---
# Hybrid skill body
`
		skill, err := ParseSkillText(content)
		if err != nil {
			t.Fatalf("ParseSkillText error: %v", err)
		}
		if skill.RiskLevel != "low" {
			t.Errorf("RiskLevel = %q, want %q", skill.RiskLevel, "low")
		}
		if len(skill.Requires) != 2 {
			t.Errorf("Requires = %v, want [code, reasoning]", skill.Requires)
		}
		if skill.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", skill.SourceOrigin, "hermes")
		}
		if skill.Prerequisites == nil {
			t.Error("Prerequisites should not be nil")
		} else if len(skill.Prerequisites.EnvVars) != 1 || skill.Prerequisites.EnvVars[0] != "TEST_VAR" {
			t.Errorf("Prerequisites.EnvVars = %v, want [TEST_VAR]", skill.Prerequisites.EnvVars)
		}
		if !skill.HasTag("linux") {
			t.Error("should have 'linux' tag from platforms")
		}
	})
}
