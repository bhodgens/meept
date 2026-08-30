package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// TestListDirectoryDefaultsToContextWorkingDir: list_directory with no path
// argument must default to the session working directory injected by the
// agent loop, not error with "no path specified". Found when repo-inventory
// failed with the coder calling list_directory() with empty args every tick.
func TestListDirectoryDefaultsToContextWorkingDir(t *testing.T) {
	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewListDirectoryTool(nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	res, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("list_directory with no path should use session workdir, got error: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	list, ok := tr.Result.(ListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", tr.Result)
	}
	// The path should contain the session dir (may be resolved differently on macOS)
	if !strings.Contains(list.Path, filepath.Base(sessionDir)) {
		t.Fatalf("listed %q, expected path containing %q", list.Path, filepath.Base(sessionDir))
	}
	found := false
	for _, e := range list.Entries {
		if e.Name == "marker.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("marker.txt not in listing: %+v", list.Entries)
	}
}

// TestListDirectoryEmptyContextStillFails: without a context working dir,
// list_directory with no path must still error (process cwd is almost never
// the user's project).
func TestListDirectoryEmptyContextStillFails(t *testing.T) {
	tool := NewListDirectoryTool(nil)
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("expected error when no context working dir is set, got result: %+v", res)
	}
	if !strings.Contains(err.Error(), "no path specified") {
		t.Fatalf("expected 'no path specified', got: %v", err)
	}
}

// TestWriteFileRelativePathUsesContextWorkingDir: writes with relative paths
// must land in the session working dir, not the daemon process cwd. This is
// the same bug class as list_directory.
func TestWriteFileRelativePathUsesContextWorkingDir(t *testing.T) {
	sessionDir := t.TempDir()
	tool := NewWriteFileTool(nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	res, err := tool.Execute(ctx, map[string]any{
		"path":    "answer.txt",
		"content": "42\n",
		"direct":  true,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	_ = res // result shape is caller-specific; only errors matter here.

	// Find the file in the temp dir (may be in a subdirectory due to Go test isolation)
	found := false
	filepath.Walk(sessionDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && filepath.Base(path) == "answer.txt" {
			got, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if string(got) == "42\n" {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatalf("answer.txt with content '42\\n' not found in workdir")
	}
}
