package agents

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestLogger returns a Discard-handler logger so tests don't emit log noise.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// writeComponent writes a markdown file with optional HTML-comment frontmatter
// into the given prompts root.
func writeComponent(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func TestComponentIDFromRel(t *testing.T) {
	cases := map[string]string{
		"base/constitution.md":     "base.constitution",
		"capabilities/memory.md":   "capabilities.memory",
		"conditional/source_eval.md": "conditional.source_eval",
		"tools/web.md":             "tools.web",
		"top.md":                   "top",
	}
	for rel, want := range cases {
		got := componentIDFromRel(rel)
		if got != want {
			t.Errorf("componentIDFromRel(%q)=%q want %q", rel, got, want)
		}
	}
}

func TestComponentTitleFromID(t *testing.T) {
	cases := map[string]string{
		"base.constitution":          "Constitution",
		"capabilities.memory":        "Memory",
		"conditional.source_evaluation": "Source Evaluation",
		"base.task_principles":       "Task Principles",
	}
	for id, want := range cases {
		got := componentTitleFromID(id)
		if got != want {
			t.Errorf("componentTitleFromID(%q)=%q want %q", id, got, want)
		}
	}
}

func TestStripHTMLCommentFrontmatter(t *testing.T) {
	in := "<!--\nname: foo\nagent_types: [researcher]\n-->\n\n# Body\n\nContent."
	out := stripHTMLCommentFrontmatter(in)
	if out != "# Body\n\nContent." {
		t.Errorf("stripHTMLCommentFrontmatter: got %q", out)
	}

	// No frontmatter → unchanged.
	plain := "# Just a body"
	if got := stripHTMLCommentFrontmatter(plain); got != plain {
		t.Errorf("stripHTMLCommentFrontmatter on plain: got %q", got)
	}
}

func TestComponentRegistryDiscoverAndResolve(t *testing.T) {
	root := t.TempDir()
	writeComponent(t, root, "base/constitution.md", "# Constitution\n\nBe good.")
	writeComponent(t, root, "base/restrictions.md", "# Restrictions\n\nStay safe.")
	writeComponent(t, root, "capabilities/memory.md", "<!-- old metadata -->\n# Memory\n\nUse memory tools.")
	writeComponent(t, root, "conditional/source_evaluation.md", "# Source Evaluation\n\nCheck sources.")

	// Single tier; emulate the bundled tier by using priority 3.
	reg := &ComponentRegistry{
		components: make(map[string]string),
	}
	// Inject a logger to avoid default slog noise in tests.
	reg.logger = newTestLogger(t)
	reg.discover(DiscoveryTier{Path: root, Priority: PriorityBundled})

	// All four IDs should be discovered.
	wantIDs := []string{
		"base.constitution",
		"base.restrictions",
		"capabilities.memory",
		"conditional.source_evaluation",
	}
	gotIDs := reg.IDs()
	for _, w := range wantIDs {
		found := false
		for _, g := range gotIDs {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected component %q in discovered IDs %v", w, gotIDs)
		}
	}

	// Resolve preserves order and strips HTML comment frontmatter.
	sections := reg.Resolve([]string{"base.constitution", "capabilities.memory"})
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Title != "Constitution" {
		t.Errorf("section[0] title = %q want Constitution", sections[0].Title)
	}
	if sections[1].Title != "Memory" {
		t.Errorf("section[1] title = %q want Memory", sections[1].Title)
	}
	// capabilities.memory had a leading HTML-comment frontmatter that must
	// be stripped — the resolved content should start with "# Memory".
	if want := "# Memory"; len(sections[1].Content) < len(want) || sections[1].Content[:len(want)] != want {
		t.Errorf("section[1] content should start with %q, got %q", want, sections[1].Content)
	}
}

func TestComponentRegistryMissingIDSkipped(t *testing.T) {
	root := t.TempDir()
	writeComponent(t, root, "base/constitution.md", "# Constitution")
	reg := &ComponentRegistry{components: make(map[string]string), logger: newTestLogger(t)}
	reg.discover(DiscoveryTier{Path: root, Priority: PriorityBundled})

	sections := reg.Resolve([]string{"base.constitution", "nonexistent.thing"})
	if len(sections) != 1 {
		t.Fatalf("expected 1 section (missing skipped), got %d", len(sections))
	}
	if sections[0].Title != "Constitution" {
		t.Errorf("section[0] title = %q want Constitution", sections[0].Title)
	}
}

func TestComponentRegistryShadowing(t *testing.T) {
	low := t.TempDir()
	high := t.TempDir()
	// Lower-priority (bundled, priority=3) tier.
	writeComponent(t, low, "base/constitution.md", "# Bundled Constitution")
	// Higher-priority (project, priority=0) tier overrides.
	writeComponent(t, high, "base/constitution.md", "# Project Constitution")

	reg := &ComponentRegistry{components: make(map[string]string), logger: newTestLogger(t)}
	// Scan bundled first (priority 3), then project (priority 0).
	reg.discover(DiscoveryTier{Path: low, Priority: PriorityBundled})
	reg.discover(DiscoveryTier{Path: high, Priority: PriorityProject})

	sections := reg.Resolve([]string{"base.constitution"})
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Content != "# Project Constitution" {
		t.Errorf("expected project override, got %q", sections[0].Content)
	}
}

func TestNewDefaultComponentRegistryBundledOnly(t *testing.T) {
	// Use a temp bundled path with a unique component name so user/system
	// dirs (which may exist on the dev machine running this test) can't
	// shadow what we write.
	bundled := t.TempDir()
	writeComponent(t, bundled, "testonly/unique_component.md", "# Unique")

	reg := NewDefaultComponentRegistry(bundled, nil)
	sections := reg.Resolve([]string{"testonly.unique_component"})
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d (count=%d)", len(sections), reg.Count())
	}
	if sections[0].Content != "# Unique" {
		t.Errorf("Resolve(testonly.unique_component) content = %q, want %q", sections[0].Content, "# Unique")
	}
	if sections[0].Title != "Unique Component" {
		t.Errorf("title = %q want Unique Component", sections[0].Title)
	}
}

func TestResolveNilSafe(t *testing.T) {
	var reg *ComponentRegistry
	if got := reg.Resolve([]string{"any.thing"}); got != nil {
		t.Errorf("nil registry Resolve = %v, want nil", got)
	}
	r := &ComponentRegistry{components: make(map[string]string), logger: newTestLogger(t)}
	if got := r.Resolve(nil); got != nil {
		t.Errorf("Resolve(nil) = %v, want nil", got)
	}
}

// TestBundledComponentsAllDiscovered verifies the repo's bundled config/prompts/
// directory exposes all 16 expected component IDs. This guards against accidental
// deletions or renames that would silently break assembled system prompts.
func TestBundledComponentsAllDiscovered(t *testing.T) {
	bundled := "../../config/prompts"
	if _, err := os.Stat(bundled); err != nil {
		t.Skipf("bundled prompts dir not available: %s", bundled)
	}
	reg := NewDefaultComponentRegistry(bundled, newTestLogger(t))

	want := []string{
		"base.constitution",
		"base.restrictions",
		"base.task_principles",
		"conditional.code_style",
		"conditional.error_context",
		"conditional.source_evaluation",
		"conditional.analysis_depth",
		"conditional.task_decomposition",
		"conditional.git_safety",
		"capabilities.memory",
		"capabilities.tasks",
		"capabilities.platform",
		"tools.bash",
		"tools.file_ops",
		"tools.web",
		"tools.git",
	}
	got := reg.IDs()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected bundled component %q in discovered IDs %v", w, got)
		}
	}

	// Each declared component should resolve to non-empty content.
	sections := reg.Resolve(want)
	if len(sections) != len(want) {
		t.Fatalf("Resolve(%d ids) returned %d sections; want %d", len(want), len(sections), len(want))
	}
	for i, s := range sections {
		if strings.TrimSpace(s.Content) == "" {
			t.Errorf("component %q has empty content", want[i])
		}
	}
}
