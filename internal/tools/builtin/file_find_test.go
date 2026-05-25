package builtin

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestFileFind_BasicTxtFiles(t *testing.T) {
	// Create temp directory with files
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("world"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "c.go"), []byte("package main"), 0o644)

	tool := NewFileFindTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.txt",
		"path":    tmpDir,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	fr := tr.Result.(FindResult)
	if fr.Count != 2 {
		t.Errorf("expected 2 results, got %d", fr.Count)
	}
	if fr.Truncated {
		t.Error("should not be truncated")
	}

	// Verify file names
	names := make([]string, len(fr.Results))
	for i, r := range fr.Results {
		names[i] = filepath.Base(r.Path)
	}
	sort.Strings(names)
	if names[0] != "a.txt" || names[1] != "b.txt" {
		t.Errorf("expected [a.txt b.txt], got %v", names)
	}
}

func TestFileFind_Subdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(tmpDir, "top.txt"), []byte("top"), 0o644)
	os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0o644)

	tool := NewFileFindTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.txt",
		"path":    tmpDir,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	fr := tr.Result.(FindResult)
	if fr.Count != 2 {
		t.Errorf("expected 2 results, got %d", fr.Count)
	}
}

func TestFileFind_MaxResultsTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(tmpDir, filepath.FromSlash("file"+string(rune('0'+i))+".txt")), []byte("x"), 0o644)
	}

	tool := NewFileFindTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "*.txt",
		"path":        tmpDir,
		"max_results": float64(3),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	fr := tr.Result.(FindResult)
	if fr.Count > 3 {
		t.Errorf("expected at most 3 results, got %d", fr.Count)
	}
	if !fr.Truncated {
		t.Error("expected truncated to be true")
	}
}

func TestFileFind_FilterByType(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "mydir"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0o644)

	tool := NewFileFindTool(nil)

	// Test type=dir
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "*",
		"path":    tmpDir,
		"type":    "dir",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	tr := result.(tools.ToolResult)
	fr := tr.Result.(FindResult)
	for _, r := range fr.Results {
		if r.Type != "dir" {
			t.Errorf("expected only dirs, got type=%q for %s", r.Type, r.Path)
		}
	}

	// Test type=file
	result, err = tool.Execute(context.Background(), map[string]any{
		"pattern": "*",
		"path":    tmpDir,
		"type":    "file",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	tr = result.(tools.ToolResult)
	fr = tr.Result.(FindResult)
	for _, r := range fr.Results {
		if r.Type != "file" {
			t.Errorf("expected only files, got type=%q for %s", r.Type, r.Path)
		}
	}
}

func TestFileFind_NonExistentDirectory(t *testing.T) {
	tool := NewFileFindTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.txt",
		"path":    "/nonexistent/path/that/does/not/exist",
	})
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestFileFind_EmptyPattern(t *testing.T) {
	tool := NewFileFindTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "",
	})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestFileFind_DoubleStarPattern(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "pkg", "inner")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(tmpDir, "root.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "pkg", "mid.go"), []byte("package pkg"), 0o644)
	os.WriteFile(filepath.Join(subDir, "deep.go"), []byte("package inner"), 0o644)
	os.WriteFile(filepath.Join(subDir, "deep.txt"), []byte("text"), 0o644)

	tool := NewFileFindTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.go",
		"path":    tmpDir,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	fr := tr.Result.(FindResult)
	if fr.Count != 3 {
		t.Errorf("expected 3 .go files, got %d", fr.Count)
		for _, r := range fr.Results {
			t.Logf("  matched: %s", r.Path)
		}
	}
}
