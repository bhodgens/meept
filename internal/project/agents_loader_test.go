package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAgentsMDForPath_EmptyProjectRoot(t *testing.T) {
	loaded, err := LoadAgentsMDForPath("", "some/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected empty, got %d results", len(loaded))
	}
}

func TestLoadAgentsMDForPath_EmptyFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	content := "# Root AGENTS.md\nSome content here."
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := LoadAgentsMDForPath(tmpDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 result, got %d", len(loaded))
	}
	if loaded[0].RelPath != "" {
		t.Errorf("expected empty relPath, got %q", loaded[0].RelPath)
	}
	if loaded[0].Content != content {
		t.Errorf("content mismatch: got %q", loaded[0].Content)
	}
}

func TestLoadAgentsMDForPath_EmptyFilePath_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	loaded, err := LoadAgentsMDForPath(tmpDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 results, got %d", len(loaded))
	}
}

func TestLoadAgentsMDForPath_Hierarchical(t *testing.T) {
	tmpDir := t.TempDir()

	// Root AGENTS.md
	rootContent := "# Root Context\nRoot-level info."
	os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(rootContent), 0o644)

	// internal/AGENTS.md
	internalDir := filepath.Join(tmpDir, "internal")
	os.MkdirAll(internalDir, 0o755)
	internalContent := "# Internal Context\nInternal-level info."
	os.WriteFile(filepath.Join(internalDir, "AGENTS.md"), []byte(internalContent), 0o644)

	// internal/agent/AGENTS.md
	agentDir := filepath.Join(internalDir, "agent")
	os.MkdirAll(agentDir, 0o755)
	agentContent := "# Agent Context\nAgent-level info."
	os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte(agentContent), 0o644)

	workingFile := filepath.Join(agentDir, "loop.go")
	loaded, err := LoadAgentsMDForPath(tmpDir, workingFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 results (root, internal, agent), got %d", len(loaded))
	}

	// Should be root-to-leaf order
	if !strings.Contains(loaded[0].Content, "Root Context") {
		t.Errorf("first entry should be root AGENTS.md, got: %s", loaded[0].Content)
	}
	if !strings.Contains(loaded[1].Content, "Internal Context") {
		t.Errorf("second entry should be internal AGENTS.md, got: %s", loaded[1].Content)
	}
	if !strings.Contains(loaded[2].Content, "Agent Context") {
		t.Errorf("third entry should be agent AGENTS.md, got: %s", loaded[2].Content)
	}

	// Check RelPaths
	if loaded[0].RelPath != "." {
		t.Errorf("root relPath should be '.', got %q", loaded[0].RelPath)
	}
	expectedInternal := filepath.Join("internal")
	if loaded[1].RelPath != expectedInternal {
		t.Errorf("internal relPath = %q, want %q", loaded[1].RelPath, expectedInternal)
	}
	expectedAgent := filepath.Join("internal", "agent")
	if loaded[2].RelPath != expectedAgent {
		t.Errorf("agent relPath = %q, want %q", loaded[2].RelPath, expectedAgent)
	}
}

func TestLoadAgentsMDForPath_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	loaded, err := LoadAgentsMDForPath(tmpDir, filepath.Join(tmpDir, "some", "file.go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 results, got %d", len(loaded))
	}
}

func TestLoadAgentsMDForPath_PartialHierarchy(t *testing.T) {
	tmpDir := t.TempDir()

	// Only root and a deep subdir, no AGENTS.md in the middle
	rootContent := "# Root"
	os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(rootContent), 0o644)

	deepDir := filepath.Join(tmpDir, "a", "b", "c")
	os.MkdirAll(deepDir, 0o755)
	deepContent := "# Deep"
	os.WriteFile(filepath.Join(deepDir, "AGENTS.md"), []byte(deepContent), 0o644)

	workingFile := filepath.Join(deepDir, "file.go")
	loaded, err := LoadAgentsMDForPath(tmpDir, workingFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 results, got %d", len(loaded))
	}
	if !strings.Contains(loaded[0].Content, "Root") {
		t.Error("first should be root")
	}
	if !strings.Contains(loaded[1].Content, "Deep") {
		t.Error("second should be deep")
	}
}
