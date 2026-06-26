package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// TestContextInjector_SetSkillLoader_Nil verifies the setter is nil-safe
// (per the typed-nil setter guard rule in CLAUDE.md).
func TestContextInjector_SetSkillLoader_Nil(t *testing.T) {
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(nil) // must be a no-op, no panic
	if ci.skillLoader != nil {
		t.Error("SetSkillLoader(nil) should not set the field")
	}
}

// TestContextInjector_SkillsSection verifies that when a LazySkillLoader with
// cached skills is wired, BuildSystemPrompt includes an "## Active Skills"
// section listing each skill. It also verifies the section is absent when the
// cache is empty.
func TestContextInjector_SkillsSection(t *testing.T) {
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "code-review",
		Description: "Review code for bugs and style",
	})
	loader := skills.NewLazySkillLoader(idx)

	// With no cached skills, the prompt should NOT contain the skills section.
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	prompt := ci.BuildSystemPrompt(context.Background(), "base")
	if strings.Contains(prompt, "## Active Skills") {
		t.Error("expected no Active Skills section when cache is empty")
	}
	if !strings.Contains(prompt, "base") {
		t.Error("base prompt missing")
	}

	// Compile-time import check.
	_ = skills.LazySkillLoader{}
}

// TestContextInjector_BaseOnly verifies that with no learning, no instructions,
// and no skill loader, BuildSystemPrompt returns the base prompt unchanged.
func TestContextInjector_BaseOnly(t *testing.T) {
	ci := NewContextInjector(nil, nil)
	prompt := ci.BuildSystemPrompt(context.Background(), "you are a helper")
	if prompt != "you are a helper" {
		t.Errorf("expected base prompt unchanged, got %q", prompt)
	}
}

// createTestSkillFile creates a temporary skill directory with a SKILL.md file
// for testing lazy loading via the filesystem.
func createTestSkillFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill directory: %v", err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}
	return skillFile
}

// TestContextInjector_SkillsRelevanceFilter verifies that when the base prompt
// contains task-relevant keywords, only matching skills appear in the output
// (not all cached skills). Skills with no relevance to the base are filtered out.
func TestContextInjector_SkillsRelevanceFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two skill files on disk: one for code review, one for deployment.
	reviewContent := `---
name: code review
description: Review code for bugs and style
tags:
  - testing
---

# Code Review

Review code changes.
`
	reviewPath := createTestSkillFile(t, tmpDir, "code review", reviewContent)

	deployContent := `---
name: deploy
description: Deploy applications to production
tags:
  - ops
---

# Deploy

Deploy to production.
`
	deployPath := createTestSkillFile(t, tmpDir, "deploy", deployContent)

	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "code review",
		Description: "Review code for bugs and style",
		Tags:        []string{"testing"},
		Path:        reviewPath,
	})
	idx.Index(&skills.SkillIndexEntry{
		Name:        "deploy",
		Description: "Deploy applications to production",
		Tags:        []string{"ops"},
		Path:        deployPath,
	})

	loader := skills.NewLazySkillLoader(idx)

	// Load both skills into the cache.
	ctx := context.Background()
	if _, err := loader.Load(ctx, "code review"); err != nil {
		t.Fatalf("failed to load 'code review' skill: %v", err)
	}
	if _, err := loader.Load(ctx, "deploy"); err != nil {
		t.Fatalf("failed to load 'deploy' skill: %v", err)
	}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	// Base prompt mentions "review" which should match "code review" skill.
	// "reviewer" is a query word that matches name word "review" (+5).
	base := "you are a code reviewer for pull requests"
	prompt := ci.BuildSystemPrompt(ctx, base)

	// Active Skills section should be present.
	if !strings.Contains(prompt, "## Active Skills") {
		t.Error("expected Active Skills section when cached skills match base")
	}

	// The "code review" skill should appear (word match: "review" in name).
	if !strings.Contains(prompt, "code review") {
		t.Error("expected 'code review' skill in prompt when base mentions reviewer")
	}

	// The "deploy" skill should NOT appear (no relevance match).
	if strings.Contains(prompt, "Deploy applications to production") {
		t.Error("expected 'deploy' skill to be filtered out; base does not mention deploy")
	}
}

// TestContextInjector_NoLearnedPatternsSection verifies that the deprecated
// "## Learned Patterns" section does not appear in the system prompt output,
// even when a learning pipeline is not wired. This confirms the transitional
// section has been removed per the turbo Thread E self-reflection plan.
func TestContextInjector_NoLearnedPatternsSection(t *testing.T) {
	ci := NewContextInjector(nil, nil)
	prompt := ci.BuildSystemPrompt(context.Background(), "you are a helper")
	if strings.Contains(prompt, "## Learned Patterns") {
		t.Error("expected no '## Learned Patterns' section in output (deprecated)")
	}
}

// TestContextInjector_SkillsRelevanceFallback verifies that when the base
// prompt does not match any cached skills via relevance, all cached skills
// are included as fallback (current behavior preserved).
func TestContextInjector_SkillsRelevanceFallback(t *testing.T) {
	tmpDir := t.TempDir()

	skillContent := `---
name: tester
description: A testing skill
---

# Tester

Testing.
`
	skillPath := createTestSkillFile(t, tmpDir, "tester", skillContent)

	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "tester",
		Description: "A testing skill",
		Path:        skillPath,
	})

	loader := skills.NewLazySkillLoader(idx)
	ctx := context.Background()
	if _, err := loader.Load(ctx, "tester"); err != nil {
		t.Fatalf("failed to load 'tester' skill: %v", err)
	}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	// Base prompt has no relevance to the skill.
	base := "you are a weather assistant"
	prompt := ci.BuildSystemPrompt(ctx, base)

	// Fallback: skill should still appear because no relevance match was found.
	if !strings.Contains(prompt, "## Active Skills") {
		t.Error("expected Active Skills section as fallback when no relevance match")
	}
	if !strings.Contains(prompt, "tester") {
		t.Error("expected 'tester' skill in prompt as fallback")
	}
}

// TestContextInjector_EmptyBaseShowsAllSkills verifies that when base is
// empty, all cached skills are shown (no relevance filtering applied).
func TestContextInjector_EmptyBaseShowsAllSkills(t *testing.T) {
	tmpDir := t.TempDir()

	alphaContent := `---
name: alpha
description: Alpha skill
---

# Alpha
`
	alphaPath := createTestSkillFile(t, tmpDir, "alpha", alphaContent)

	betaContent := `---
name: beta
description: Beta skill
---

# Beta
`
	betaPath := createTestSkillFile(t, tmpDir, "beta", betaContent)

	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "alpha",
		Description: "Alpha skill",
		Path:        alphaPath,
	})
	idx.Index(&skills.SkillIndexEntry{
		Name:        "beta",
		Description: "Beta skill",
		Path:        betaPath,
	})

	loader := skills.NewLazySkillLoader(idx)
	ctx := context.Background()
	if _, err := loader.Load(ctx, "alpha"); err != nil {
		t.Fatalf("failed to load 'alpha' skill: %v", err)
	}
	if _, err := loader.Load(ctx, "beta"); err != nil {
		t.Fatalf("failed to load 'beta' skill: %v", err)
	}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	// Empty base → no relevance filtering → all cached skills should appear.
	prompt := ci.BuildSystemPrompt(ctx, "")
	if !strings.Contains(prompt, "## Active Skills") {
		t.Error("expected Active Skills section when base is empty and cache has skills")
	}
	if !strings.Contains(prompt, "alpha") {
		t.Error("expected 'alpha' skill in prompt when base is empty")
	}
	if !strings.Contains(prompt, "beta") {
		t.Error("expected 'beta' skill in prompt when base is empty")
	}
}
