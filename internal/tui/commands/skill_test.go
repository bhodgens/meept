package commands

import (
	"context"
	"strings"
	"testing"
)

// fakeSkillLister is a test double for SkillLister that returns canned data.
type fakeSkillLister struct {
	skills []SkillInfo
	err    error
}

func (f *fakeSkillLister) ListSkills(_ context.Context) ([]SkillInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	// Return a defensive copy so tests mutating the slice don't bleed state.
	out := make([]SkillInfo, len(f.skills))
	copy(out, f.skills)
	return out, nil
}

func TestSkillCommand_List(t *testing.T) {
	lister := &fakeSkillLister{
		skills: []SkillInfo{
			{Name: "code-review", Description: "Review code changes"},
			{Name: "debugger", Description: "Debug issues"},
		},
	}

	handler := NewSkillCommand(lister)
	result := handler.Execute([]string{})

	if result.Output == "" {
		t.Error("expected non-empty output for skill list")
	}

	if !strings.Contains(result.Output, "code-review") {
		t.Error("expected output to contain 'code-review'")
	}

	if !strings.Contains(result.Output, "debugger") {
		t.Error("expected output to contain 'debugger'")
	}
}

func TestSkillCommand_Show(t *testing.T) {
	lister := &fakeSkillLister{
		skills: []SkillInfo{
			{
				Name:        "test-skill",
				Description: "A test skill",
				Tags:        []string{"test", "example"},
				RiskLevel:   "low",
			},
		},
	}

	handler := NewSkillCommand(lister)
	result := handler.Execute([]string{"test-skill"})

	if result.Output == "" {
		t.Error("expected non-empty output for skill show")
	}

	if !strings.Contains(result.Output, "test-skill") {
		t.Error("expected output to contain skill name")
	}
}

func TestSkillCommand_NotFound(t *testing.T) {
	lister := &fakeSkillLister{skills: nil}
	handler := NewSkillCommand(lister)
	result := handler.Execute([]string{"nonexistent"})

	if !result.IsError {
		t.Error("expected error for nonexistent skill")
	}
}

func TestSkillCommand_Search(t *testing.T) {
	lister := &fakeSkillLister{
		skills: []SkillInfo{
			{Name: "code-review", Description: "Review code changes"},
		},
	}

	handler := NewSkillCommand(lister)
	result := handler.Execute([]string{"search", "code"})

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Output)
	}

	if !strings.Contains(result.Output, "code-review") {
		t.Error("expected search to find 'code-review'")
	}
}

func TestSkillCommand_SearchNotFound(t *testing.T) {
	lister := &fakeSkillLister{skills: nil}
	handler := NewSkillCommand(lister)
	result := handler.Execute([]string{"search", "nonexistent"})

	if !result.IsError {
		t.Error("expected error for nonexistent search")
	}
}

func TestSkillCommand_EmptyArgs(t *testing.T) {
	lister := &fakeSkillLister{
		skills: []SkillInfo{
			{Name: "test", Description: "Test skill"},
		},
	}

	handler := NewSkillCommand(lister)
	result := handler.Execute([]string{})

	if result.IsError {
		t.Errorf("unexpected error for empty args: %s", result.Output)
	}
}

func TestGetSkillNames(t *testing.T) {
	lister := &fakeSkillLister{
		skills: []SkillInfo{
			{Name: "alpha", Description: "First"},
			{Name: "beta", Description: "Second"},
		},
	}

	handler := NewSkillCommand(lister)
	names := handler.GetSkillNames()

	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	// Should be sorted
	if names[0] != "alpha" || names[1] != "beta" {
		t.Error("expected names to be sorted")
	}
}
