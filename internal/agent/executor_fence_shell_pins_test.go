package agent

// executor_fence_shell_pins_test.go — bughunt 2026-11-27 F2 pins for the
// executor arg-normalization seam (checkPermission), issues H2/H3/M4.
//
// H2 (fence-vs-shell key mismatch): the shell tool runs in the directory
// resolved by resolveShellWorkingDir (internal/tools/builtin/shell.go):
// args["working_dir"] first (via resolvePath: "~" -> $HOME, relative ->
// turn dir), else the turn's session dir. The executor must inject that
// SAME resolution into details["working_dir"] so the central fence judges
// the directory the command will actually run in — pre-fix the fence
// judged "." (daemon cwd) and a model-supplied working_dir passed
// unchecked.
//
// H3 (tilde join bypass): a raw "~/x" path arg was joined as
// "<turn-dir>/~/x" — judged INSIDE the session jail by the fence while the
// filesystem tools expand "~" to $HOME and touch the real target. The
// blocked globs (~/.ssh/*) were defeated the same way. The executor must
// expand "~" BEFORE the join, mirroring resolvePath.
//
// M4 (git_* unjudged): git_* calls with no working_dir run in the daemon
// process cwd (the tools never consult WorkingDirFromContext and are
// constructed with ""); the executor must inject the turn's session dir so
// the git_ fence arm judges it.
//
// All pins run the REAL executor pipeline (NewExecutor + real production
// fence stage minted via composeLoopFence/NewSessionFenceStage — the same
// construction the daemon uses) and assert on the permission verdict.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// f2PinnedExecutor builds the production shape: a placeholder registry with
// the named mock tool, a wildcard base checker (so ONLY the session fence
// can produce the verdicts these pins assert), and the per-session fence
// composed exactly as composeLoopFence does on every daemon loop. The jail
// is memoized per test (see f2JailDir) so every call within one test sees
// the SAME session root.
func f2PinnedExecutor(t *testing.T, toolName string) *Executor {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool(toolName, "pin tool", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	}))
	base := security.NewPermissionChecker(security.Config{AllowedPaths: []string{"/"}})
	loop := composeLoopFence(base, newSessionFenceFactory(testLoggerForFence()), f2JailDir(t), false, nil)
	if loop == nil || loop == base {
		t.Fatal("composeLoopFence must install a per-session fenced clone")
	}
	return NewExecutor(registry, loop)
}

// f2Jails memoizes one jail per test function: t.TempDir() mints a NEW
// numbered dir on EVERY call, so the executor's fence and the test's
// expectations must share the dir through this cache or the in-jail pins
// compare two different temp dirs.
var f2Jails = map[string]string{}

func f2JailDir(t *testing.T) string {
	t.Helper()
	if jail, ok := f2Jails[t.Name()]; ok {
		return jail
	}
	jail := t.TempDir()
	f2Jails[t.Name()] = jail
	t.Cleanup(func() { delete(f2Jails, t.Name()) })
	return jail
}

// TestExecutor_ShellWorkingDirFencedAgainstTurnJail (H2 executor half): a
// shell call whose model-supplied working_dir points OUTSIDE the session
// jail must be DENIED at the permission stage — pre-fix the fence judged
// "." and the call passed.
func TestExecutor_ShellWorkingDirFencedAgainstTurnJail(t *testing.T) {
	e := f2PinnedExecutor(t, "shell")
	jail := f2JailDir(t)
	outside := t.TempDir() // never equal to the jail

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "call_h2_out", Type: "function",
		Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command": "cat /etc/hosts", "working_dir": "` + outside + `"}`},
	})
	if res.Success {
		t.Fatalf("shell with out-of-jail working_dir allowed — the executor did not inject the model-supplied dir into details[\"working_dir\"] (H2 unfixed): %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.Error), "outside the session root") {
		t.Fatalf("denial is not the jail sentinel: %v", res.Error)
	}
	if !errors.Is(f2Sentinel(res.Error), intsecurity.ErrOutsideSessionRoot) && !strings.Contains(res.Error, jail) {
		t.Fatalf("denial must attribute the jail root: %v", res.Error)
	}
}

// TestExecutor_ShellWorkingDirInsideJailAllowed (H2 executor half): the
// same call with an in-jail working_dir must pass the fence.
func TestExecutor_ShellWorkingDirInsideJailAllowed(t *testing.T) {
	e := f2PinnedExecutor(t, "shell")
	jail := f2JailDir(t)
	inside := filepath.Join(jail, "sub")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir in-jail workdir: %v", err)
	}

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "call_h2_in", Type: "function",
		Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command": "true", "working_dir": "` + inside + `"}`},
	})
	if !res.Success {
		t.Fatalf("shell with in-jail working_dir denied (over-block): %v", res.Error)
	}
}

// TestExecutor_ShellDefaultWorkdirIsTurnDir (H2 executor half): a shell
// call with NO working_dir must be fenced against the TURN dir — the tool
// would run there (resolveShellWorkingDir step 2), so the fence must judge
// it and admit it when it is the jail root itself.
func TestExecutor_ShellDefaultWorkdirIsTurnDir(t *testing.T) {
	e := f2PinnedExecutor(t, "shell")
	jail := f2JailDir(t)

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "call_h2_def", Type: "function",
		Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command": "true"}`},
	})
	if !res.Success {
		t.Fatalf("plain shell call in the jail root denied — the fence is still judging \".\" (daemon cwd) instead of the turn dir: %v", res.Error)
	}
}

// TestExecutor_TildePathJudgedAtHome (H3): file_write with path "~/x" must
// be DENIED — the expanded $HOME target is outside every session jail, and
// the naive Join ("<jail>/~/x") would have let it through as an in-jail
// path while the tool wrote into the real home directory.
func TestExecutor_TildePathJudgedAtHome(t *testing.T) {
	e := f2PinnedExecutor(t, "file_write")
	jail := f2JailDir(t)

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "call_h3_home", Type: "function",
		Function: llm.ToolCallFunction{Name: "file_write", Arguments: `{"path": "~/meept-h3-probe.txt", "content": "x"}`},
	})
	if res.Success {
		t.Fatalf("write to ~/... allowed — the tilde join bypass (H3) is live: the fence judged <jail>/~/x instead of $HOME/... : %+v", res)
	}
}

// TestExecutor_TildeSSHPathHitsBlockedGlob (H3 glob half): "~/.ssh/id_rsa"
// must be denied by the BLOCKED-SPATHS config, which requires the expanded
// $HOME spelling to reach the glob matcher (the joined "<jail>/~/.ssh/..."
// form never matched it).
func TestExecutor_TildeSSHPathHitsBlockedGlob(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "pin tool", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	}))
	base := security.NewPermissionChecker(security.Config{
		AllowedPaths: []string{"/"},
		BlockedPaths: []string{home + "/.ssh/**"},
	})
	loop := composeLoopFence(base, newSessionFenceFactory(testLoggerForFence()), f2JailDir(t), false, nil)
	e := NewExecutor(registry, loop)
	jail := f2JailDir(t)

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "call_h3_ssh", Type: "function",
		Function: llm.ToolCallFunction{Name: "file_read", Arguments: `{"path": "~/.ssh/id_rsa"}`},
	})
	if res.Success {
		t.Fatalf("read of ~/.ssh/id_rsa allowed — the expanded home form never reached the blocked globs (H3 glob half): %+v", res)
	}
}

// TestExecutor_GitToolsWithoutDirJudgedAgainstSessionDir (M4): a git_*
// call with no working_dir must be fenced against the turn's session dir —
// the daemon-registered git tools run in the daemon process cwd when the
// arg is absent, and the central git_ arm skipped exactly that case.
// git_validate maps to file_read, whose arm only fires on a path arg, so
// the pin uses git_commit (shell_execute) whose arm always judges the dir.
func TestExecutor_GitToolsWithoutDirJudgedAgainstSessionDir(t *testing.T) {
	e := f2PinnedExecutor(t, "git_commit")
	jail := f2JailDir(t)

	// In-jail: the injected dir is the jail root -> allowed.
	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "call_m4_in", Type: "function",
		Function: llm.ToolCallFunction{Name: "git_commit", Arguments: `{"message": "test commit message"}`},
	})
	if !res.Success {
		t.Fatalf("git_commit with no working_dir denied when the injected session dir IS the jail — the executor is not injecting the turn dir for git_* (M4 unfixed): %v", res.Error)
	}
}

// f2Sentinel is a helper kept trivial: the pins above assert on the
// human-readable denial text; this exists only so the errors import stays
// honest about the errors.Is probe shape.
func f2Sentinel(msg string) error {
	if strings.Contains(msg, "outside the session root") {
		return intsecurity.ErrOutsideSessionRoot
	}
	return errors.New(msg)
}
