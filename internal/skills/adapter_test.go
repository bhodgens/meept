package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsClaudeSkillPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory")
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "claude skills subdirectory",
			path: filepath.Join(homeDir, ".claude", "skills", "graphify", "SKILL.md"),
			want: true,
		},
		{
			name: "claude skills root",
			path: filepath.Join(homeDir, ".claude", "skills"),
			want: true,
		},
		{
			name: "claude config but not skills",
			path: filepath.Join(homeDir, ".claude", "settings.json"),
			want: false,
		},
		{
			name: "meept project skills",
			path: filepath.Join("project", ".meept", "skills", "code-review", "SKILL.md"),
			want: false,
		},
		{
			name: "meept user skills",
			path: filepath.Join(homeDir, ".meept", "skills", "my-skill", "SKILL.md"),
			want: false,
		},
		{
			name: "meept system skills",
			path: filepath.Join(homeDir, ".config", "meept", "skills", "system-skill", "SKILL.md"),
			want: false,
		},
		{
			name: "empty path",
			path: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsClaudeSkillPath(tt.path)
			if got != tt.want {
				t.Errorf("IsClaudeSkillPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestClaudeSkillAdapter_AdaptSkill_Nil(t *testing.T) {
	a := &ClaudeSkillAdapter{}
	got := a.AdaptSkill(nil)
	if got != nil {
		t.Errorf("AdaptSkill(nil) = %v, want nil", got)
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
	}

	a := &ClaudeSkillAdapter{}
	first := a.AdaptSkill(skill)
	second := a.AdaptSkill(first)

	if second.Name != skill.Name {
		t.Errorf("After double AdaptSkill, Name = %q, want %q", second.Name, skill.Name)
	}
	if len(second.Tags) != len(skill.Tags) {
		t.Errorf("After double AdaptSkill, Tags count = %d, want %d", len(second.Tags), len(skill.Tags))
	}
}

func TestClaudeSkillAdapter_AdaptSkill_NoChanges(t *testing.T) {
	skill := &Skill{
		Name:        "unchanged-skill",
		Description: "Skill that needs no adaptation",
		Body:         "Instructions.",
		RiskLevel:    "medium",
		MaxIterations: 10,
	}

	a := &ClaudeSkillAdapter{}
	result := a.AdaptSkill(skill)

	// Should return the same pointer (or equal values)
	if result.Name != skill.Name {
		t.Errorf("Name changed: %q -> %q", skill.Name, result.Name)
	}
	if result.RiskLevel != skill.RiskLevel {
		t.Errorf("RiskLevel changed: %q -> %q", skill.RiskLevel, result.RiskLevel)
	}
	if result.MaxIterations != skill.MaxIterations {
		t.Errorf("MaxIterations changed: %d -> %d", skill.MaxIterations, result.MaxIterations)
	}
}
