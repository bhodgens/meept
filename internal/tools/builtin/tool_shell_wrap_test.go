package builtin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
)

// TestShellJobShape_Executes pins bughunt 2026-11-24 P2 at the EXECUTION
// layer. schedule_create and cron_create used to store the raw multi-word
// shell line in ShellJobConfig.Command with no Args; exec.CommandContext
// treats Command as a binary NAME, so every multi-word scheduled shell job
// died with "exec: not found". The working twin (tool_job_start) wraps as
// {Command:"/bin/sh", Args:["-c", line]}; both tools now produce that shape.
// This pin runs the REAL ShellJob.Execute against both shapes.
func TestShellJobShape_Executes(t *testing.T) {
	line := "echo wrap-probe-ok"

	newShape := scheduler.JobConfig{
		ID:       "shape-new",
		Name:     "shape-new",
		Type:     scheduler.JobTypeShell,
		Schedule: "0 8 * * *",
		Enabled:  true,
		ShellConfig: &scheduler.ShellJobConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", line},
		},
	}
	oldShape := scheduler.JobConfig{
		ID:       "shape-old",
		Name:     "shape-old",
		Type:     scheduler.JobTypeShell,
		Schedule: "0 8 * * *",
		Enabled:  true,
		ShellConfig: &scheduler.ShellJobConfig{
			Command: line,
		},
	}

	oldJob, err := scheduler.CreateJob(oldShape, nil)
	if err != nil {
		t.Fatalf("CreateJob(old shape): %v", err)
	}
	oldErr := oldJob.Execute(context.Background())
	if oldErr == nil || !strings.Contains(oldErr.Error(), "not found") {
		t.Fatalf("old shape must fail with exec not found, got: %v", oldErr)
	}

	newJob, err := scheduler.CreateJob(newShape, nil)
	if err != nil {
		t.Fatalf("CreateJob(new shape): %v", err)
	}
	if err := newJob.Execute(context.Background()); err != nil {
		t.Fatalf("new shape must execute cleanly, got: %v", err)
	}
}

// waitForResult polls GetJobResult until the settled result lands or the
// deadline passes (poll the TERMINAL state, never a fixed sleep).
func waitForResult(t *testing.T, sched *scheduler.Scheduler, handle string) scheduler.JobExecutionResult {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		res, ok := sched.GetJobResult(handle)
		if ok && res.Status == scheduler.JobStatusSucceeded {
			return res
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s never reached completed within 15s", handle)
	return scheduler.JobExecutionResult{}
}

// TestScheduleCreateToolShellJobFires pins the tool-side half by firing
// the job the tool actually scheduled (RunNow through the real scheduler)
// and asserting the settled result. Pre-fix shape fails at fire with
// "not found"; the wrapped shape completes.
func TestScheduleCreateToolShellJobFires(t *testing.T) {
	sched, err := scheduler.NewScheduler(config.SchedulerConfig{}, nil, scheduler.WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	tool := NewScheduleCreateTool(sched)

	res, err := tool.Execute(ctx, map[string]any{
		"name":     "wrap probe",
		"job_type": "shell",
		"command":  "echo wrap-probe-ok",
		"schedule": "0 8 * * *",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	create := res.(ScheduleCreateResult)
	if !create.Success {
		t.Fatalf("expected success, got error %q", create.Error)
	}
	if err := sched.RunNow(create.JobID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	got := waitForResult(t, sched, create.JobID)
	if got.ExitCode != scheduler.ExitCodeOK {
		t.Fatalf("scheduled shell job must exit 0, got exit=%d err=%q", got.ExitCode, got.Error)
	}
}

func TestCronCreateToolShellJobFires(t *testing.T) {
	sched, err := scheduler.NewScheduler(config.SchedulerConfig{}, nil, scheduler.WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	tool := NewCronCreateTool(sched)

	res, err := tool.Execute(ctx, map[string]any{
		"name":     "cron wrap probe",
		"job_type": "shell",
		"command":  "echo cron-probe-ok",
		"interval": "daily",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	create := res.(CronCreateResult)
	if !create.Success {
		t.Fatalf("expected success, got error %q", create.Error)
	}
	if err := sched.RunNow(create.JobID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	got := waitForResult(t, sched, create.JobID)
	if got.ExitCode != scheduler.ExitCodeOK {
		t.Fatalf("scheduled cron shell job must exit 0, got exit=%d err=%q", got.ExitCode, got.Error)
	}
}
