package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"gopkg.in/yaml.v3"
)

// sampleAGENTMD is a minimal AGENT.md with a couple of frontmatter fields and
// a body. Used to verify the YAML rewrite preserves structure.
const sampleAGENTMD = `---
id: coder
name: Code Specialist
role: executor
description: Implements, modifies, and maintains code with precision
enabled: true
can_delegate: false
additional_tools:
  - file_read
  - file_write
capabilities:
  - code
  - reasoning
max_iterations: 15
temperature: 0.3
---

# Code Specialist

You implement, modify, and maintain code with precision.
`

// TestRenderReasoningBlock verifies the YAML shape emitted for a given effort
// tier matches the spec (2-space indent, effort + allow_self_modulation).
func TestRenderReasoningBlock(t *testing.T) {
	got := renderReasoningBlock(llm.ReasoningHigh)
	want := `reasoning:
  effort: high
  allow_self_modulation: true
`
	if got != want {
		t.Fatalf("renderReasoningBlock mismatch:\nwant=%q\ngot =%q", want, got)
	}
}

// TestSplitAgentFrontmatter verifies the frontmatter splitter correctly
// handles a typical AGENT.md file.
func TestSplitAgentFrontmatter(t *testing.T) {
	fm, body, err := splitAgentFrontmatter(sampleAGENTMD)
	if err != nil {
		t.Fatalf("splitAgentFrontmatter: %v", err)
	}
	if !strings.Contains(fm, "id: coder") {
		t.Errorf("frontmatter missing id: %q", fm)
	}
	if !strings.Contains(body, "# Code Specialist") {
		t.Errorf("body missing heading: %q", body)
	}
}

// TestInjectReasoningBlock_New verifies that injecting a reasoning block into
// an AGENT.md without one adds the block and preserves all existing fields.
func TestInjectReasoningBlock_New(t *testing.T) {
	out, err := injectReasoningBlockFromText(t, sampleAGENTMD, llm.ReasoningMedium)
	if err != nil {
		t.Fatalf("injectReasoningBlockFromText: %v", err)
	}

	// The original fields must still be present.
	for _, want := range []string{"id: coder", "name: Code Specialist", "additional_tools:", "capabilities:", "max_iterations: 15"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}

	// The reasoning block must be present.
	if !strings.Contains(out, "reasoning:") {
		t.Errorf("output missing reasoning block:\n%s", out)
	}
	if !strings.Contains(out, "effort: medium") {
		t.Errorf("output missing effort: medium:\n%s", out)
	}
	if !strings.Contains(out, "allow_self_modulation: true") {
		t.Errorf("output missing allow_self_modulation: true:\n%s", out)
	}

	// The body must still be present.
	if !strings.Contains(out, "# Code Specialist") {
		t.Errorf("output missing body heading:\n%s", out)
	}

	// Re-parsing the result must yield a valid agent definition with the
	// reasoning block attached. We parse the YAML frontmatter directly to
	// avoid pulling internal/agent (which may be in a broken uncommitted
	// state from other worktrees).
	fm2, _, err := splitAgentFrontmatter(out)
	if err != nil {
		t.Fatalf("re-split frontmatter: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(fm2), &parsed); err != nil {
		t.Fatalf("re-parse YAML frontmatter: %v\nfrontmatter:\n%s", err, fm2)
	}
	reasoning, ok := parsed["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("re-parsed reasoning is not a map: %T", parsed["reasoning"])
	}
	if got := reasoning["effort"]; got != llm.ReasoningMedium {
		t.Errorf("re-parsed effort = %v, want %q", got, llm.ReasoningMedium)
	}
	if got, _ := reasoning["allow_self_modulation"].(bool); !got {
		t.Errorf("re-parsed allow_self_modulation = %v, want true", reasoning["allow_self_modulation"])
	}
}

// TestInjectReasoningBlock_Overwrite verifies that re-injecting replaces the
// existing effort rather than appending a duplicate.
func TestInjectReasoningBlock_Overwrite(t *testing.T) {
	first, err := injectReasoningBlockFromText(t, sampleAGENTMD, llm.ReasoningMedium)
	if err != nil {
		t.Fatalf("first inject: %v", err)
	}
	second, err := injectReasoningBlockFromText(t, first, llm.ReasoningHigh)
	if err != nil {
		t.Fatalf("second inject: %v", err)
	}
	if got := strings.Count(second, "reasoning:"); got != 1 {
		t.Errorf("expected exactly 1 reasoning: block, got %d\noutput:\n%s", got, second)
	}
	if !strings.Contains(second, "effort: high") {
		t.Errorf("output missing effort: high:\n%s", second)
	}
	if strings.Contains(second, "effort: medium") {
		t.Errorf("output still contains old effort: medium:\n%s", second)
	}
}

// TestRoleHeuristic verifies every entry maps to a valid effort tier.
func TestRoleHeuristic(t *testing.T) {
	if len(roleHeuristic) == 0 {
		t.Fatal("roleHeuristic is empty")
	}
	for id, effort := range roleHeuristic {
		if !llm.IsValidEffort(effort) {
			t.Errorf("roleHeuristic[%s] = %q is not a valid effort tier", id, effort)
		}
		if effort == llm.ReasoningNone {
			t.Errorf("roleHeuristic[%s] = none; none disables thinking and is not a sensible default", id)
		}
	}
}

// TestAtomicWriteFile verifies atomicWriteFile overwrites the destination and
// leaves no temp files behind.
func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := atomicWriteFile(dest, []byte("new content")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("content = %q, want %q", string(got), "new content")
	}
	// Check no temp files remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".migrate-reasoning-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// TestDiscoverAgentFiles_EmptyDir verifies discoverAgentFiles returns no
// error and no paths when no agents exist in the bundled/project tiers.
func TestDiscoverAgentFiles_EmptyDir(t *testing.T) {
	// Run from a temp directory with no AGENT.md files. The function scans
	// relative project-local and bundled dirs, so cd there first.
	dir := t.TempDir()
	chdir(t, dir)
	files, err := discoverAgentFiles()
	if err != nil {
		t.Fatalf("discoverAgentFiles: %v", err)
	}
	// ~/.meept/agents may have files from the user's home — filter those out.
	for _, f := range files {
		if strings.Contains(f, dir) {
			t.Errorf("unexpected file under temp dir: %s", f)
		}
	}
}

// injectReasoningBlockFromText is a test helper that runs injectReasoningBlock
// against an in-memory string by writing it to a temp file.
func injectReasoningBlockFromText(t *testing.T, text, effort string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return injectReasoningBlock(path, effort)
}

// chdir changes the working directory for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}
