package agents

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// writeAgentDef writes a minimal AGENT.md with the given addit.
func writeAgentDef(t *testing.T, dir, id, extraFrontmatter string) {
	t.Helper()
	path := filepath.Join(dir, id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	body := "---\nid: " + id + "\nname: " + id + "\nrole: executor\n" + extraFrontmatter + "---\n\nBody for " + id + ".\n"
	if err := os.WriteFile(filepath.Join(path, "AGENT.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
}

// A higher-priority agent definition must not silently drop what the bundled
// definition has gained. Observed 2026-09-13: a Sep 6 copy of the coder
// definition in ~/.meept/agents shadowed the bundled one and lost
// json_extract plus the intents list, so production could not run the tool its
// route aimed at.
func TestDiscovery_ShadowingUnionsAdditiveFields(t *testing.T) {
	userDir := t.TempDir()
	bundledDir := t.TempDir()

	// The user's (higher-priority) copy: no json_extract, no intents, and it
	// keeps one tool of its own.
	writeAgentDef(t, userDir, "coder", `additional_tools:
  - shell_execute
`)
	// The bundled (lower-priority) definition: has the newer tool and the
	// declared lanes.
	writeAgentDef(t, bundledDir, "coder", `additional_tools:
  - file_read
  - json_extract
available_skills:
  - bundled-skill
intents: [code, review, tooluse]
`)

	d := NewDiscovery(
		WithTiers([]DiscoveryTier{{Path: userDir, Priority: PriorityUser}}),
		WithBundledPath(bundledDir),
		WithDiscoveryLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))),
	)
	defs, err := d.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("got %d definitions, want 1", len(defs))
	}
	got := defs[0]

	for _, tool := range []string{"shell_execute", "file_read", "json_extract"} {
		if !contains(got.AdditionalTools, tool) {
			t.Errorf("AdditionalTools = %v, want it to include %q (union with the shadowed definition)", got.AdditionalTools, tool)
		}
	}
	if !contains(got.Intents, "quickplan") && !contains(got.Intents, "code") {
		t.Errorf("Intents = %v, want the bundled intents preserved", got.Intents)
	}
	if !contains(got.AvailableSkills, "bundled-skill") {
		t.Errorf("AvailableSkills = %v, want the bundled skill preserved", got.AvailableSkills)
	}
}

// The user's own values still win for the fields they set: the union must not
// duplicate an entry the user also declares.
func TestDiscovery_ShadowingUnionHasNoDuplicates(t *testing.T) {
	userDir := t.TempDir()
	bundledDir := t.TempDir()

	writeAgentDef(t, userDir, "coder", "additional_tools:\n  - file_read\n")
	writeAgentDef(t, bundledDir, "coder", "additional_tools:\n  - file_read\n  - json_extract\n")

	d := NewDiscovery(
		WithTiers([]DiscoveryTier{{Path: userDir, Priority: PriorityUser}}),
		WithBundledPath(bundledDir),
	)
	defs, err := d.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got := defs[0]
	seen := map[string]int{}
	for _, tool := range got.AdditionalTools {
		seen[tool]++
	}
	for tool, n := range seen {
		if n != 1 {
			t.Errorf("tool %q appears %d times in %v, want 1", tool, n, got.AdditionalTools)
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
