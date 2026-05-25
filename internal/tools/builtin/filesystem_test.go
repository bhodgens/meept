package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestReadFileTool(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	//nolint:gosec // test directory/file
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadFileTool(nil, nil)
	ctx := context.Background()

	// Test basic read (raw mode preserves original behavior)
	t.Run("basic read", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{"path": filePath, "raw": true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		toolResult := result.(tools.ToolResult)
		if toolResult.Result != content {
			t.Errorf("expected %q, got %q", content, toolResult.Result)
		}
	})

	// Test hashline formatting (default)
	t.Run("hashline read", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{"path": filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		toolResult := result.(tools.ToolResult)
		got, ok := toolResult.Result.(string)
		if !ok {
			t.Fatalf("expected string result, got %T", toolResult.Result)
		}
		// Should contain hashline format: LINE:HASH|content
		if !containsHashlineFormat(got, 1, "line 1") {
			t.Errorf("expected hashline format for line 1, got %q", got)
		}
		if !containsHashlineFormat(got, 3, "line 3") {
			t.Errorf("expected hashline format for line 3, got %q", got)
		}
	})

	// Test with offset (raw mode)
	t.Run("with offset", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"path":   filePath,
			"offset": float64(2),
			"raw":    true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "line 2\nline 3\nline 4\nline 5"
		toolResult := result.(tools.ToolResult)
		if toolResult.Result != expected {
			t.Errorf("expected %q, got %q", expected, toolResult.Result)
		}
	})

	// Test with offset and limit (raw mode)
	t.Run("with offset and limit", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"path":   filePath,
			"offset": float64(2),
			"limit":  float64(2),
			"raw":    true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "line 2\nline 3"
		toolResult := result.(tools.ToolResult)
		if toolResult.Result != expected {
			t.Errorf("expected %q, got %q", expected, toolResult.Result)
		}
	})

	// Test hashline with offset
	t.Run("hashline with offset", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"path":   filePath,
			"offset": float64(3),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		toolResult := result.(tools.ToolResult)
		got, ok := toolResult.Result.(string)
		if !ok {
			t.Fatalf("expected string result, got %T", toolResult.Result)
		}
		// Should start with line 3, not line 1
		if !containsHashlineFormat(got, 3, "line 3") {
			t.Errorf("expected hashline format for line 3, got %q", got)
		}
	})

	// Test hashline consistency (same content produces same hash)
	t.Run("hashline consistent", func(t *testing.T) {
		result1, _ := tool.Execute(ctx, map[string]any{"path": filePath})
		result2, _ := tool.Execute(ctx, map[string]any{"path": filePath})
		if result1.(tools.ToolResult).Result != result2.(tools.ToolResult).Result {
			t.Error("hashline output should be deterministic")
		}
	})

	// Test non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"path": "/nonexistent/file.txt"})
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	// Test empty path
	t.Run("empty path", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"path": ""})
		if err == nil {
			t.Error("expected error for empty path")
		}
	})

	// Test directory
	t.Run("directory", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"path": dir})
		if err == nil {
			t.Error("expected error for directory")
		}
	})
}

func TestWriteFileTool(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteFileTool(nil)
	ctx := context.Background()

	// Test basic write
	t.Run("basic write", func(t *testing.T) {
		filePath := filepath.Join(dir, "write_test.txt")
		result, err := tool.Execute(ctx, map[string]any{
			"path":    filePath,
			"content": "hello world",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Error("expected result")
		}

		// Verify file contents
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if string(data) != "hello world" {
			t.Errorf("expected 'hello world', got %q", string(data))
		}
	})

	// Test overwrite
	t.Run("overwrite", func(t *testing.T) {
		filePath := filepath.Join(dir, "overwrite_test.txt")
		_ = os.WriteFile(filePath, []byte("original"), 0o644) //nolint:gosec // test uses temp dir

		_, err := tool.Execute(ctx, map[string]any{
			"path":    filePath,
			"content": "new content",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(filePath)
		if string(data) != "new content" {
			t.Errorf("expected 'new content', got %q", string(data))
		}
	})

	// Test append
	t.Run("append", func(t *testing.T) {
		filePath := filepath.Join(dir, "append_test.txt")
		_ = os.WriteFile(filePath, []byte("original"), 0o644) //nolint:gosec // test uses temp dir

		_, err := tool.Execute(ctx, map[string]any{
			"path":    filePath,
			"content": " appended",
			"append":  true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(filePath)
		if string(data) != "original appended" {
			t.Errorf("expected 'original appended', got %q", string(data))
		}
	})

	// Test create parent directories
	t.Run("create parent directories", func(t *testing.T) {
		filePath := filepath.Join(dir, "subdir", "nested", "file.txt")
		_, err := tool.Execute(ctx, map[string]any{
			"path":    filePath,
			"content": "nested content",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(filePath)
		if string(data) != "nested content" {
			t.Errorf("expected 'nested content', got %q", string(data))
		}
	})

	// Test empty path
	t.Run("empty path", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{
			"path":    "",
			"content": "test",
		})
		if err == nil {
			t.Error("expected error for empty path")
		}
	})
}

func TestDeleteFileTool(t *testing.T) {
	dir := t.TempDir()
	tool := NewDeleteFileTool(nil)
	ctx := context.Background()

	// Test basic delete
	t.Run("basic delete", func(t *testing.T) {
		filePath := filepath.Join(dir, "delete_test.txt")
		_ = os.WriteFile(filePath, []byte("to be deleted"), 0o644) //nolint:gosec // test uses temp dir

		_, err := tool.Execute(ctx, map[string]any{"path": filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Error("file should have been deleted")
		}
	})

	// Test non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"path": filepath.Join(dir, "nonexistent.txt")})
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	// Test delete directory
	t.Run("delete directory", func(t *testing.T) {
		subdir := filepath.Join(dir, "subdir")
		_ = os.Mkdir(subdir, 0o755) //nolint:gosec // test uses temp dir

		_, err := tool.Execute(ctx, map[string]any{"path": subdir})
		if err == nil {
			t.Error("expected error for directory")
		}
	})
}

func TestListDirectoryTool(t *testing.T) {
	dir := t.TempDir()

	// Create test structure
	_ = os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("content"), 0o644) //nolint:gosec // test uses temp dir
	_ = os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("content"), 0o644) //nolint:gosec // test uses temp dir
	_ = os.Mkdir(filepath.Join(dir, "subdir"), 0o755)                            //nolint:gosec // test uses temp dir
	_ = os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte("content"), 0o644) //nolint:gosec // test uses temp dir

	tool := NewListDirectoryTool(nil)
	ctx := context.Background()

	// Test basic listing
	t.Run("basic listing", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{"path": dir})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		toolResult := result.(tools.ToolResult)
		listResult, ok := toolResult.Result.(ListResult)
		if !ok {
			t.Fatalf("expected ListResult, got %T", toolResult.Result)
		}

		if listResult.Count != 3 {
			t.Errorf("expected 3 entries, got %d", listResult.Count)
		}
	})

	// Test recursive listing
	t.Run("recursive listing", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"path":      dir,
			"recursive": true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		toolResult := result.(tools.ToolResult)
		listResult, ok := toolResult.Result.(ListResult)
		if !ok {
			t.Fatalf("expected ListResult, got %T", toolResult.Result)
		}

		if listResult.Count != 4 {
			t.Errorf("expected 4 entries (recursive), got %d", listResult.Count)
		}
	})

	// Test max_entries
	t.Run("max_entries", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"path":        dir,
			"max_entries": float64(2),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		toolResult := result.(tools.ToolResult)
		listResult, ok := toolResult.Result.(ListResult)
		if !ok {
			t.Fatalf("expected ListResult, got %T", toolResult.Result)
		}

		if listResult.Count != 2 {
			t.Errorf("expected 2 entries, got %d", listResult.Count)
		}
		if !listResult.Truncated {
			t.Error("expected truncated to be true")
		}
	})

	// Test non-existent directory
	t.Run("non-existent directory", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"path": "/nonexistent/dir"})
		if err == nil {
			t.Error("expected error for non-existent directory")
		}
	})

	// Test file instead of directory
	t.Run("file instead of directory", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"path": filepath.Join(dir, "file1.txt")})
		if err == nil {
			t.Error("expected error for file")
		}
	})
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"absolute path", "/tmp/test", false},
		{"relative path", "test/file", false},
		{"tilde path", "~/test", false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolvePath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolvePath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// containsHashlineFormat checks if the output contains a hashline tag for the given line.
func containsHashlineFormat(output string, lineNum int, content string) bool {
	prefix := fmt.Sprintf("%d:", lineNum)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, "|"+content) {
			return true
		}
	}
	return false
}
