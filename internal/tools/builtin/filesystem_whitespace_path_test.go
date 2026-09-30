package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// Pins the L3 fix: a whitespace-only path must fall back to the session
// working directory from context, NOT anchor at the daemon process CWD.
func TestListDirectoryTool_WhitespacePathFallsBackToWorkingDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644)

	tool := NewListDirectoryTool(nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)

	for _, ws := range []string{" ", "\t", "  \t "} {
		result, err := tool.Execute(ctx, map[string]any{"path": ws})
		if err != nil {
			t.Fatalf("Execute with whitespace path %q: %v", ws, err)
		}
		toolResult := result.(tools.ToolResult)
		listResult, ok := toolResult.Result.(ListResult)
		if !ok {
			t.Fatalf("expected ListResult, got %T", toolResult.Result)
		}
		if listResult.Count == 0 {
			t.Errorf("whitespace path %q: expected working-dir listing (marker.txt), got 0 entries at %s", ws, listResult.Path)
		}
	}
}

// With no working dir in context AND a whitespace path, the tool must fail
// with the actionable no-working-dir error (never daemon CWD).
func TestListDirectoryTool_WhitespacePathNoWorkingDir(t *testing.T) {
	tool := NewListDirectoryTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"path": "   "}); err == nil {
		t.Fatal("whitespace path with no working dir: want error, got nil")
	}
}
