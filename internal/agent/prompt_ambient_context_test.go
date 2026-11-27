package agent

// Pins for issue #193 (ambient per-step context):
//
//  1. Per-section prompt telemetry: one assembly produces one
//     agent.prompt_section.bytes record per non-empty section, including the
//     agents-context section with nonzero bytes, tagged with the section name.
//
//  2. AGENTS.md chain scoping: the ambient injection carries the project-root
//     file and the cwd-ancestor files ONLY. A session working at the root
//     never receives a deep package's AGENTS.md; a session working in that
//     package receives root + package, ordered root-first.
//
// Non-vacuity is proven by the disable-restore probe documented in the fix
// commit (temporarily swapping the loader back to project.LoadAllAgentsMD
// fails TestAgentsContextChainScoping; restore verified byte-identical).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// writeAgentsTree builds a fake project root containing a .git marker (so
// FindProjectRoot stops there), a root AGENTS.md, and a deep-package
// AGENTS.md at pkgA. Returns the root.
func writeAgentsTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /nowhere\n"), 0o644); err != nil {
		t.Fatalf("write .git marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("ROOT CONVENTIONS MARKER\n"), 0o644); err != nil {
		t.Fatalf("write root AGENTS.md: %v", err)
	}
	pkgA := filepath.Join(root, "pkgA")
	if err := os.MkdirAll(pkgA, 0o755); err != nil {
		t.Fatalf("mkdir pkgA: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgA, "AGENTS.md"),
		[]byte("PKGA DEEP INVARIANT MARKER\n"), 0o644); err != nil {
		t.Fatalf("write pkgA AGENTS.md: %v", err)
	}
	return root
}

// TestAgentsContextChainScoping pins the #193 ambient scoping: workingDir at
// the project root must NOT carry the pkgA invariant doc; workingDir inside
// pkgA must carry both files, root-first.
func TestAgentsContextChainScoping(t *testing.T) {
	root := writeAgentsTree(t)
	pkgA := filepath.Join(root, "pkgA")

	// Case 1: session working at the root.
	loop := NewAgentLoop("scope-root-session", root, WithAgentConfig(DefaultAgentConfig()))
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	ctx := loop.loadAgentsContext(root)
	if !strings.Contains(ctx, "ROOT CONVENTIONS MARKER") {
		t.Errorf("root workingDir prompt must contain the root AGENTS.md, got %q", ctx)
	}
	if strings.Contains(ctx, "PKGA DEEP INVARIANT MARKER") {
		t.Errorf("root workingDir prompt must NOT contain deep-package AGENTS.md (whole-tree leak): %q", ctx)
	}

	// Case 2: session working inside pkgA — both files, root-first.
	loopA := NewAgentLoop("scope-pkga-session", pkgA, WithAgentConfig(DefaultAgentConfig()))
	if loopA == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	ctxA := loopA.loadAgentsContext(pkgA)
	rootPos := strings.Index(ctxA, "ROOT CONVENTIONS MARKER")
	pkgPos := strings.Index(ctxA, "PKGA DEEP INVARIANT MARKER")
	if rootPos < 0 {
		t.Errorf("pkgA workingDir prompt must contain the root AGENTS.md, got %q", ctxA)
	}
	if pkgPos < 0 {
		t.Errorf("pkgA workingDir prompt must contain its own AGENTS.md, got %q", ctxA)
	}
	if rootPos >= 0 && pkgPos >= 0 && rootPos > pkgPos {
		t.Errorf("chain order must be root-first: root@%d pkgA@%d", rootPos, pkgPos)
	}
}

// TestAgentsContextEndToEndPrompt pins that the scoped context actually rides
// the assembled system prompt through injectProjectContext + the
// agents-context section (and the 8KB cap applies).
func TestAgentsContextEndToEndPrompt(t *testing.T) {
	root := writeAgentsTree(t)
	pkgA := filepath.Join(root, "pkgA")

	loop := NewAgentLoop("e2e-scope-session", pkgA, WithAgentConfig(DefaultAgentConfig()))
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	conv := NewConversation()
	prompt := loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, nil)
	if !strings.Contains(prompt, "ROOT CONVENTIONS MARKER") || !strings.Contains(prompt, "PKGA DEEP INVARIANT MARKER") {
		t.Errorf("prompt from pkgA must contain root+pkgA AGENTS.md chain")
	}
	if !strings.Contains(prompt, "<agents-context>") {
		t.Errorf("agents context must ride the dedicated <agents-context> component")
	}

	// Same loop re-pointed at the root: the deep doc must disappear.
	loop.SetWorkingDir(root)
	promptRoot := loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, nil)
	if strings.Contains(promptRoot, "PKGA DEEP INVARIANT MARKER") {
		t.Errorf("root-session prompt leaked deep-package invariant doc: %q",
			tail(promptRoot, "PKGA DEEP INVARIANT MARKER"))
	}
}

// TestAgentsContextSizeCap pins the 8KB total cap on the joined chain.
func TestAgentsContextSizeCap(t *testing.T) {
	root := writeAgentsTree(t)
	// Inflate the root file past the cap.
	huge := strings.Repeat("H", maxAgentsContextBytes*2) + "\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(huge), 0o644); err != nil {
		t.Fatalf("inflate root AGENTS.md: %v", err)
	}
	loop := NewAgentLoop("cap-session", root, WithAgentConfig(DefaultAgentConfig()))
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	ctx := loop.loadAgentsContext(root)
	if len(ctx) > maxAgentsContextBytes+1024 { // cap + truncation marker slack
		t.Errorf("joined agents context exceeded cap: %d bytes", len(ctx))
	}
	if !strings.Contains(ctx, "truncated at 8KB") {
		t.Errorf("expected truncation marker, got tail %q", tail(ctx, "truncated"))
	}
}

// TestAgentsContextWholeTreeEscapeHatch pins the opt-in restore of the
// pre-#193 whole-tree behavior (the flag that keeps
// project.LoadAllAgentsMD wired on a production path).
func TestAgentsContextWholeTreeEscapeHatch(t *testing.T) {
	root := writeAgentsTree(t)
	cfg := DefaultAgentConfig()
	cfg.AgentsContextWholeTree = true
	loop := NewAgentLoop("whole-tree-session", root, WithAgentConfig(cfg))
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	ctx := loop.loadAgentsContext(root)
	if !strings.Contains(ctx, "PKGA DEEP INVARIANT MARKER") {
		t.Errorf("escape hatch must restore whole-tree loading, got %q", ctx)
	}
}

// TestPromptSectionTelemetry pins deliverable 1: one assembly produces
// per-section byte records including a nonzero agents-context entry.
func TestPromptSectionTelemetry(t *testing.T) {
	root := writeAgentsTree(t)
	rec := &fakeTurnMetricRecorder{}
	loop := NewAgentLoop("telemetry-session", root,
		WithAgentConfig(DefaultAgentConfig()),
		WithPromptSectionRecorder(rec))
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	loop.SetPromptSectionRecorder(rec) // setter lane must be idempotent-safe

	conv := NewConversation()
	loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, nil)

	seen := map[string]float64{}
	for _, m := range rec.records {
		if m.name != PromptSectionBytesMetric {
			t.Errorf("unexpected metric name %q", m.name)
			continue
		}
		if m.tags["section"] == "" {
			t.Error("telemetry record missing section tag")
		}
		if m.tags["session_id"] != "telemetry-session" {
			t.Errorf("session_id tag = %q", m.tags["session_id"])
		}
		seen[m.tags["section"]] = m.value
	}
	// Multiple distinct sections must be recorded, not just one blob.
	if len(seen) < 3 {
		t.Fatalf("expected per-section records for >=3 sections, got %d: %v", len(seen), sectionNames(seen))
	}
	if b, ok := seen["agents-context"]; !ok || b <= 0 {
		t.Errorf("expected nonzero agents-context section record, got present=%v bytes=%v", ok, b)
	}
}

// --- Increment 2 pins (issue #193): bounded skill bodies + CLAUDE.md ------

// writeSkillFixture builds a temp dir with one real SKILL.md (frontmatter +
// a recognizable body marker) and a lazy loader over an index entry that
// points at it. Returns (loader, entry).
func writeSkillFixture(t *testing.T) (*skills.LazySkillLoader, *skills.SkillIndexEntry) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pin-skill.md")
	content := "---\nname: pin-skill\ndescription: A skill pinned by #193 increment 2\n---\n\nEAGER SKILL BODY MARKER\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	idx := skills.NewSkillIndex()
	entry := &skills.SkillIndexEntry{
		Name:        "pin-skill",
		Description: "A skill pinned by #193 increment 2",
		Path:        path,
	}
	idx.Index(entry)
	return skills.NewLazySkillLoader(idx), entry
}

// TestSkillContextEagerBodiesDefault pins the H5 flipped default
// (bughunt 2026-11-27): a DEFAULT-config loop reads full bodies into the
// prompt (the #193 index-only default pointed file_read at paths the
// per-session jail refuses — the posture and the fence were coupled).
// The index-only shape is pinned as the explicit opt-out in
// TestSkillContextIndexOnlyOptOut below.
func TestSkillContextEagerBodiesDefault(t *testing.T) {
	loader, entry := writeSkillFixture(t)
	loop := NewAgentLoop("skill-index-session", "/tmp",
		WithAgentConfig(DefaultAgentConfig()),
		WithSkillLoader(loader),
	)
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	discovered := []*DiscoveredSkill{{Entry: entry, Confidence: 0.9}}

	section := loop.buildSkillContextSection(context.Background(), discovered)
	if section == "" {
		t.Fatal("default posture must surface the skill")
	}
	if !strings.Contains(section, "EAGER SKILL BODY MARKER") {
		t.Errorf("default posture must carry the full skill body, got %q", tail(section, "Relevant Skills"))
	}
	if stats := loader.Stats(); stats.Loads == 0 {
		t.Errorf("default posture must read the body via the loader: Loads=%d", stats.Loads)
	}
}

// TestSkillContextEagerBodiesEscapeHatch pins the eager-body DEFAULT
// (bughunt H5, 2026-11-27): the default config reads full bodies and the
// prompt carries them — exactly as pre-#193-increment-2. (Renamed from
// the old opt-in pin when the default flipped: this is now the default-
// TestSkillContextIndexOnlyOptOut pins the H5 escape hatch in BOTH
// directions: with AgentConfig.SkillContextIndexOnly set, surfaced skills
// ride the prompt as index entries with the [load:] pointer and the loader
// NEVER reads a body (the #193 increment-2 posture, now opt-in-only). The
// telemetry end-to-end half of the old index-only pin rides along: the
// section classifies under the "skills" kind either posture.
func TestSkillContextIndexOnlyOptOut(t *testing.T) {
	loader, entry := writeSkillFixture(t)
	cfg := DefaultAgentConfig()
	cfg.SkillContextIndexOnly = true
	loop := NewAgentLoop("skill-index-optout", "/tmp",
		WithAgentConfig(cfg),
		WithSkillLoader(loader),
	)
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	discovered := []*DiscoveredSkill{{Entry: entry, Confidence: 0.9}}

	section := loop.buildSkillContextSection(context.Background(), discovered)
	if section == "" {
		t.Fatal("index-only opt-out must still surface the skill")
	}
	if strings.Contains(section, "EAGER SKILL BODY MARKER") {
		t.Errorf("index-only opt-out leaked a full skill body: %q", tail(section, "EAGER"))
	}
	pointer := "[load: file_read " + entry.Path + "]"
	if !strings.Contains(section, pointer) {
		t.Errorf("index entry must carry the load pointer %q, got %q", pointer, section)
	}
	if stats := loader.Stats(); stats.Loads != 0 {
		t.Errorf("index-only opt-out must not read bodies: loader Loads=%d", stats.Loads)
	}

	// End-to-end (carried from the old index-only pin): the section rides
	// the assembled prompt and telemetry classifies it under the
	// "skills" kind (#193 increment 2 taxonomy).
	rec := &fakeTurnMetricRecorder{}
	loop.SetPromptSectionRecorder(rec)
	conv := NewConversation()
	prompt := loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, discovered)
	if !strings.Contains(prompt, pointer) {
		t.Errorf("assembled prompt missing skill index entry: %q", tail(prompt, "Relevant Skills"))
	}
	var skillsBytes float64
	var haveSkills bool
	for _, m := range rec.records {
		if m.name == PromptSectionBytesMetric && m.tags["section"] == "skills" {
			skillsBytes, haveSkills = m.value, true
		}
	}
	if !haveSkills || skillsBytes <= 0 {
		t.Errorf("expected nonzero 'skills' section telemetry record, got present=%v bytes=%v", haveSkills, skillsBytes)
	}
}

// TestArtifactContextCLAUDEMDBudgeted pins the #193 increment-2 CLAUDE.md
// posture end-to-end: an over-budget CLAUDE.md produces a BOUNDED
// "Artifact Context" section in the system prompt, and the telemetry
// record shows the shrink versus the whole verbatim dump. The
// ArtifactContextWholeDoc escape hatch restores the unbounded bytes.
func TestArtifactContextCLAUDEMDBudgeted(t *testing.T) {
	dir := t.TempDir()
	// ~120KB of CLAUDE.md: far over DefaultDocBudgetTokens under any
	// reasonable counter.
	huge := "CLAUDE DOC HEAD MARKER\n" + strings.Repeat("binding project instruction line\n", 4000)
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(huge), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	am := NewArtifactManager(nil)
	whole := am.BuildFullArtifactContext("", dir) // pre-increment-2 dump (escape hatch shape)
	if !strings.Contains(whole, "CLAUDE DOC HEAD MARKER") {
		t.Fatal("fixture: whole dump must carry the doc")
	}

	rec := &fakeTurnMetricRecorder{}
	loop := NewAgentLoop("claude-budget-session", dir,
		WithAgentConfig(DefaultAgentConfig()),
		WithArtifactManager(am),
		WithPromptSectionRecorder(rec),
	)
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	conv := NewConversation()
	prompt := loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, nil)

	if !strings.Contains(prompt, "CLAUDE DOC HEAD MARKER") {
		t.Errorf("budgeted artifact section must still lead with the doc head: %q", tail(prompt, "CLAUDE.md"))
	}
	if !noticeRe.MatchString(prompt) {
		t.Errorf("over-budget CLAUDE.md must carry the truncation notice in the prompt")
	}
	if strings.Contains(prompt, strings.Repeat("binding project instruction line\n", 200)) {
		t.Error("prompt still carries the unbounded verbatim dump")
	}

	// Telemetry shows the shrink: the artifact-context section record is
	// present, nonzero, and materially smaller than the whole dump.
	var artifactBytes float64
	var have bool
	for _, m := range rec.records {
		if m.name == PromptSectionBytesMetric && m.tags["section"] == "artifact-context" {
			artifactBytes, have = m.value, true
		}
	}
	if !have || artifactBytes <= 0 {
		t.Fatalf("expected nonzero 'artifact-context' telemetry record, got present=%v bytes=%v", have, artifactBytes)
	}
	if artifactBytes >= float64(len(whole))/2 {
		t.Errorf("telemetry does not show the shrink: section=%d whole dump=%d bytes",
			int(artifactBytes), len(whole))
	}

	// Escape hatch: ArtifactContextWholeDoc restores the verbatim dump.
	cfg := DefaultAgentConfig()
	cfg.ArtifactContextWholeDoc = true
	loopHatch := NewAgentLoop("claude-hatch-session", dir,
		WithAgentConfig(cfg),
		WithArtifactManager(NewArtifactManager(nil)),
	)
	if loopHatch == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	promptHatch := loopHatch.buildSystemPromptWithContextAndSkills(context.Background(), NewConversation(), nil)
	if !strings.Contains(promptHatch, strings.Repeat("binding project instruction line\n", 200)) {
		t.Error("escape hatch must restore the whole verbatim CLAUDE.md dump")
	}
	if noticeRe.MatchString(promptHatch) {
		t.Error("escape hatch must NOT truncate")
	}
}

// tail returns the surroundings of needle in s for failure messages.
func tail(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return "<absent>"
	}
	lo := i - 40
	if lo < 0 {
		lo = 0
	}
	hi := i + len(needle) + 40
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

func sectionNames(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
