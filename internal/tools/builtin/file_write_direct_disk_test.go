package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// Disk-level pin for e2e run 3 T1 (2026-09-10): with the pending-changes
// registry wired (the scratch-daemon condition), file_write called with
// direct as a STRING must land the file on disk, exactly like direct:true.
// Run 3's coder sent {"content":"hello","direct":"True","path":"hello.txt"};
// the strict .(bool) failed, the write staged into the in-memory registry,
// nobody ever called resolve in the headless run, and the file never
// existed while the step claimed success.
func TestExecuteWrite_DirectStringLandsFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	ctx := ContextWithSessionID(context.Background(), "sess-direct-disk")

	cases := []struct {
		name      string
		directArg any
	}{
		{"string True (run 3 shape)", "True"},
		{"string true", "true"},
		{"string 1", "1"},
		{"bool true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			safe := strings.NewReplacer("/", "_", " ", "_", "(", "", ")", "").Replace(tc.name)
			path := filepath.Join(dir, "direct_"+safe+".txt")
			tool := NewWriteFileTool(nil)
			tool.SetPendingChangesRegistry(NewPendingChangesRegistry()) // staged path armed

			args := map[string]any{"path": path, "content": "hello", "direct": tc.directArg}

			res, err := tool.Execute(ctx, args)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			tr, ok := res.(tools.ToolResult)
			if !ok {
				t.Fatalf("unexpected result type %T", res)
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("direct write must land on disk: %v (result text: %v)", readErr, tr.Result)
			}
			if string(data) != "hello" {
				t.Fatalf("disk content = %q, want %q", string(data), "hello")
			}
			for _, ev := range tr.Evidence {
				if string(ev.Type) == "pending_change_created" {
					t.Fatal("direct write must bypass the staging path")
				}
			}
		})
	}
}

// Symmetric pin: absent or false direct still STAGES (no disk write) when a
// registry is wired — the preview/accept workflow must survive the
// coercion.
func TestExecuteWrite_DirectAbsentOrFalseStillStages(t *testing.T) {
	dir := t.TempDir()
	ctx := ContextWithSessionID(context.Background(), "sess-direct-staged")

	t.Run("absent", func(t *testing.T) {
		path := filepath.Join(dir, "staged_absent.txt")
		tool := NewWriteFileTool(nil)
		reg := NewPendingChangesRegistry()
		tool.SetPendingChangesRegistry(reg)

		if _, err := tool.Execute(ctx, map[string]any{"path": path, "content": "staged body"}); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if _, readErr := os.Stat(path); readErr == nil {
			t.Fatal("staged write must not touch disk")
		}
		if len(reg.GetBySession("sess-direct-staged")) != 1 {
			t.Fatal("expected one staged change")
		}
	})
	t.Run("explicit false", func(t *testing.T) {
		path := filepath.Join(dir, "staged_false.txt")
		tool := NewWriteFileTool(nil)
		tool.SetPendingChangesRegistry(NewPendingChangesRegistry())

		args := map[string]any{"path": path, "content": "staged body", "direct": false}
		if _, err := tool.Execute(ctx, args); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if _, readErr := os.Stat(path); readErr == nil {
			t.Fatal("direct=false must still stage, not write disk")
		}
	})
	t.Run("string false", func(t *testing.T) {
		path := filepath.Join(dir, "staged_strfalse.txt")
		tool := NewWriteFileTool(nil)
		tool.SetPendingChangesRegistry(NewPendingChangesRegistry())

		args := map[string]any{"path": path, "content": "staged body", "direct": "false"}
		if _, err := tool.Execute(ctx, args); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if _, readErr := os.Stat(path); readErr == nil {
			t.Fatal(`direct="false" must still stage, not write disk`)
		}
	})
}
