package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/tools"
)

// Issue #138 (second half): an ABSOLUTE path used to bypass the session's
// bound worktree. The rig-wide fence allowlist admits every tree under the
// configured root, so a session bound to worktree A could write into the
// rig's repo clone and file_write still answered success:true — the task
// then failed its checker with no error anywhere in the transcript.
//
// These tests pin the per-session jail: filesystem tools pass the session
// working directory from the context as the jail root, so an absolute path
// outside it is REFUSED even though the static fence admits it.

// jailToolFixture builds one fence that admits both trees (the shape that
// produced the silent cross-worktree write) plus the tool's own checker.
func jailToolFixture(t *testing.T) (fence *intsecurity.FenceChecker, sessionDir, foreignTree string) {
	t.Helper()
	rig := t.TempDir()
	sessionDir = filepath.Join(rig, "worktrees", "session-a")
	foreignTree = filepath.Join(rig, "clones", "rig")
	for _, d := range []string{sessionDir, foreignTree} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return intsecurity.NewFenceChecker(intsecurity.FenceConfig{
		Enabled:  true,
		RootPath: rig,
	}, nil), sessionDir, foreignTree
}

func sessionCtx(dir string) context.Context {
	return tools.ContextWithWorkingDir(context.Background(), dir)
}

func TestWriteFileTool_AbsolutePathOutsideSessionRootRefused(t *testing.T) {
	fence, sessionDir, foreignTree := jailToolFixture(t)
	tool := NewWriteFileTool(nil)
	tool.SetFenceChecker(fence)
	ctx := sessionCtx(sessionDir)

	foreign := filepath.Join(foreignTree, "answer.txt")
	_, err := tool.Execute(ctx, map[string]any{
		"path":    foreign,
		"content": "rig\n",
		"direct":  true,
	})
	if err == nil {
		t.Fatalf("file_write accepted %q outside the session root %q", foreign, sessionDir)
	}
	if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Errorf("error = %v, want errors.Is(ErrOutsideSessionRoot)", err)
	}
	if !strings.Contains(err.Error(), sessionDir) {
		t.Errorf("refusal %q does not name the session root %q", err, sessionDir)
	}
	if _, statErr := os.Stat(foreign); !os.IsNotExist(statErr) {
		t.Errorf("file outside the session root was written anyway (stat err = %v)", statErr)
	}

	// The same call against a path INSIDE the session root succeeds.
	inside := filepath.Join(sessionDir, "answer.txt")
	res, err := tool.Execute(ctx, map[string]any{
		"path":    inside,
		"content": "42\n",
		"direct":  true,
	})
	if err != nil {
		t.Fatalf("file_write inside the session root refused: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if !tr.Success {
		t.Errorf("inside write reported success=false: %+v", tr)
	}
	if data, readErr := os.ReadFile(inside); readErr != nil || string(data) != "42\n" {
		t.Errorf("inside write landed %q / %v, want \"42\\n\"", string(data), readErr)
	}
}

func TestReadFileTool_AbsolutePathOutsideSessionRootRefused(t *testing.T) {
	fence, sessionDir, foreignTree := jailToolFixture(t)
	foreign := filepath.Join(foreignTree, "notes.md")
	if err := os.WriteFile(foreign, []byte("rig notes\n"), 0o600); err != nil {
		t.Fatalf("seed foreign file: %v", err)
	}

	tool := NewReadFileTool(nil, nil)
	tool.SetFenceChecker(fence)
	ctx := sessionCtx(sessionDir)

	if _, err := tool.Execute(ctx, map[string]any{"path": foreign}); err == nil {
		t.Fatalf("file_read accepted %q outside the session root", foreign)
	} else if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Errorf("error = %v, want errors.Is(ErrOutsideSessionRoot)", err)
	}

	inside := filepath.Join(sessionDir, "notes.md")
	if err := os.WriteFile(inside, []byte("session notes\n"), 0o600); err != nil {
		t.Fatalf("seed session file: %v", err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"path": inside}); err != nil {
		t.Errorf("file_read inside the session root refused: %v", err)
	}
}

func TestListDirectoryTool_AbsolutePathOutsideSessionRootRefused(t *testing.T) {
	fence, sessionDir, foreignTree := jailToolFixture(t)
	if err := os.WriteFile(filepath.Join(foreignTree, "secret.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("seed foreign file: %v", err)
	}
	tool := NewListDirectoryTool(nil)
	tool.SetFenceChecker(fence)
	ctx := sessionCtx(sessionDir)

	if _, err := tool.Execute(ctx, map[string]any{"path": foreignTree}); err == nil {
		t.Fatalf("list_directory accepted %q outside the session root", foreignTree)
	} else if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Errorf("error = %v, want errors.Is(ErrOutsideSessionRoot)", err)
	}

	if _, err := tool.Execute(ctx, map[string]any{"path": sessionDir}); err != nil {
		t.Errorf("list_directory of the session root refused: %v", err)
	}
	// An omitted path defaults to the session working directory, so it is
	// inside the jail by construction.
	if _, err := tool.Execute(ctx, map[string]any{}); err != nil {
		t.Errorf("list_directory with no path refused: %v", err)
	}
}

func TestDeleteFileTool_AbsolutePathOutsideSessionRootRefused(t *testing.T) {
	fence, sessionDir, foreignTree := jailToolFixture(t)
	foreign := filepath.Join(foreignTree, "victim.txt")
	if err := os.WriteFile(foreign, []byte("doomed\n"), 0o600); err != nil {
		t.Fatalf("seed foreign file: %v", err)
	}
	tool := NewDeleteFileTool(nil)
	tool.SetFenceChecker(fence)
	ctx := sessionCtx(sessionDir)

	if _, err := tool.Execute(ctx, map[string]any{"path": foreign}); err == nil {
		t.Fatalf("file_delete accepted %q outside the session root", foreign)
	} else if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Errorf("error = %v, want errors.Is(ErrOutsideSessionRoot)", err)
	}
	if _, statErr := os.Stat(foreign); statErr != nil {
		t.Errorf("foreign file was deleted anyway: %v", statErr)
	}
}

// A session with NO bound working directory has no jail: behavior must stay
// byte-identical to the pre-jail posture (rig fence only).
func TestFilesystemTools_NoSessionDirKeepsLegacyFence(t *testing.T) {
	fence, _, foreignTree := jailToolFixture(t)
	foreign := filepath.Join(foreignTree, "loose.txt")

	tool := NewWriteFileTool(nil)
	tool.SetFenceChecker(fence)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"path":    foreign,
		"content": "x\n",
		"direct":  true,
	}); err != nil {
		t.Fatalf("unbound session must keep the legacy (unjailed) fence, got %v", err)
	}
	if _, statErr := os.Stat(foreign); statErr != nil {
		t.Errorf("unbound write did not land: %v", statErr)
	}
}
