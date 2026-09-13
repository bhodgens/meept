package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools"
)

// These tests cover the turn-start contract end to end at the tool boundary:
// a session resolves a working directory (session.ResolveWorkingDir, the
// repo-wide precedence in AGENTS.md), the loop injects it with
// tools.ContextWithWorkingDir, and the filesystem tools then succeed. When
// nothing is bound the tool must fail with the actionable
// tools.ErrNoWorkingDir instead of the bare "no path specified"
// (fresh-rig daemon11, 2026-09-13).

func writeMarker(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// listViaSession resolves the session's working directory, injects it into the
// tool context and lists the directory with no explicit path argument.
func listViaSession(t *testing.T, sess *session.Session) ListResult {
	t.Helper()
	dir, src := session.ResolveWorkingDir(sess)
	if dir == "" {
		t.Fatalf("session resolved no working directory (source %q)", src)
	}
	tool := NewListDirectoryTool(nil)
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)
	res, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("list_directory with session working dir %q: %v", dir, err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	list, ok := tr.Result.(ListResult)
	if !ok {
		t.Fatalf("unexpected result type %T", tr.Result)
	}
	return list
}

func names(list ListResult) []string {
	out := make([]string, 0, len(list.Entries))
	for _, e := range list.Entries {
		out = append(out, e.Name)
	}
	return out
}

// TestSessionWorkdir_BoundProjectResolvesAndToolSucceeds: a session bound to a
// project resolves that directory and a filesystem tool call succeeds there.
func TestSessionWorkdir_BoundProjectResolvesAndToolSucceeds(t *testing.T) {
	projectDir := t.TempDir()
	writeMarker(t, projectDir, "marker.txt")

	sess := &session.Session{ID: "session-proj", ProjectPath: projectDir}

	dir, src := session.ResolveWorkingDir(sess)
	if dir != projectDir {
		t.Fatalf("ResolveWorkingDir() = %q, want %q", dir, projectDir)
	}
	if src != session.WorkingDirFromProject {
		t.Errorf("ResolveWorkingDir() source = %q, want %q", src, session.WorkingDirFromProject)
	}

	list := listViaSession(t, sess)
	if !strings.Contains(list.Path, filepath.Base(projectDir)) {
		t.Errorf("listed %q, want path containing %q", list.Path, filepath.Base(projectDir))
	}
	if got := names(list); len(got) != 1 || got[0] != "marker.txt" {
		t.Errorf("entries = %v, want [marker.txt]", got)
	}
}

// TestSessionWorkdir_WorktreePreferredOverProject: a session with a worktree
// resolves the worktree, not the project path.
func TestSessionWorkdir_WorktreePreferredOverProject(t *testing.T) {
	worktreeDir := t.TempDir()
	projectDir := t.TempDir()
	writeMarker(t, worktreeDir, "in-worktree.txt")
	writeMarker(t, projectDir, "in-project.txt")

	sess := &session.Session{
		ID:           "session-wt",
		WorktreePath: worktreeDir,
		ProjectPath:  projectDir,
	}

	dir, src := session.ResolveWorkingDir(sess)
	if dir != worktreeDir {
		t.Fatalf("ResolveWorkingDir() = %q, want worktree %q", dir, worktreeDir)
	}
	if src != session.WorkingDirFromWorktree {
		t.Errorf("ResolveWorkingDir() source = %q, want %q", src, session.WorkingDirFromWorktree)
	}

	list := listViaSession(t, sess)
	if !strings.Contains(list.Path, filepath.Base(worktreeDir)) {
		t.Errorf("listed %q, want worktree path containing %q", list.Path, filepath.Base(worktreeDir))
	}
	if got := names(list); len(got) != 1 || got[0] != "in-worktree.txt" {
		t.Errorf("entries = %v, want [in-worktree.txt] (project dir must not be used)", got)
	}
}

// TestSessionWorkdir_DetectionCWDWithoutProjectUsesCWD: a session with no
// project but a client-supplied CWD in its detection context uses that CWD.
// This is the exact daemon11 shape that used to run with no working directory.
func TestSessionWorkdir_DetectionCWDWithoutProjectUsesCWD(t *testing.T) {
	clientCWD := t.TempDir()
	writeMarker(t, clientCWD, "from-client-cwd.txt")

	sess := &session.Session{
		ID:               "session-cwd-only",
		DetectionContext: &session.DetectionContext{CWD: clientCWD},
	}

	dir, src := session.ResolveWorkingDir(sess)
	if dir != clientCWD {
		t.Fatalf("ResolveWorkingDir() = %q, want client CWD %q", dir, clientCWD)
	}
	if src != session.WorkingDirFromDetection {
		t.Errorf("ResolveWorkingDir() source = %q, want %q", src, session.WorkingDirFromDetection)
	}

	list := listViaSession(t, sess)
	if got := names(list); len(got) != 1 || got[0] != "from-client-cwd.txt" {
		t.Errorf("entries = %v, want [from-client-cwd.txt]", got)
	}
}

// TestSessionWorkdir_NothingBoundIsActionableError: a session with neither
// project, worktree nor detection CWD yields no directory, and list_directory
// with no path must fail with the actionable tools.ErrNoWorkingDir — never the
// bare "no path specified".
func TestSessionWorkdir_NothingBoundIsActionableError(t *testing.T) {
	sess := &session.Session{ID: "session-unbound"}

	dir, src := session.ResolveWorkingDir(sess)
	if dir != "" {
		t.Fatalf("ResolveWorkingDir() = %q, want empty for an unbound session", dir)
	}
	if src != session.WorkingDirFromNone {
		t.Errorf("ResolveWorkingDir() source = %q, want %q", src, session.WorkingDirFromNone)
	}

	tool := NewListDirectoryTool(nil)
	// The agent loop injects the resolved directory; empty means unbound.
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)
	res, err := tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Fatalf("expected an error for an unbound session, got result %+v", res)
	}
	if !errors.Is(err, tools.ErrNoWorkingDir) {
		t.Errorf("error %v does not wrap tools.ErrNoWorkingDir", err)
	}
	if !tools.IsNoWorkingDir(err) {
		t.Errorf("IsNoWorkingDir(%v) = false, want true", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "no working directory for this session; pass an explicit path") {
		t.Errorf("error text %q is not the actionable message", msg)
	}
	if strings.Contains(msg, "no path specified") {
		t.Errorf("error text %q still carries the old bare wording", msg)
	}
	if !strings.Contains(msg, "list_directory") {
		t.Errorf("error text %q does not name the tool", msg)
	}
}

// TestSessionWorkdir_UnboundReadFileIsActionable: the required-path tools get
// the same treatment: an omitted path names the missing argument and wraps a
// detectable sentinel instead of the bare string.
func TestSessionWorkdir_UnboundReadFileIsActionable(t *testing.T) {
	tool := NewReadFileTool(nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected an error for read_file with no path")
	}
	if !errors.Is(err, tools.ErrNoPath) {
		t.Errorf("error %v does not wrap tools.ErrNoPath", err)
	}
	if errors.Is(err, tools.ErrNoWorkingDir) {
		t.Errorf("error %v must not claim the session has no working directory", err)
	}
	if !strings.Contains(err.Error(), "read_file:") {
		t.Errorf("error text %q does not name the tool", err.Error())
	}

	edit := NewFileEditTool(nil, nil)
	_, err = edit.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected an error for file_edit with no path")
	}
	if !errors.Is(err, tools.ErrNoPath) {
		t.Errorf("file_edit error %v does not wrap tools.ErrNoPath", err)
	}
}
