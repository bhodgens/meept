package security

// bughunt 2026-11-27 F2 pins — H2 fence-vs-shell key mismatch.
//
// Origin: audit 02-security-fence.md S-1. The shell_execute fence arm read
// details["workdir"], but the shell tool's arg key is "working_dir"
// (internal/tools/builtin/shell.go resolveShellWorkingDir). The fence then
// always judged "." — Abs'd against the DAEMON process cwd — and a
// model-supplied working_dir passed the path check entirely unchecked.
//
// Post-fix contract in this arm (permissions.go shell_execute case):
//   - details["working_dir"] (the tool's real key) is consulted FIRST;
//   - "workdir" stays a legacy alias fallback (existing pins and any
//     hand-built callers keep working);
//   - absent both -> "." (the legacy default).
//
// Non-vacuity: reverting the arm to workDir := details["workdir"] makes the
// working_dir deny pin pass the fence (Allowed=true) and the recorded
// CheckCommand workdir regress to ".".

import (
	"strings"
	"testing"
)

// TestFenceStage_ShellWorkingDirKeyPreferred: a working_dir OUTSIDE the
// fence must deny shell_execute — the arm must read the tool's real key,
// not just the legacy "workdir" alias.
func TestFenceStage_ShellWorkingDirKeyPreferred(t *testing.T) {
	fence := &fakeFence{denyWorkdirPrefix: "/tmp/outside", errText: "fence: command outside root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("shell_execute", map[string]string{
		"command":     "cat /etc/hosts",
		"working_dir": "/tmp/outside/etc",
	}, "")
	if got.Allowed {
		t.Fatalf("shell_execute with out-of-fence working_dir allowed — the arm is still reading only the legacy \"workdir\" key (H2 unfixed)")
	}
	if got.Reason != "fence: command outside root" {
		t.Fatalf("Reason = %q, want fence reason", got.Reason)
	}
	if got.RuleSource != "fence" {
		t.Fatalf("RuleSource = %q, want \"fence\"", got.RuleSource)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if !strings.Contains(calls, "path:/tmp/outside/etc:exec") {
		t.Fatalf("working_dir not judged as an exec path; calls = %v", fence.recordedCalls())
	}
	if strings.Contains(calls, "cmd:cat /etc/hosts:.") {
		t.Fatalf("CheckCommand still invoked with the legacy \".\" workdir; calls = %v", fence.recordedCalls())
	}
}

// TestFenceStage_ShellWorkingDirKeyAllowedInside: an in-fence working_dir
// must reach BOTH fence surfaces with the tool's key.
func TestFenceStage_ShellWorkingDirKeyAllowedInside(t *testing.T) {
	fence := &fakeFence{}
	pc := NewPermissionChecker(Config{AllowedPaths: []string{"/tmp"}})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("shell_execute", map[string]string{
		"command":     "make build",
		"working_dir": "/tmp/proj",
	}, "")
	if !got.Allowed || got.Reason != "Permitted" {
		t.Fatalf("in-fence working_dir denied: Allowed=%v Reason=%q", got.Allowed, got.Reason)
	}
	calls := strings.Join(fence.recordedCalls(), "|")
	if !strings.Contains(calls, "path:/tmp/proj:exec") {
		t.Fatalf("working_dir not fenced as exec path; calls = %v", fence.recordedCalls())
	}
	if !strings.Contains(calls, "cmd:make build:/tmp/proj") {
		t.Fatalf("command not routed to CheckCommand with the tool-resolved working_dir; calls = %v", fence.recordedCalls())
	}
}

// TestFenceStage_ShellWorkdirLegacyAliasStillHonored: the legacy "workdir"
// alias keeps its pre-fix behavior (existing pins in fence_stage_test.go
// depend on it).
func TestFenceStage_ShellWorkdirLegacyAliasStillHonored(t *testing.T) {
	fence := &fakeFence{denyWorkdirPrefix: "/tmp/outside", errText: "fence: command outside root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("shell_execute", map[string]string{
		"command": "make build",
		"workdir": "/tmp/outside/repo",
	}, "")
	if got.Allowed {
		t.Fatalf("legacy workdir alias no longer honored — the H2 fix must keep it as a fallback")
	}
}

// TestFenceStage_ShellAbsentWorkdirDefaultsToDot: no key at all keeps the
// legacy "." default (byte-identical posture for unbound callers).
func TestFenceStage_ShellAbsentWorkdirDefaultsToDot(t *testing.T) {
	fence := &fakeFence{}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("shell_execute", map[string]string{
		"command": "ls",
	}, "")
	if !got.Allowed || got.Reason != "Permitted" {
		t.Fatalf("absent workdir must keep the legacy \".\" default: Allowed=%v Reason=%q", got.Allowed, got.Reason)
	}
	if calls := fence.recordedCalls(); !strings.Contains(strings.Join(calls, "|"), "cmd:ls:.") {
		t.Fatalf("absent-workdir call did not default to \".\"; calls = %v", calls)
	}
}
