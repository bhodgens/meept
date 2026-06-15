package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// testFence is a test double for FenceChecker that confines paths to root.
type testFence struct {
	root string
}

func (f testFence) CheckPath(p, op string) error {
	abs, err := filepath.Abs(p)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", p, err)
	}
	if !strings.HasPrefix(abs, f.root) {
		return fmt.Errorf("path %s outside fence %s", abs, f.root)
	}
	return nil
}

func (f testFence) CheckCommand(cmd, workDir string) error { return nil }

// TestGitCommitTool_SetFenceCheckerExists verifies the setter exists and is
// safe to call with nil.
func TestGitCommitTool_SetFenceCheckerExists(t *testing.T) {
	tool := NewGitCommitTool(t.TempDir())
	// Calling with nil must not panic and must leave the tool usable.
	tool.SetFenceChecker(nil)
	// Calling with a real fence must not panic either.
	tool.SetFenceChecker(testFence{root: t.TempDir()})
}

// TestGitSplitTool_SetFenceCheckerExists mirrors the above for the split tool.
func TestGitSplitTool_SetFenceCheckerExists(t *testing.T) {
	tool := NewGitSplitTool(t.TempDir())
	tool.SetFenceChecker(nil)
	tool.SetFenceChecker(testFence{root: t.TempDir()})
}

// TestGitOverviewTool_SetFenceCheckerExists mirrors the above for the overview tool.
func TestGitOverviewTool_SetFenceCheckerExists(t *testing.T) {
	tool := NewGitOverviewTool(t.TempDir())
	tool.SetFenceChecker(nil)
	tool.SetFenceChecker(testFence{root: t.TempDir()})
}

// TestGitCommitTool_RejectsOutOfFencePath verifies that a commit attempting
// to stage a path outside the fence is rejected before git is invoked.
func TestGitCommitTool_RejectsOutOfFencePath(t *testing.T) {
	root := t.TempDir()
	tool := NewGitCommitTool(root)
	tool.SetFenceChecker(testFence{root: root})

	// Use a path that is clearly outside the sandbox root.
	res, err := tool.Execute(context.Background(), map[string]any{
		"working_dir": root,
		"files":       []any{"../../../etc/passwd"},
		"message":     "exfil attempt",
	})
	if err == nil {
		t.Fatalf("expected fence rejection, got result=%v", res)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "fence") {
		t.Fatalf("expected fence error, got %v", err)
	}
}

// TestGitCommitTool_ValidateToggle_DefaultsTrue verifies that when the
// "validate" key is absent, validation defaults to enabled.
func TestGitCommitTool_ValidateToggle_DefaultsTrue(t *testing.T) {
	tool := NewGitCommitTool(t.TempDir())

	// Message too short and non-conventional → should fail validation.
	_, err := tool.Execute(context.Background(), map[string]any{
		"message": "bad", // < 10 chars and non-conventional
	})
	if err == nil {
		t.Fatal("expected validation error for short message when validate unspecified")
	}
	if !strings.Contains(err.Error(), "invalid commit message") {
		t.Fatalf("expected invalid commit message error, got %v", err)
	}
}

// TestGitCommitTool_ValidateToggle_ExplicitlyFalse verifies that an explicit
// validate:false bypasses the conventional-commit format check. The tool
// should still fail because "message required" only triggers on empty, so we
// pass a short non-conventional message and expect success (or at least no
// validation error). Since no git repo exists in the temp dir, we expect a
// git failure — but the error must NOT mention "invalid commit message".
func TestGitCommitTool_ValidateToggle_ExplicitlyFalse(t *testing.T) {
	tool := NewGitCommitTool(t.TempDir())

	_, err := tool.Execute(context.Background(), map[string]any{
		"message":  "bad",
		"validate": false,
	})
	if err == nil {
		// validate=false bypassed; git failed because no repo. That's fine.
		return
	}
	if strings.Contains(err.Error(), "invalid commit message") {
		t.Fatalf("validate:false should bypass validation, got: %v", err)
	}
}
