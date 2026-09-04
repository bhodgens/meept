package skills

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// capturingHandler records (level, message, path) tuples emitted through a
// slog.Logger so tests can assert exact warn/debug counts.
type capturingHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

type capturedRecord struct {
	level slog.Level
	msg   string
	path  string
}

func (h *capturingHandler) Enabled(_ context.Context, level slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec := capturedRecord{level: r.Level, msg: r.Message}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "path" {
			rec.path = a.Value.String()
		}
		return true
	})
	h.records = append(h.records, rec)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func (h *capturingHandler) count(level slog.Level, path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.level == level && r.path == path {
			n++

		}
	}
	return n
}

func newTestLogger(h *capturingHandler) *slog.Logger {
	return slog.New(h)
}

// TestClaudeSource_ParseFailureWarnDedupe verifies that repeated parse
// failures for the same skill path log WARN once and DEBUG afterwards.
func TestClaudeSource_ParseFailureWarnDedupe(t *testing.T) {
	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "bad-skill.md")
	// allowed-tools as a non-list, non-scalar value (a bare map) is a hard
	// YAML type error that survives the stringList tolerance.
	content := "---\nname: bad-skill\ndescription: broken\nallowed-tools:\n  foo: bar\n---\nBody.\n"
	//nolint:gosec // test file
	if err := os.WriteFile(badPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write bad skill: %v", err)
	}

	handler := &capturingHandler{}
	src := NewClaudeSourceWithPath(tmpDir, newTestLogger(handler))

	for i := 0; i < 3; i++ {
		if _, err := src.Discover(context.Background()); err != nil {
			t.Fatalf("Discover pass %d: %v", i, err)
		}
	}

	if got := handler.count(slog.LevelWarn, badPath); got != 1 {
		t.Errorf("WARN lines for %s = %d, want 1", badPath, got)
	}
	if got := handler.count(slog.LevelDebug, badPath); got < 1 {
		t.Errorf("DEBUG lines for %s = %d, want >= 1", badPath, got)
	}
}

// TestClaudeSource_ParseFailureWarnDedupe_DifferentPaths ensures dedupe is
// per-path: distinct failing skills each get their own WARN.
func TestClaudeSource_ParseFailureWarnDedupe_DifferentPaths(t *testing.T) {
	tmpDir := t.TempDir()
	paths := []string{
		filepath.Join(tmpDir, "bad-one.md"),
		filepath.Join(tmpDir, "bad-two.md"),
	}
	for _, p := range paths {
		content := "---\nname: broken\ndescription: broken\nallowed-tools:\n  foo: bar\n---\nBody.\n"
		//nolint:gosec // test file
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	handler := &capturingHandler{}
	src := NewClaudeSourceWithPath(tmpDir, newTestLogger(handler))

	for i := 0; i < 2; i++ {
		if _, err := src.Discover(context.Background()); err != nil {
			t.Fatalf("Discover pass %d: %v", i, err)
		}
	}

	for _, p := range paths {
		if got := handler.count(slog.LevelWarn, p); got != 1 {
			t.Errorf("WARN lines for %s = %d, want 1", p, got)
		}
	}
}

func TestClaudeSource_Name(t *testing.T) {
	src := NewClaudeSource(nil)
	if got := src.Name(); got != "claude" {
		t.Errorf("ClaudeSource.Name() = %q, want %q", got, "claude")
	}
}

func TestClaudeSource_Discover_NonexistentDirectory(t *testing.T) {
	src := NewClaudeSourceWithPath("/nonexistent/claude/skills", nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover should not fail for nonexistent dir: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}

func TestClaudeSource_Discover_FlatAndDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-discover-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create flat skill
	flatContent := `---
name: claude-flat
description: A flat Claude skill
trigger: graphify
---
Instructions for graphify.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "claude-flat.md"), []byte(flatContent), 0o644); err != nil {
		t.Fatalf("Failed to write flat skill: %v", err)
	}

	// Create directory skill
	dirSkill := filepath.Join(tmpDir, "claude-dir")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(dirSkill, 0o755); err != nil {
		t.Fatalf("Failed to create skill subdir: %v", err)
	}

	dirContent := `---
name: claude-dir
description: A directory Claude skill
---
Instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(dirSkill, "SKILL.md"), []byte(dirContent), 0o644); err != nil {
		t.Fatalf("Failed to write dir skill: %v", err)
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("Expected 2 skills, got %d", len(skills))
	}

	// Check that the flat skill was adapted (trigger -> tags)
	foundFlat := false
	for _, s := range skills {
		if s.Name == "claude-flat" {
			foundFlat = true
			if s.Priority != PriorityClaude {
				t.Errorf("Priority = %d, want %d", s.Priority, PriorityClaude)
			}
			// Trigger should be mapped to Tags by the parser
			hasGraphify := false
			for _, tag := range s.Tags {
				if tag == "graphify" {
					hasGraphify = true
					break
				}
			}
			if !hasGraphify {
				t.Error("Expected 'graphify' tag from trigger field")
			}
		}
	}
	if !foundFlat {
		t.Error("Missing claude-flat skill")
	}
}

func TestClaudeSource_Discover_AdapterApplied(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-adapter-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillContent := `---
name: test-adapter
description: Test skill for adapter
---
Body content.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "test-adapter.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill: %v", err)
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	if skills[0].Name != "test-adapter" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "test-adapter")
	}

	// The adapter should have been applied (currently a no-op, but the path is exercised)
	if skills[0].Priority != PriorityClaude {
		t.Errorf("Priority = %d, want %d (ClaudeSource should set Claude priority)", skills[0].Priority, PriorityClaude)
	}
}

func TestClaudeSource_Discover_ExcludesReadme(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-readme-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	readme := `---
name: readme-skill
description: Should be excluded
---
Not a real skill.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	realSkill := `---
name: claude-real
description: A real Claude skill
---
Instructions.
`
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filepath.Join(tmpDir, "claude-real.md"), []byte(realSkill), 0o644); err != nil {
		t.Fatalf("Failed to write real skill: %v", err)
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("Expected 1 skill (README excluded), got %d", len(skills))
	}
	if skills[0].Name != "claude-real" {
		t.Errorf("Name = %q, want %q", skills[0].Name, "claude-real")
	}
}

func TestClaudeSource_Discover_Sorted(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-sort-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, name := range []string{"zebra", "alpha", "mike"} {
		content := `---
name: ` + name + `
description: Test
---
Body.
`
		//nolint:gosec // test directory/file
		if err := os.WriteFile(filepath.Join(tmpDir, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write skill: %v", err)
		}
	}

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(skills) != 3 {
		t.Fatalf("Expected 3 skills, got %d", len(skills))
	}

	// Should be sorted by name
	if skills[0].Name != "alpha" || skills[1].Name != "mike" || skills[2].Name != "zebra" {
		t.Errorf("Skills not sorted: got %v", []string{skills[0].Name, skills[1].Name, skills[2].Name})
	}
}

func TestClaudeSource_NilLogger(t *testing.T) {
	src := NewClaudeSource(nil)
	if src == nil {
		t.Fatal("NewClaudeSource returned nil")
	}
	if src.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", src.Name(), "claude")
	}
}

func TestClaudeSource_Discover_PathIsFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "claudesource-file-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	src := NewClaudeSourceWithPath(tmpFile.Name(), nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover should not fail when path is a file: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills when path is a file, got %d", len(skills))
	}
}

func TestClaudeSource_Discover_EmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claudesource-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	src := NewClaudeSourceWithPath(tmpDir, nil)

	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}
