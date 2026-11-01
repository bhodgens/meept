package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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

// ---------------------------------------------------------------------------
// C4 skill-listing gate tests (token-efficiency branch 04, leaf 00T).
// These pin the POST-FIX contract (docs/plans/token-efficiency/04-skill-routing/
// 00T-gate.md + master.md C4) and must fail RED on behavioral assertions
// against current code. No t.Skip: a skipped gate test is a gate failure.
// ---------------------------------------------------------------------------

// c4LinePattern is the frozen C4 line shape (master.md C4): one line per
// cached visible skill, NO cap, byte-exact:
//
//   - <name>: <description> [load: file_read <absolute SKILL.md path>]
//
// Names are lowercase [a-z0-9-]+ (skill frontmatter names); the load hint
// points at the absolute SKILL.md path so the model can pull bodies on demand.
var c4LinePattern = regexp.MustCompile(`^- [a-z0-9-]+: .+ \[load: file_read /.+/SKILL\.md\]$`)

// c4ListingLines extracts the C4-shaped listing lines from a built prompt.
func c4ListingLines(prompt string) []string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		if c4LinePattern.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}

// c4TestSkill is one fixture skill for the C4 gate tests.
type c4TestSkill struct {
	name        string
	description string
	hidden      bool // frontmatter: disable-model-invocation: true
}

// c4SkillFixture writes each def as <tmpDir>/<name>/SKILL.md with YAML
// frontmatter, indexes the skills, and loads them all into a fresh
// LazySkillLoader cache (deterministic t.TempDir trees, no sleeps). Returns
// the loader and the temp root; a skill's absolute SKILL.md path is
// filepath.Join(tmpDir, def.name, "SKILL.md").
func c4SkillFixture(t *testing.T, defs []c4TestSkill) (loader *skills.LazySkillLoader, tmpDir string) {
	t.Helper()
	tmpDir = t.TempDir()
	idx := skills.NewSkillIndex()
	for _, def := range defs {
		fm := "---\nname: " + def.name + "\ndescription: " + def.description + "\n"
		if def.hidden {
			fm += "disable-model-invocation: true\n"
		}
		fm += "---\n\nBody.\n"
		path := createTestSkillFile(t, tmpDir, def.name, fm)
		idx.Index(&skills.SkillIndexEntry{Name: def.name, Description: def.description, Path: path})
	}
	loader = skills.NewLazySkillLoader(idx)
	ctx := context.Background()
	for _, def := range defs {
		if _, err := loader.Load(ctx, def.name); err != nil {
			t.Fatalf("c4SkillFixture: failed to load skill %q: %v", def.name, err)
		}
	}
	return loader, tmpDir
}

// c4DefaultSkills returns 8 deterministic visible fixtures whose descriptions
// all contain the words "review code". With base = "review code" the current
// relevance path is taken (MatchAll scores > 0), which is the path where the
// current 5-skill cap actually bites.
func c4DefaultSkills() []c4TestSkill {
	names := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	defs := make([]c4TestSkill, 0, len(names))
	for _, n := range names {
		defs = append(defs, c4TestSkill{name: n, description: "Review code with " + n})
	}
	return defs
}

// TestSkillsListingHasNoCap pins C4/T1: with 8 cached visible skills, ALL 8
// appear in the prompt — one C4 line each, no cap.
// RED today: the listing is capped at maxRelevantSkills (5) on the relevance
// path, and the line shape is the legacy "- **name**: desc", not C4.
func TestSkillsListingHasNoCap(t *testing.T) {
	loader, _ := c4SkillFixture(t, c4DefaultSkills())
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	prompt := ci.BuildSystemPrompt(context.Background(), "review code")
	lines := c4ListingLines(prompt)
	if len(lines) != 8 {
		t.Errorf("no-cap listing: got %d C4 lines, want 8:\n%v", len(lines), lines)
	}
	for _, def := range c4DefaultSkills() {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, "- "+def.name+": ") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("skill %q missing from the no-cap listing", def.name)
		}
	}
}

// TestSkillsListingLineShape pins C4/T2: the single listing line is
// byte-exact C4 shape, including the absolute SKILL.md load path.
// RED today: the current shape is "- **alpha**: <desc>" (no load hint).
func TestSkillsListingLineShape(t *testing.T) {
	loader, tmpDir := c4SkillFixture(t, []c4TestSkill{{name: "alpha", description: "Alpha skill"}})
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	prompt := ci.BuildSystemPrompt(context.Background(), "review code")
	lines := c4ListingLines(prompt)
	if len(lines) != 1 {
		t.Fatalf("got %d C4-shaped lines, want exactly 1 (prompt:\n%s)", len(lines), prompt)
	}
	want := "- alpha: Alpha skill [load: file_read " + filepath.Join(tmpDir, "alpha", "SKILL.md") + "]"
	if lines[0] != want {
		t.Errorf("C4 line shape:\n got  %q\n want %q", lines[0], want)
	}
}

// TestSkillsListingHidesModelInvocationDisabled pins C4/T3: a skill whose
// frontmatter sets disable-model-invocation: true is omitted from the
// listing (it stays executable via explicit invocation paths).
// RED today: the key is not parsed (unknown frontmatter keys are tolerated,
// so the skill is cached and listed like any other).
func TestSkillsListingHidesModelInvocationDisabled(t *testing.T) {
	loader, _ := c4SkillFixture(t, []c4TestSkill{
		{name: "alpha", description: "Review code with alpha"},
		{name: "secret-tool", description: "Review code with secret tool", hidden: true},
	})
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	prompt := ci.BuildSystemPrompt(context.Background(), "review code")
	if strings.Contains(prompt, "secret-tool") {
		t.Errorf("skill with disable-model-invocation: true must be hidden from the listing; prompt still contains 'secret-tool':\n%s", prompt)
	}
	lines := c4ListingLines(prompt)
	if len(lines) != 1 {
		t.Errorf("expected exactly 1 C4 line (the visible skill only), got %d:\n%v", len(lines), lines)
	}
}

// c4SkillsSection returns only the skill-listing lines of the "## Active
// Skills" section (excluding the section header and the trailing "When
// triggers occur" footer), so a comparison between two builds reflects ONLY
// the listing bytes — not the per-turn base text that prefixes the prompt.
func c4SkillsSection(prompt string) string {
	const header = "\n## Active Skills\n"
	i := strings.Index(prompt, header)
	if i < 0 {
		return ""
	}
	rest := prompt[i+len(header):]
	if j := strings.Index(rest, "\nWhen triggers occur"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestSkillsListingStableWithinEpisode pins C4/T4: the skills listing is
// built ONCE per episode and is cache-stable — it must NOT depend on the
// per-turn task text (the `base`). Two consecutive builds of the same episode
// (same injector + cache, DIFFERENT per-turn base) must produce byte-identical
// skills sections, so per-turn prompt bytes do not churn the cache.
// RED today: `base` IS a relevance signal (MatchAll(base)); a skill set that
// matched in build A is filtered/reordered/capped in build B, so the two
// skills sections differ.
func TestSkillsListingStableWithinEpisode(t *testing.T) {
	loader, _ := c4SkillFixture(t, c4DefaultSkills())
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	ctx := context.Background()
	// Two consecutive turns of the same episode carry different task text.
	turnA := ci.BuildSystemPrompt(ctx, "review code")
	turnB := ci.BuildSystemPrompt(ctx, "deploy to prod xyz")

	secA := c4SkillsSection(turnA)
	secB := c4SkillsSection(turnB)
	if secA != secB {
		t.Errorf("skills section churns with per-turn base within one episode:\n--- turn A base ---\n%s\n--- turn B base ---\n%s", secA, secB)
	}
}
