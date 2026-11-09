package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSkillAdapter_AdaptSkill_Nil(t *testing.T) {
	a := &ClaudeSkillAdapter{}
	got := a.AdaptSkill(nil)
	if got != nil {
		t.Errorf("AdaptSkill(nil) = %v, want nil", got)
	}
}

func TestClaudeSkillAdapter_AdaptSkill_SetsSource(t *testing.T) {
	a := &ClaudeSkillAdapter{}
	skill := &Skill{
		Name:      "claude-skill",
		Path:      filepath.Join("home", ".claude", "skills", "test", "SKILL.md"),
		Body:      "Do something useful.",
		RiskLevel: "low",
	}

	result := a.AdaptSkill(skill)

	if result.Source != "claude" {
		t.Errorf("Source = %q, want %q", result.Source, "claude")
	}
}

func TestClaudeSkillAdapter_AdaptSkill_DerivesDescriptionFromBody(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		wantDescription string
	}{
		{
			name:            "simple body",
			body:            "First line of instructions.\nSecond line.",
			wantDescription: "First line of instructions.",
		},
		{
			name:            "skips heading lines",
			body:            "# My Skill\n\nSome description text.\nMore text.",
			wantDescription: "Some description text.",
		},
		{
			name:            "skips empty and heading lines",
			body:            "# Title\n## Subtitle\n\nActual description here.",
			wantDescription: "Actual description here.",
		},
		{
			name:            "truncates long lines",
			body:            strings.Repeat("A", 250) + " more text",
			wantDescription: strings.Repeat("A", 200),
		},
		{
			name:            "body is empty string",
			body:            "",
			wantDescription: "",
		},
		{
			name:            "body is only headings",
			body:            "# Title\n## Subtitle",
			wantDescription: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ClaudeSkillAdapter{}
			skill := &Skill{
				Name: "desc-test",
				Body: tt.body,
			}

			result := a.AdaptSkill(skill)

			if result.Description != tt.wantDescription {
				t.Errorf("Description = %q, want %q", result.Description, tt.wantDescription)
			}
		})
	}
}

func TestClaudeSkillAdapter_AdaptSkill_DerivesTagFromDirectory(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		tags     []string
		wantTags []string
	}{
		{
			name:     "derives tag from directory name",
			path:     filepath.Join("home", ".claude", "skills", "graphify", "SKILL.md"),
			tags:     nil,
			wantTags: []string{"graphify"},
		},
		{
			name:     "derives tag from kebab directory",
			path:     filepath.Join("home", ".claude", "skills", "ui-patterns", "SKILL.md"),
			tags:     nil,
			wantTags: []string{"ui-patterns"},
		},
		{
			name:     "does not overwrite existing tags",
			path:     filepath.Join("home", ".claude", "skills", "graphify", "SKILL.md"),
			tags:     []string{"existing-tag"},
			wantTags: []string{"existing-tag"},
		},
		{
			name:     "no tag when parent is skills root",
			path:     filepath.Join("home", ".claude", "skills", "SKILL.md"),
			tags:     nil,
			wantTags: nil,
		},
		{
			name:     "no tag when path is empty",
			path:     "",
			tags:     nil,
			wantTags: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ClaudeSkillAdapter{}
			skill := &Skill{
				Name: "tag-test",
				Path: tt.path,
				Tags: tt.tags,
			}

			result := a.AdaptSkill(skill)

			if len(result.Tags) != len(tt.wantTags) {
				t.Errorf("Tags = %v, want %v", result.Tags, tt.wantTags)
				return
			}
			for i, tag := range result.Tags {
				if tag != tt.wantTags[i] {
					t.Errorf("Tags[%d] = %q, want %q", i, tag, tt.wantTags[i])
				}
			}
		})
	}
}

func TestClaudeSkillAdapter_AdaptSkill_PreservesExistingValues(t *testing.T) {
	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Requires:    []string{"code"},
		Tags:        []string{"test"},
		Body:        "Do something.",
		RiskLevel:   "low",
	}

	a := &ClaudeSkillAdapter{}
	result := a.AdaptSkill(skill)

	// Source should be set to "claude" even if description/tags exist.
	if result.Source != "claude" {
		t.Errorf("Source = %q, want %q", result.Source, "claude")
	}
	// Existing description and tags should NOT be overwritten.
	if result.Description != "A test skill" {
		t.Errorf("Description = %q, want %q (should not overwrite)", result.Description, "A test skill")
	}
	if len(result.Tags) != 1 || result.Tags[0] != "test" {
		t.Errorf("Tags = %v, want %v (should not overwrite)", result.Tags, []string{"test"})
	}
}

func TestClaudeSkillAdapter_AdaptSkill_Idempotent(t *testing.T) {
	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Requires:    []string{"code"},
		Tags:        []string{"test"},
		Body:        "Do something.",
		RiskLevel:   "low",
		Path:        filepath.Join("home", ".claude", "skills", "test", "SKILL.md"),
	}

	a := &ClaudeSkillAdapter{}
	first := a.AdaptSkill(skill)
	second := a.AdaptSkill(first)

	// Source is always set, so idempotent means it stays "claude".
	if second.Source != "claude" {
		t.Errorf("After double AdaptSkill, Source = %q, want %q", second.Source, "claude")
	}
	if second.Name != skill.Name {
		t.Errorf("After double AdaptSkill, Name = %q, want %q", second.Name, skill.Name)
	}
	if second.Description != "A test skill" {
		t.Errorf("After double AdaptSkill, Description = %q, want %q", second.Description, "A test skill")
	}
	if len(second.Tags) != len(skill.Tags) {
		t.Errorf("After double AdaptSkill, Tags count = %d, want %d", len(second.Tags), len(skill.Tags))
	}
}
