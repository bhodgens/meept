package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestFileGrep_ContentMode(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\nfoo bar\nhello again\n"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("no match here\n"), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":      "hello",
		"path":         tmpDir,
		"output_mode":  "content",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	if gr.Matches != 2 {
		t.Errorf("expected 2 matches, got %d", gr.Matches)
	}
	if gr.Output == "" {
		t.Error("expected non-empty output")
	}
	if !strings.Contains(gr.Output, "hello") {
		t.Error("output should contain 'hello'")
	}
}

func TestFileGrep_FilesWithMatchesMode(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\n"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("no match\n"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte("hello again\n"), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "hello",
		"path":        tmpDir,
		"output_mode": "files_with_matches",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	if len(gr.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(gr.Files))
	}
	if gr.Matches != 2 {
		t.Errorf("expected 2 total matches, got %d", gr.Matches)
	}
}

func TestFileGrep_CountMode(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello\nhello\nworld\n"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("hello\nworld\n"), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "hello",
		"path":        tmpDir,
		"output_mode": "count",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	if len(gr.Counts) != 2 {
		t.Fatalf("expected 2 file counts, got %d", len(gr.Counts))
	}

	// Verify counts
	totalCount := 0
	for _, gc := range gr.Counts {
		totalCount += gc.Count
	}
	if totalCount != 3 {
		t.Errorf("expected total count of 3, got %d", totalCount)
	}
}

func TestFileGrep_WithGlobFilter(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("hello world\n"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("hello world\n"), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":      "hello",
		"path":         tmpDir,
		"output_mode":  "files_with_matches",
		"glob":         "*.go",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	if len(gr.Files) != 1 {
		t.Errorf("expected 1 file with glob filter, got %d", len(gr.Files))
	}
	if len(gr.Files) > 0 && !strings.HasSuffix(gr.Files[0], "a.go") {
		t.Errorf("expected a.go, got %s", gr.Files[0])
	}
}

func TestFileGrep_ContextLines(t *testing.T) {
	tmpDir := t.TempDir()
	content := "line1\nline2\nTARGET\nline4\nline5\n"
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte(content), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "TARGET",
		"path":        filepath.Join(tmpDir, "test.txt"),
		"output_mode": "content",
		"context":     float64(1),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	if gr.Matches != 1 {
		t.Errorf("expected 1 match, got %d", gr.Matches)
	}
	// With context=1, should see line2, TARGET, line4 (3 lines)
	lines := strings.Split(gr.Output, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines with context=1, got %d: %v", len(lines), lines)
	}
}

func TestFileGrep_MaxResults(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("match1\nmatch2\nmatch3\nmatch4\nmatch5\n"), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":      "match",
		"path":         tmpDir,
		"output_mode":  "content",
		"max_results":  float64(2),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	if gr.Matches != 5 {
		t.Errorf("expected 5 total matches, got %d", gr.Matches)
	}
	// With default context=2 and only 5 lines total, the context may overlap.
	// Just verify truncated is true
	if !gr.Truncated {
		t.Error("expected truncated to be true")
	}
}

func TestFileGrep_BinaryFileSkipping(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a binary file with null bytes
	binaryContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00, 0x00} // PNG-like header with null
	os.WriteFile(filepath.Join(tmpDir, "image.bin"), binaryContent, 0o644)
	os.WriteFile(filepath.Join(tmpDir, "text.txt"), []byte("hello world\n"), 0o644)

	tool := NewFileGrepTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "hello",
		"path":        tmpDir,
		"output_mode": "files_with_matches",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	tr := result.(tools.ToolResult)
	gr := tr.Result.(GrepResult)
	// Should only find text.txt, not binary file
	if len(gr.Files) != 1 {
		t.Errorf("expected 1 file (binary should be skipped), got %d", len(gr.Files))
	}
}

func TestFileGrep_NonExistentDirectory(t *testing.T) {
	tool := NewFileGrepTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "hello",
		"path":    "/nonexistent/path/that/does/not/exist",
	})
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestFileGrep_InvalidRegex(t *testing.T) {
	tool := NewFileGrepTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "[invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}
