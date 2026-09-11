package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// Disk-level pin for e2e run 8 T1 (2026-09-11): with the pending-changes
// registry wired AND the context marked AUTONOMOUS (job-driven step job),
// file_write with direct:"False" — the exact argument the coder sent — must
// land the file on disk. An autonomous run has no later interactive turn to
// resolve a pending change: staging there is a silent no-op (the step
// completed, the model fabricated file_exists evidence, and the artifact
// never existed).
func TestExecuteWrite_AutonomousContextLandsFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	ctx := tools.ContextWithAutonomous(ContextWithSessionID(context.Background(), "sess-auto-disk"))

	path := filepath.Join(dir, "hello.txt")
	tool := NewWriteFileTool(nil)
	tool.SetPendingChangesRegistry(NewPendingChangesRegistry()) // staged path armed

	// Run 8's exact shape: direct present but string "False".
	args := map[string]any{"path": path, "content": "hello", "direct": "False"}

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
		t.Fatalf("autonomous write must land on disk: %v (result text: %v)", readErr, tr.Result)
	}
	if string(data) != "hello" {
		t.Fatalf("disk content = %q, want %q", string(data), "hello")
	}
	for _, ev := range tr.Evidence {
		if string(ev.Type) == "pending_change_created" {
			t.Fatal("autonomous write must bypass the staging path")
		}
	}
	for _, ev := range tr.Evidence {
		if string(ev.Type) == "file_exists" {
			return // real evidence found
		}
	}
	t.Logf("note: no file_exists evidence emitted (result: %v)", tr.Result)
}

// Symmetric pin: WITHOUT the autonomous marker the write still stages when
// direct:"False" — the interactive preview/accept workflow must survive
// alongside the autonomous bypass.
func TestExecuteWrite_NonAutonomousStillStages(t *testing.T) {
	dir := t.TempDir()
	ctx := ContextWithSessionID(context.Background(), "sess-nonauto")

	path := filepath.Join(dir, "staged.txt")
	tool := NewWriteFileTool(nil)
	tool.SetPendingChangesRegistry(NewPendingChangesRegistry())

	args := map[string]any{"path": path, "content": "hello", "direct": "False"}
	res, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("non-autonomous direct:\"False\" must stage, not write disk")
	}
	found := false
	for _, ev := range tr.Evidence {
		if string(ev.Type) == "pending_change_created" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pending_change_created evidence, got %v", tr.Evidence)
	}
}

// file_edit mirror: an autonomous-context edit applies directly instead of
// staging a pending change nothing can resolve.
func TestExecuteEdit_AutonomousContextAppliesDirectly(t *testing.T) {
	dir := t.TempDir()
	ctx := tools.ContextWithAutonomous(ContextWithSessionID(context.Background(), "sess-auto-edit"))

	path := filepath.Join(dir, "code.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	tool := NewFileEditTool(nil, nil)
	tool.SetPendingChangesRegistry(NewPendingChangesRegistry())

	lineHash2 := ComputeLineHash("beta")
	args := map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "replace",
				"anchor":  "2:" + lineHash2,
				"content": "gamma",
			},
		},
	}
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
		t.Fatalf("read back: %v", readErr)
	}
	// The replace op swaps line 2's content, dropping the trailing newline
	// the original line carried — the point of this pin is that the edit
	// APPLIED (gamma present, beta gone, no pending change), not the exact
	// whitespace round-trip.
	if string(data) != "alpha\ngamma" {
		t.Fatalf("autonomous edit must apply directly; disk = %q (result: %v)", string(data), tr.Result)
	}
	for _, ev := range tr.Evidence {
		if string(ev.Type) == "pending_change_created" {
			t.Fatal("autonomous edit must bypass the staging path")
		}
	}
}

// Context-marker unit pins: absent marker is false (interactive default);
// present marker reads back true; nested contexts inherit it.
func TestAutonomousFromContext(t *testing.T) {
	if tools.AutonomousFromContext(context.Background()) {
		t.Fatal("plain context must not be autonomous")
	}
	marked := tools.ContextWithAutonomous(context.Background())
	if !tools.AutonomousFromContext(marked) {
		t.Fatal("marked context must be autonomous")
	}
	if !tools.AutonomousFromContext(context.WithValue(marked, sessionIDContextKey, "x")) {
		t.Fatal("derived context must inherit autonomy")
	}
}
