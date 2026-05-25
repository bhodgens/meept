package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// helperFileEditSetup creates a temp file with the given content and returns its path,
// the lines, and the FileEditTool.
func helperFileEditSetup(t *testing.T, content string) (string, []string, *FileEditTool) {
	t.Helper()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	tool := NewFileEditTool(nil, nil)
	return filePath, lines, tool
}

// helperAnchor builds a "LINE:HASH" anchor string for the given line in the lines slice.
func helperAnchor(lines []string, lineNum int) string {
	return FormatHashLine(lineNum, lines[lineNum-1])
}

// helperAnchorStripped returns just the "LINE:HASH" part (without content).
func helperAnchorStripped(lines []string, lineNum int) string {
	full := FormatHashLine(lineNum, lines[lineNum-1])
	parts := strings.SplitN(full, "|", 2)
	return parts[0]
}

func readFileLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func TestFileEdit_ReplaceSingleLine(t *testing.T) {
	content := "alpha\nbeta\ngamma"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	anchor := helperAnchorStripped(lines, 2)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "replace",
				"anchor":  anchor,
				"content": "BETA",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "BETA", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_ReplaceRange(t *testing.T) {
	content := "alpha\nbeta\ngamma\ndelta\nepsilon"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	startAnchor := helperAnchorStripped(lines, 2)
	endAnchor := helperAnchorStripped(lines, 4)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":         "replace",
				"anchor":     startAnchor,
				"end_anchor": endAnchor,
				"content":    "NEW1\nNEW2",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "NEW1", "NEW2", "epsilon"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_InsertBefore(t *testing.T) {
	content := "alpha\nbeta\ngamma"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	anchor := helperAnchorStripped(lines, 2)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "insert_before",
				"anchor":  anchor,
				"content": "INSERTED",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "INSERTED", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_InsertAfter(t *testing.T) {
	content := "alpha\nbeta\ngamma"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	anchor := helperAnchorStripped(lines, 2)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "insert_after",
				"anchor":  anchor,
				"content": "INSERTED",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "beta", "INSERTED", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_InsertAtBOF(t *testing.T) {
	content := "alpha\nbeta"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "insert_before",
				"anchor":  "BOF",
				"content": "FIRST",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"FIRST", "alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_InsertAtEOF(t *testing.T) {
	content := "alpha\nbeta"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "insert_after",
				"anchor":  "EOF",
				"content": "LAST",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "beta", "LAST"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_DeleteSingleLine(t *testing.T) {
	content := "alpha\nbeta\ngamma"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	anchor := helperAnchorStripped(lines, 2)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":     "delete",
				"anchor": anchor,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_DeleteRange(t *testing.T) {
	content := "alpha\nbeta\ngamma\ndelta\nepsilon"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	startAnchor := helperAnchorStripped(lines, 2)
	endAnchor := helperAnchorStripped(lines, 4)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":         "delete",
				"anchor":     startAnchor,
				"end_anchor": endAnchor,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "epsilon"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_MultipleEdits(t *testing.T) {
	content := "alpha\nbeta\ngamma\ndelta\nepsilon"
	path, lines, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	// Delete line 2 and replace line 4
	anchor2 := helperAnchorStripped(lines, 2)
	anchor4 := helperAnchorStripped(lines, 4)

	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":     "delete",
				"anchor": anchor2,
			},
			map[string]any{
				"op":      "replace",
				"anchor":  anchor4,
				"content": "DELTA_NEW",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	got := readFileLines(t, path)
	want := []string{"alpha", "gamma", "DELTA_NEW", "epsilon"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestFileEdit_StaleAnchorRejection(t *testing.T) {
	content := "alpha\nbeta\ngamma"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	// Use an intentionally wrong anchor (line 2 but with wrong hash)
	result, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "replace",
				"anchor":  "2:zz",
				"content": "SHOULD NOT APPLY",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	if tr.Success {
		t.Error("expected edit to be rejected for stale anchor")
	}
	if tr.Error == "" {
		t.Error("expected error message for stale anchor")
	}

	// Verify file was NOT modified
	got := readFileLines(t, path)
	if got[1] != "beta" {
		t.Errorf("file should not be modified, got line 2 = %q", got[1])
	}
}

func TestFileEdit_InvalidAnchorFormat(t *testing.T) {
	content := "alpha\nbeta\ngamma"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "replace",
				"anchor":  "invalid",
				"content": "SHOULD NOT APPLY",
			},
		},
	})
	if err == nil {
		t.Error("expected error for invalid anchor format")
	}
}

func TestFileEdit_NonExistentFile(t *testing.T) {
	tool := NewFileEditTool(nil, nil)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"path": "/nonexistent/path/file.txt",
		"edits": []any{
			map[string]any{
				"op":      "replace",
				"anchor":  "1:ab",
				"content": "test",
			},
		},
	})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFileEdit_EmptyEdits(t *testing.T) {
	content := "alpha\nbeta"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"path":  path,
		"edits": []any{},
	})
	if err == nil {
		t.Error("expected error for empty edits")
	}
}

func TestFileEdit_MissingOp(t *testing.T) {
	content := "alpha\nbeta"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"anchor":  "1:ab",
				"content": "test",
			},
		},
	})
	if err == nil {
		t.Error("expected error for missing op")
	}
}

func TestFileEdit_InvalidOp(t *testing.T) {
	content := "alpha\nbeta"
	path, _, tool := helperFileEditSetup(t, content)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "invalid_op",
				"anchor":  "1:ab",
				"content": "test",
			},
		},
	})
	if err == nil {
		t.Error("expected error for invalid op")
	}
}

func TestFileEdit_NoPath(t *testing.T) {
	tool := NewFileEditTool(nil, nil)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"edits": []any{
			map[string]any{"op": "delete", "anchor": "1:ab"},
		},
	})
	if err == nil {
		t.Error("expected error for empty path")
	}
}
