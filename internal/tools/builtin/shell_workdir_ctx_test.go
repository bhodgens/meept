package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
)

// Issue #136 / #138: the shell tool fell back to the PROCESS HOME instead of
// the session-bound working directory.
//
// internal/daemon/components.go builds the agent-path shell tool with
// NewShellExecuteTool("") and NewShellExecuteTool replaced the empty string
// with resolvePath(ctx, "~") — the daemon-wide HOME fallback the AGENTS.md
// invariant forbids ("never fall back to the daemon's CWD"; $HOME is the same
// class of bug). Every chat/step turn injects the session working directory
// into the tool context via tools.ContextWithWorkingDir (internal/agent/loop.go
// executeToolCallsUngated), but Execute/ExecuteStreaming only ever read
// t.workingDir, so `shell pwd` answered $HOME.
//
// These tests pin the resolution precedence at the tool boundary:
//
//	explicit args["working_dir"] > tools.WorkingDirFromContext(ctx) >
//	t.workingDir (only when it was EXPLICITLY configured) > actionable
//	tools.NoWorkingDirError("shell")
//
// No lane may ever resolve to "~".

func homeDirForShellTest(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory to compare against: %v", err)
	}
	// /var -> /private/var on macOS; compare against the real path so a
	// symlinked temp dir still compares equal.
	real, err := filepath.EvalSymlinks(home)
	if err != nil {
		return home
	}
	return real
}

// TestShellWorkingDirFromContextBeatsEmptyConstruction is the RED test for the
// bug itself: constructed exactly like the daemon does (workingDir == "") and
// driven with the session working directory on the context, `pwd` must print
// the session directory — not the process home.
func TestShellWorkingDirFromContextBeatsEmptyConstruction(t *testing.T) {
	sessionDir := t.TempDir()
	tool := NewShellExecuteTool("", 10*time.Second, nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	result, err := tool.Execute(ctx, map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("shell pwd: %v", err)
	}
	sr := unwrapShellResult(t, result)
	if !strings.Contains(sr.Stdout, sessionDir) {
		t.Fatalf("shell pwd ran in %q, want the session working dir %q (issue #136: fell back to $HOME=%q)",
			strings.TrimSpace(sr.Stdout), sessionDir, homeDirForShellTest(t))
	}
}

// TestShellWorkingDirContextBeatsExplicitWorkingDirField pins the precedence:
// an explicitly configured t.workingDir must not win over the live session
// binding, because the daemon-wide construction value is stale by definition
// (the daemon never binds a project).
func TestShellWorkingDirContextBeatsExplicitWorkingDirField(t *testing.T) {
	sessionDir := t.TempDir()
	constructedDir := t.TempDir()

	tool := NewShellExecuteTool(constructedDir, 10*time.Second, nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	result, err := tool.Execute(ctx, map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("shell pwd: %v", err)
	}
	sr := unwrapShellResult(t, result)
	if !strings.Contains(sr.Stdout, sessionDir) {
		t.Fatalf("shell pwd ran in %q, want the context session dir %q", strings.TrimSpace(sr.Stdout), sessionDir)
	}
	if strings.Contains(sr.Stdout, constructedDir) {
		t.Fatalf("shell pwd used the construction-time dir %q over the session binding", constructedDir)
	}
}

// TestShellWorkingDirArgumentBeatsContext pins rank 1: an explicit
// args["working_dir"] still wins over everything else (back-compat).
func TestShellWorkingDirArgumentBeatsContext(t *testing.T) {
	sessionDir := t.TempDir()
	argDir := t.TempDir()

	tool := NewShellExecuteTool("", 10*time.Second, nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	result, err := tool.Execute(ctx, map[string]any{
		"command":     "pwd",
		"working_dir": argDir,
	})
	if err != nil {
		t.Fatalf("shell pwd: %v", err)
	}
	sr := unwrapShellResult(t, result)
	if !strings.Contains(sr.Stdout, argDir) {
		t.Fatalf("explicit working_dir %q lost to %q", argDir, strings.TrimSpace(sr.Stdout))
	}
}

// TestShellWorkingDirExplicitConstructionStillWorks is the back-compat pin for
// the terminal service: when a working dir WAS explicitly configured (a
// non-empty construction value) and the context carries nothing, it is used.
func TestShellWorkingDirExplicitConstructionStillWorks(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellExecuteTool(dir, 10*time.Second, nil)

	result, err := tool.Execute(context.Background(), map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("shell pwd: %v", err)
	}
	sr := unwrapShellResult(t, result)
	if !strings.Contains(sr.Stdout, dir) {
		t.Fatalf("explicit construction dir %q not used; pwd said %q", dir, strings.TrimSpace(sr.Stdout))
	}
}

// TestShellWorkingDirNothingBoundIsActionableError pins the refusal: no arg, no
// context binding and no explicitly configured dir must fail with the
// actionable sentinel — never $HOME, never the daemon's CWD.
func TestShellWorkingDirNothingBoundIsActionableError(t *testing.T) {
	tool := NewShellExecuteTool("", 10*time.Second, nil)

	result, err := tool.Execute(context.Background(), map[string]any{"command": "pwd"})
	if err == nil {
		sr := unwrapShellResult(t, result)
		t.Fatalf("expected an actionable refusal with no working directory bound; pwd ran in %q", strings.TrimSpace(sr.Stdout))
	}
	if !errors.Is(err, tools.ErrNoWorkingDir) {
		t.Fatalf("error %v does not wrap tools.ErrNoWorkingDir", err)
	}
	if !tools.IsNoWorkingDir(err) {
		t.Fatalf("IsNoWorkingDir(%v) = false, want true", err)
	}
	if !strings.Contains(err.Error(), "shell") {
		t.Errorf("error %q does not name the tool", err.Error())
	}
	if home := homeDirForShellTest(t); strings.Contains(err.Error(), home) {
		t.Errorf("error %q still points at the process home %q", err.Error(), home)
	}
}

// TestShellWorkingDirEmptyContextValueIsUnbound: the agent loop injects an
// EMPTY working dir for an unbound session (tools.ContextWithWorkingDir stores
// it verbatim). Empty must mean "nothing bound", not "use the process home".
func TestShellWorkingDirEmptyContextValueIsUnbound(t *testing.T) {
	tool := NewShellExecuteTool("", 10*time.Second, nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), "")

	_, err := tool.Execute(ctx, map[string]any{"command": "pwd"})
	if !errors.Is(err, tools.ErrNoWorkingDir) {
		t.Fatalf("empty context working dir must be treated as unbound; got err=%v", err)
	}
}

// TestShellWorkingDirStreamingLaneMatchesExecute pins the second Execute copy
// (ExecuteStreaming, the lane the executor uses whenever the bus is wired): it
// must resolve the work dir identically.
func TestShellWorkingDirStreamingLaneMatchesExecute(t *testing.T) {
	sessionDir := t.TempDir()
	tool := NewShellExecuteTool("", 10*time.Second, nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	var updates []tools.ProgressUpdate
	result, err := tool.ExecuteStreaming(ctx, map[string]any{"command": "pwd"}, func(pu tools.ProgressUpdate) {
		updates = append(updates, pu)
	})
	if err != nil {
		t.Fatalf("ExecuteStreaming shell pwd: %v", err)
	}
	sr := unwrapShellResult(t, result)
	if !strings.Contains(sr.Stdout, sessionDir) {
		t.Fatalf("ExecuteStreaming pwd ran in %q, want session dir %q", strings.TrimSpace(sr.Stdout), sessionDir)
	}
	if len(updates) == 0 {
		t.Error("streaming lane emitted no progress updates")
	}
}

// TestShellWorkingDirStreamingLaneNothingBoundIsActionable mirrors the
// non-streaming refusal on the streaming lane.
func TestShellWorkingDirStreamingLaneNothingBoundIsActionable(t *testing.T) {
	tool := NewShellExecuteTool("", 10*time.Second, nil)

	_, err := tool.ExecuteStreaming(context.Background(), map[string]any{"command": "pwd"}, func(tools.ProgressUpdate) {})
	if !errors.Is(err, tools.ErrNoWorkingDir) {
		t.Fatalf("streaming lane must fail actionably with no working dir; got err=%v", err)
	}
}

// recordingFence captures the work dir the shell tool hands to the fence so the
// test can prove the fenced boundary is the session directory too (a stale
// $HOME here would fence the wrong root).
type recordingFence struct {
	workDir string
}

func (f *recordingFence) CheckPath(string, string) error { return nil }

func (f *recordingFence) CheckCommand(_ string, workDir string) error {
	f.workDir = workDir
	return nil
}

// TestShellWorkingDirFenceSeesSessionDir: the fence check must run against the
// session directory (both lanes), not the process home.
func TestShellWorkingDirFenceSeesSessionDir(t *testing.T) {
	sessionDir := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	t.Run("Execute", func(t *testing.T) {
		fence := &recordingFence{}
		tool := NewShellExecuteTool("", 10*time.Second, nil)
		tool.SetFenceChecker(fence)

		if _, err := tool.Execute(ctx, map[string]any{"command": "pwd"}); err != nil {
			t.Fatalf("shell pwd: %v", err)
		}
		if fence.workDir != sessionDir {
			t.Fatalf("fence checked workDir %q, want session dir %q", fence.workDir, sessionDir)
		}
	})

	t.Run("ExecuteStreaming", func(t *testing.T) {
		fence := &recordingFence{}
		tool := NewShellExecuteTool("", 10*time.Second, nil)
		tool.SetFenceChecker(fence)

		if _, err := tool.ExecuteStreaming(ctx, map[string]any{"command": "pwd"}, func(tools.ProgressUpdate) {}); err != nil {
			t.Fatalf("ExecuteStreaming shell pwd: %v", err)
		}
		if fence.workDir != sessionDir {
			t.Fatalf("fence checked workDir %q, want session dir %q", fence.workDir, sessionDir)
		}
	})
}

// TestShellWorkingDirPTYSessionFallsBackToToolDir pins the PTY lane: with no
// config.Dir the session must inherit the explicitly configured tool dir, and
// the fence must see that same directory (it used to see t.workingDir only,
// which is $HOME for the daemon construction).
func TestShellWorkingDirPTYSessionFallsBackToToolDir(t *testing.T) {
	dir := t.TempDir()
	fence := &recordingFence{}

	tool := NewShellExecuteTool(dir, 10*time.Second, nil)
	tool.SetFenceChecker(fence)

	// A nil PTY manager refuses AFTER the fence check, which is all this test
	// needs: the fence must have been consulted with the resolved dir.
	_, _ = tool.CreateSession("pty-workdir-probe", tools.PTYSessionConfig{Cmd: "bash"})

	if fence.workDir != dir {
		t.Fatalf("PTY fence checked %q, want the tool's configured dir %q", fence.workDir, dir)
	}
}

// TestFileWriteRelativePathUsesContextWorkingDir is the #138 companion: a
// RELATIVE path in file_write must resolve under the session working directory
// from the context (not the daemon's process CWD, and not the process home).
func TestFileWriteRelativePathUsesContextWorkingDir(t *testing.T) {
	sessionDir := t.TempDir()
	tool := NewWriteFileTool(nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	res, err := tool.Execute(ctx, map[string]any{
		"path":    "relative-answer.txt",
		"content": "42\n",
		"direct":  true,
	})
	if err != nil {
		t.Fatalf("file_write: %v", err)
	}
	_ = res

	// The tool result names the resolved path; the file must land under the
	// session dir, never in the test process CWD or the home dir.
	if _, statErr := os.Stat(filepath.Join(sessionDir, "relative-answer.txt")); statErr != nil {
		t.Fatalf("relative file_write did not land in the session workdir %q: %v", sessionDir, statErr)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if msg, ok := tr.Result.(string); ok && !strings.Contains(msg, sessionDir) {
		t.Errorf("file_write reported %q, want a path under %q", msg, sessionDir)
	}
}

// TestListDirectoryRelativePathUsesContextWorkingDir pins the same relative-path
// resolution for list_directory with an explicit relative path.
func TestListDirectoryRelativePathUsesContextWorkingDir(t *testing.T) {
	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "ls-marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewListDirectoryTool(nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), sessionDir)

	res, err := tool.Execute(ctx, map[string]any{"path": "."})
	if err != nil {
		t.Fatalf("list_directory .: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	list, ok := tr.Result.(ListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", tr.Result)
	}
	if !strings.Contains(list.Path, filepath.Base(sessionDir)) {
		t.Fatalf("listed %q, want a path under the session dir %q", list.Path, sessionDir)
	}
}
