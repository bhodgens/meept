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
