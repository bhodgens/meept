package security

import (
	"strings"
	"testing"
)

// Pins for the H9/A6-7 central file_path seam (permissions.go) and the
// F8 git_* working_dir fence arm.
//
// The 18 code-intelligence tools (internal/code/tools: ast_parse/
// ast_symbols/ast_query/ast_edit/ast_resolve/resolve_ast_edit + the
// lsp_* family; internal/tools/builtin/lsp_navigate.go) pass their file
// path under key file_path. Before the fix, the central glob checks and
// the fence stage read ONLY details["path"], so a fenced/blocked read of
// /etc/shadow via ast_parse{file_path:"/etc/shadow"} sailed through.
//
// One-line failing reverts:
//   - TestFilePathArg_FenceDeny fails: revert the file_* fence arm to
//     details["path"] — the fence is never consulted for file_path and
//     the deny leaks (allowed).
//   - TestFilePathArg_GlobBlocked fails: same revert of the glob branch —
//     the blocked-glob verdict leaks.
//   - TestFilePathArg_PathWins fails: drop the "path wins" precedence in
//     filePathArg (check file_path first) — the both-keys call starts
//     consulting the file_path value and the recorded path changes.
//   - TestGitWorkingDir_FenceDeny fails: delete the git_* tool_name arm —
//     git_validate's working_dir never reaches fence.CheckPath.
//   - Existing pins (fence_stage_test.go, permissions_jobstart_test.go)
//     stay green under every revert above.

// --- F1: file_path normalization on the central seam ---

func TestFilePathArg_FenceDeny(t *testing.T) {
	fence := &fakeFence{denyPathPrefix: "/etc", errText: "fence: read outside project root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	// file_read-class action whose ONLY key is file_path, outside the fence.
	got := pc.CheckPermissionForAgent("file_read", map[string]string{
		"file_path": "/etc/shadow",
		"tool_name": "lsp_hover",
	}, "")
	if got.Allowed {
		t.Fatalf("file_path-only read outside the fence was allowed — central fence blind to file_path (H9)")
	}
	if got.RuleSource != "fence" {
		t.Fatalf("RuleSource = %q, want \"fence\"", got.RuleSource)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if !strings.Contains(calls, "path:/etc/shadow:read") {
		t.Fatalf("fence not consulted with the file_path value; calls = %s", calls)
	}
}

func TestFilePathArg_FenceAllowedInside(t *testing.T) {
	fence := &fakeFence{denyPathPrefix: "/etc", errText: "fence: read outside project root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("file_read", map[string]string{
		"file_path": "/proj/main.go",
		"tool_name": "ast_parse",
	}, "")
	if !got.Allowed || got.Reason != "Permitted" {
		t.Fatalf("file_path inside the fence denied: Allowed=%v Reason=%q", got.Allowed, got.Reason)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if !strings.Contains(calls, "path:/proj/main.go:read") {
		t.Fatalf("inside-fence path not fenced; calls = %s", calls)
	}
}

func TestFilePathArg_WriteDeleteOps(t *testing.T) {
	// file_write/file_delete arms must map file_path to the write/delete op
	// exactly like the path arm did.
	fence := &fakeFence{denyPathPrefix: "/etc", errText: "fence: no"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("file_write", map[string]string{
		"file_path": "/etc/cron.d/x", "tool_name": "ast_edit",
	}, "")
	if got.Allowed || got.RuleSource != "fence" {
		t.Fatalf("file_write file_path outside fence: Allowed=%v Source=%q", got.Allowed, got.RuleSource)
	}
	got = pc.CheckPermissionForAgent("file_delete", map[string]string{
		"file_path": "/etc/important", "tool_name": "lsp_rename",
	}, "")
	if got.Allowed || got.RuleSource != "fence" {
		t.Fatalf("file_delete file_path outside fence: Allowed=%v Source=%q", got.Allowed, got.RuleSource)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if !strings.Contains(calls, "path:/etc/cron.d/x:write") || !strings.Contains(calls, "path:/etc/important:delete") {
		t.Fatalf("op mapping wrong; calls = %s", calls)
	}
}

func TestFilePathArg_PathWins(t *testing.T) {
	// An action carrying BOTH keys: path wins (byte-identical legacy
	// posture — the fence is consulted ONLY on the path value).
	fence := &fakeFence{denyPathPrefix: "/etc", errText: "fence: no"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("file_read", map[string]string{
		"path":      "/proj/ok.go",
		"file_path": "/etc/shadow",
		"tool_name": "ast_query",
	}, "")
	if !got.Allowed {
		t.Fatalf("both-keys call denied (file_path consulted instead of path?): Reason=%q", got.Reason)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if strings.Contains(calls, "/etc/shadow") {
		t.Fatalf("file_path consulted while path present — precedence broken; calls = %s", calls)
	}
	if !strings.Contains(calls, "path:/proj/ok.go:read") {
		t.Fatalf("path not fenced; calls = %s", calls)
	}
}

func TestFilePathArg_GlobBlocked(t *testing.T) {
	// The glob (blocked-path) half of the seam: a blocked pattern must
	// catch a file_path-only call too.
	pc := NewPermissionChecker(Config{
		BlockedPaths: []string{"/etc/**"},
	})
	got := pc.CheckPermissionForAgent("file_read", map[string]string{
		"file_path": "/etc/shadow", "tool_name": "lsp_diagnostics",
	}, "")
	if got.Allowed {
		t.Fatalf("file_path hit a blocked glob but was allowed (H9 glob half)")
	}
	if got.RuleSource != "base_rule" || !strings.Contains(got.Reason, "blocked pattern") {
		t.Fatalf("verdict = %+v, want blocked-glob base_rule deny", got)
	}
}

// --- F8: git_* working_dir fence arm ---

func TestGitWorkingDir_FenceDeny(t *testing.T) {
	fence := &fakeFence{denyPathPrefix: "/tmp/outside", errText: "fence: git dir outside root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	// git_validate maps to file_read (ToolActionMap); the arm must fire on
	// the tool_name prefix regardless of the action class.
	got := pc.CheckPermissionForAgent("file_read", map[string]string{
		"tool_name":   "git_validate",
		"working_dir": "/tmp/outside/repo",
	}, "")
	if got.Allowed {
		t.Fatalf("git_* working_dir outside the fence was allowed")
	}
	if got.RuleSource != "fence" {
		t.Fatalf("RuleSource = %q, want \"fence\"", got.RuleSource)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if !strings.Contains(calls, "path:/tmp/outside/repo:exec") {
		t.Fatalf("working_dir not fenced as exec; calls = %s", calls)
	}
}

func TestGitWorkingDir_ShellClassAlsoFenced(t *testing.T) {
	// git_split maps to shell_execute: the shell arm checks workdir
	// (empty here -> "."), and the git arm must additionally police
	// working_dir.
	fence := &fakeFence{denyPathPrefix: "/tmp/outside", errText: "fence: git dir outside root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("shell_execute", map[string]string{
		"tool_name":   "git_split",
		"working_dir": "/tmp/outside/repo",
	}, "")
	if got.Allowed || got.RuleSource != "fence" {
		t.Fatalf("git_split working_dir verdict = %+v, want fence deny", got)
	}
}

func TestGitWorkingDir_AbsentKeySkipped(t *testing.T) {
	// No working_dir key = the tool runs in its own session-bound cwd;
	// the git arm adds nothing (byte-identical for non-path calls).
	fence := &fakeFence{}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("file_read", map[string]string{
		"tool_name": "git_validate",
		"message":   "fix(x): thing",
	}, "")
	if !got.Allowed {
		t.Fatalf("git_validate without working_dir denied: Reason=%q", got.Reason)
	}
	for _, c := range fence.recordedCalls() {
		if strings.Contains(c, "git") {
			t.Fatalf("unexpected fence call %q", c)
		}
	}
}

func TestGitWorkingDir_NonGitToolUnaffected(t *testing.T) {
	// A non-git tool carrying working_dir must NOT hit the new arm
	// (scope pin: the change is prefix-gated on tool_name).
	fence := &fakeFence{denyWorkdirPrefix: "/tmp", errText: "fence: no"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("task_read", map[string]string{
		"tool_name":   "schedule_get",
		"working_dir": "/tmp/anything",
	}, "")
	if !got.Allowed {
		t.Fatalf("non-git action policed by git arm: Reason=%q", got.Reason)
	}
}
