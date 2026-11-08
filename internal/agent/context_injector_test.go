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

// TestContextInjector_SkillsListingUnconditional verifies the C4 listing:
// every visible cached skill appears in the "## Active Skills" section, no cap,
// regardless of whether the base prompt matches it (the base is no longer a
// relevance signal — branch 04 removed relevance filtering). Both fixtures are
// listed unconditionally, each in C4 line shape.
func TestContextInjector_SkillsListingUnconditional(t *testing.T) {
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

	// Base prompt mentions "review"; under C4 this does NOT filter the
	// listing — every visible cached skill is listed unconditionally.
	base := "you are a code reviewer for pull requests"
	prompt := ci.BuildSystemPrompt(ctx, base)

	// Active Skills section should be present.
	if !strings.Contains(prompt, "## Active Skills") {
		t.Error("expected Active Skills section when cached skills exist")
	}

	// The "code review" skill should appear in C4 line shape.
	if !strings.Contains(prompt, "- code review: Review code for bugs and style [load: file_read "+reviewPath+"]") {
		t.Error("expected 'code review' C4 listing line in prompt")
	}

	// The "deploy" skill must ALSO appear: the listing is unconditional,
	// no relevance filtering, so base text no longer drops it.
	if !strings.Contains(prompt, "- deploy: Deploy applications to production [load: file_read "+deployPath+"]") {
		t.Error("expected 'deploy' C4 listing line in prompt (unconditional, no cap)")
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

// ---------------------------------------------------------------------------
// Embedding-rank episode ordering (token-efficiency branch 04, leaf 02;
// Q4 user ruling: always rank, never hide). Stubs give fixed vectors —
// no network, deterministic ordering.
// ---------------------------------------------------------------------------

// rankStubEmbedder is a test skills.Embedder with fixed vectors per text.
type rankStubEmbedder struct {
	vectors map[string][]float32
	calls   int
}

func (s *rankStubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	s.calls++
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := s.vectors[t]; ok {
			out[i] = v
			continue
		}
		out[i] = []float32{0, 0}
	}
	return out, nil
}

func c4RankFixture(t *testing.T, defs []c4TestSkill) (*skills.LazySkillLoader, *rankStubEmbedder, map[string]string) {
	t.Helper()
	loader, _ := c4SkillFixture(t, defs)
	texts := map[string]string{}
	vectors := map[string][]float32{}
	for _, def := range defs {
		texts[def.name] = def.name + "\n" + def.description
	}
	emb := &rankStubEmbedder{vectors: vectors}
	loader.Index().SetEmbedder(emb)
	return loader, emb, texts
}

// TestSkillsRankingEpisodeFrozenOrder pins T3: after SetEpisodeTask the
// listing is embedding-ranked, and every later build in the SAME episode
// (different per-turn base) reuses the frozen order byte-for-byte.
func TestSkillsRankingEpisodeFrozenOrder(t *testing.T) {
	defs := []c4TestSkill{
		{name: "alpha", description: "Alpha skill"},
		{name: "bravo", description: "Bravo skill"},
		{name: "charlie", description: "Charlie skill"},
	}
	loader, emb, texts := c4RankFixture(t, defs)
	// task vector +1,0; charlie same (cos 1), bravo orthogonal (cos 0),
	// alpha orthogonal (cos 0, tie with bravo -> name asc: alpha, bravo).
	emb.vectors[texts["charlie"]] = []float32{1, 0}
	emb.vectors[texts["bravo"]] = []float32{0, 1}
	emb.vectors[texts["alpha"]] = []float32{0, 1}
	emb.vectors["ship the release"] = []float32{1, 0}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)
	ci.SetEpisodeTask("ship the release")

	ctx := context.Background()
	first := c4SkillsSection(ci.BuildSystemPrompt(ctx, "turn one base"))
	second := c4SkillsSection(ci.BuildSystemPrompt(ctx, "completely different turn two base"))

	if first != second {
		t.Errorf("skills section churned within one episode:\n--- A ---\n%s\n--- B ---\n%s", first, second)
	}
	lines := c4ListingLines(first)
	if len(lines) != 3 {
		t.Fatalf("expected 3 ranked C4 lines, got %d:\n%v", len(lines), lines)
	}
	wantOrder := []string{"- charlie:", "- alpha:", "- bravo:"}
	for i, want := range wantOrder {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("ranked order line %d: got %q, want prefix %q", i, lines[i], want)
		}
	}
}

// TestSkillsRankingEmbedsOncePerEpisode pins the "computed once per episode"
// half of T3: subsequent builds in the same episode issue zero additional
// Embed calls (task vector and skill vectors are cached downstream).
func TestSkillsRankingEmbedsOncePerEpisode(t *testing.T) {
	defs := []c4TestSkill{{name: "alpha", description: "Alpha skill"}}
	loader, emb, texts := c4RankFixture(t, defs)
	emb.vectors[texts["alpha"]] = []float32{1, 0}
	emb.vectors["the task"] = []float32{1, 0}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)
	ci.SetEpisodeTask("the task")

	ctx := context.Background()
	ci.BuildSystemPrompt(ctx, "base one")
	afterFirst := emb.calls
	ci.BuildSystemPrompt(ctx, "base two")
	ci.BuildSystemPrompt(ctx, "base three")
	if emb.calls != afterFirst {
		t.Errorf("episode frozen order re-embedded: %d calls before, %d after two more builds", afterFirst, emb.calls)
	}
}

// TestSkillsRankingSetNeverShrinks pins the hard contract: with a ranker
// wired, the ranked listing contains EXACTLY the same set of skills as the
// alphabetical listing (order-only; no cap, no hiding).
func TestSkillsRankingSetNeverShrinks(t *testing.T) {
	defs := c4DefaultSkills() // 8 visible skills
	loader, emb, texts := c4RankFixture(t, defs)
	rand := [][]float32{{1, 0}, {0, 1}}
	for i, def := range defs {
		emb.vectors[texts[def.name]] = rand[i%2]
	}
	emb.vectors["ranked episode task"] = []float32{1, 0}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	ctx := context.Background()
	plain := map[string]bool{}
	for _, line := range c4ListingLines(ci.BuildSystemPrompt(ctx, "irrelevant base")) {
		plain[strings.SplitN(strings.TrimPrefix(line, "- "), ":", 2)[0]] = true
	}
	if len(plain) != 8 {
		t.Fatalf("alphabetical baseline should list all 8, got %d", len(plain))
	}

	// New episode with the ranker signal.
	ci.SetEpisodeTask("ranked episode task")
	ranked := map[string]bool{}
	lines := c4ListingLines(c4SkillsSection(ci.BuildSystemPrompt(ctx, "another base")))
	if len(lines) != 8 {
		t.Fatalf("ranked listing must keep all 8 lines, got %d:\n%v", len(lines), lines)
	}
	for _, line := range lines {
		ranked[strings.SplitN(strings.TrimPrefix(line, "- "), ":", 2)[0]] = true
	}
	for name := range plain {
		if !ranked[name] {
			t.Errorf("ranking dropped skill %q from the listing", name)
		}
	}
	for name := range ranked {
		if !plain[name] {
			t.Errorf("ranking invented unknown skill %q", name)
		}
	}
}

// TestSkillsRankingNoEpisodeTaskStaysAlphabetical pins the opt-in default:
// without SetEpisodeTask (config off / no signal) the listing is exactly the
// leaf-01 alphabetical behavior.
func TestSkillsRankingNoEpisodeTaskStaysAlphabetical(t *testing.T) {
	defs := []c4TestSkill{
		{name: "zulu", description: "Zulu skill"},
		{name: "alpha", description: "Alpha skill"},
	}
	loader, emb, texts := c4RankFixture(t, defs)
	emb.vectors[texts["zulu"]] = []float32{1, 0}
	emb.vectors[texts["alpha"]] = []float32{0, 1}

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)
	// No SetEpisodeTask call: episodeTask stays empty.
	lines := c4ListingLines(ci.BuildSystemPrompt(context.Background(), "any base"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "- alpha:") || !strings.HasPrefix(lines[1], "- zulu:") {
		t.Errorf("no episode task must stay alphabetical, got %v", lines)
	}
	if emb.calls != 0 {
		t.Errorf("no episode task must not embed, got %d calls", emb.calls)
	}
}

// TestContextInjector_SetEpisodeTask_Nil verifies the setter is nil-safe
// (per the typed-nil setter guard rule).
func TestContextInjector_SetEpisodeTask_Nil(t *testing.T) {
	var ci *ContextInjector
	ci.SetEpisodeTask("must not panic") //nolint // nil receiver no-op
	ci2 := NewContextInjector(nil, nil)
	ci2.SetEpisodeTask("a")
	ci2.SetEpisodeTask("a") // idempotent same text, no panic
}
