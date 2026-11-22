package security

import (
	"strings"
	"testing"
)

// Pins for the job_start fence-stage case (permissions.go fence stage):
// job_start schedules a shell job (/bin/sh -c) exactly like shell_execute,
// so the fence must consult it on the same two surfaces — the working_dir
// (as an "exec" path check) and the command — using the job tool's own arg
// names (working_dir, not workdir).
//
// One-line failing reverts:
//   - TestFenceStage_JobStart_Deny fails: delete the `case "job_start"` arm
//     in permissions.go — the fence is never consulted and the allow leaks.
//   - TestFenceStage_JobStart_Allowed fails: same revert — the allow still
//     holds but no fence call is recorded (wantCall assertions fail).
//   - Existing pins (fence_stage_test.go) stay green under either revert.

// TestFenceStage_JobStart_Deny: a fence that rejects the job's working_dir
// must deny job_start with the fence reason, same posture as shell_execute.
func TestFenceStage_JobStart_Deny(t *testing.T) {
	fence := &fakeFence{denyWorkdirPrefix: "/tmp/outside", errText: "fence: command outside root"}
	pc := NewPermissionChecker(Config{})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("job_start", map[string]string{
		"command":     "make build",
		"working_dir": "/tmp/outside/repo",
	}, "")
	if got.Allowed {
		t.Fatalf("job_start fence deny not enforced")
	}
	if got.Reason != "fence: command outside root" {
		t.Fatalf("Reason = %q, want fence reason", got.Reason)
	}
	if got.RuleSource != "fence" {
		t.Fatalf("RuleSource = %q, want \"fence\"", got.RuleSource)
	}
}

// TestFenceStage_JobStart_Allowed: inside the fence, job_start passes and
// routes BOTH fence surfaces — CheckPath(working_dir, "exec") and
// CheckCommand(command, working_dir) — with the job tool's arg names.
func TestFenceStage_JobStart_Allowed(t *testing.T) {
	fence := &fakeFence{}
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/tmp"},
	})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("job_start", map[string]string{
		"command":     "make build",
		"working_dir": "/tmp/proj",
	}, "")
	if !got.Allowed || got.Reason != "Permitted" {
		t.Fatalf("job_start inside fence denied: Allowed=%v Reason=%q", got.Allowed, got.Reason)
	}

	calls := fence.recordedCalls()
	if !strings.Contains(strings.Join(calls, "|"), "path:/tmp/proj:exec") {
		t.Fatalf("working_dir not fenced as exec path; calls = %v", calls)
	}
	if !strings.Contains(strings.Join(calls, "|"), "cmd:make build:/tmp/proj") {
		t.Fatalf("command not routed to CheckCommand with working_dir; calls = %v", calls)
	}
}
