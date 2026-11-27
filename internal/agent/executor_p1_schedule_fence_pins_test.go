package agent

// executor_p1_schedule_fence_pins_test.go — bughunt 2026-11-27 F2, P1
// (conservative branch): schedule_create / cron_create shell jobs had NO
// creation-time fence anywhere. The tool-side fence is inert (per-tool
// SetFenceChecker wiring retired, leaf 01 T3, never re-wired on the
// provider-registered scheduler tools) and the daemon's execution-time
// guard carries a nil fence phase. These pins lock the half this lane
// owns: the executor injects the tool-resolved working_dir and the
// permissions fence stage judges schedule_create/cron_create shell
// commands through the central seam (permissions.go task_write arm keyed
// on details["tool_name"]).
//
// OUT OF SCOPE (documented in
// .hermes/audits/2026-11-27-fixwave/F2-fence-shell.md): the execution-time
// half — threading a fence phase into scheduler.JobDependencies and the
// ShellCommandGuard construction at internal/daemon/components.go — needs
// internal/daemon + internal/scheduler edits this lane must not make.
//
// All pins run the REAL production fence (composeLoopFence ->
// NewSessionFenceStage) exactly as the daemon arms it.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

func p1Executor(t *testing.T, toolName string) *Executor {
	t.Helper()
	return f2PinnedExecutor(t, toolName)
}

// TestExecutor_ScheduleCreateCommandFenced (P1): a schedule_create shell
// job whose working_dir resolves OUTSIDE the session jail must be DENIED
// at creation time — pre-fix nothing judged the command at all.
func TestExecutor_ScheduleCreateCommandFenced(t *testing.T) {
	e := p1Executor(t, "schedule_create")
	jail := f2JailDir(t)
	outside := t.TempDir()

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "p1_out", Type: "function",
		Function: llm.ToolCallFunction{Name: "schedule_create", Arguments: `{"name": "p1", "job_type": "shell", "command": "cat /etc/hosts", "working_dir": "` + outside + `"}`},
	})
	if res.Success {
		t.Fatalf("schedule_create with out-of-jail working_dir allowed — the creation-time fence (P1) is not armed: %+v", res)
	}
}

// TestExecutor_ScheduleCreateInsideJailAllowed: an in-jail schedule_create
// passes — the fence must not over-block legitimate scheduling.
func TestExecutor_ScheduleCreateInsideJailAllowed(t *testing.T) {
	e := p1Executor(t, "schedule_create")
	jail := f2JailDir(t)
	inside := filepath.Join(jail, "work")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "p1_in", Type: "function",
		Function: llm.ToolCallFunction{Name: "schedule_create", Arguments: `{"name": "p1b", "job_type": "shell", "command": "make build", "working_dir": "` + inside + `"}`},
	})
	if !res.Success {
		t.Fatalf("schedule_create with in-jail working_dir denied (over-block): %v", res.Error)
	}
}

// TestExecutor_CronCreateDefaultDirIsTurnDir (P1): cron_create's shell case
// carries no working_dir schema key — the executor must inject the turn's
// session dir so the fence judges the directory the job would effectively
// run in. An in-jail turn dir passes; the pin proves the injection is live
// by running against the jail root itself.
func TestExecutor_CronCreateDefaultDirIsTurnDir(t *testing.T) {
	e := p1Executor(t, "cron_create")
	jail := f2JailDir(t)

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "p1_cron", Type: "function",
		Function: llm.ToolCallFunction{Name: "cron_create", Arguments: `{"name": "p1c", "interval": "1h", "job_type": "shell", "command": "true"}`},
	})
	if !res.Success {
		t.Fatalf("cron_create with no working_dir denied when the injected session dir IS the jail — the P1 executor injection is dead: %v", res.Error)
	}
}

// TestExecutor_PlainTaskWriteUnfenced (byte-identical control): a plain
// task_write call (no schedule/cron tool_name) must NOT reach the fence —
// the arm is keyed on the injected tool_name precisely so every other
// task_write caller stays on the legacy path.
func TestExecutor_PlainTaskWriteUnfenced(t *testing.T) {
	e := p1Executor(t, "schedule_delete")
	jail := f2JailDir(t)

	// schedule_delete maps to task_write but its name is neither of the two
	// fenced scheduler-creation tools; it carries no command/working_dir.
	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "p1_ctl", Type: "function",
		Function: llm.ToolCallFunction{Name: "schedule_delete", Arguments: `{"id": "job-1"}`},
	})
	if !res.Success {
		t.Fatalf("plain task_write-family call fenced — the P1 arm must key on schedule_create/cron_create only: %v", res.Error)
	}
}
