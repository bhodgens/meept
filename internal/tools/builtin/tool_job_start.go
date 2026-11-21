// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

const (
	// jobHandlePrefix is the frozen handle namespace for ad-hoc shell jobs
	// (distinct from the sched-/cron- ids used by schedule_create/cron_create).
	jobHandlePrefix = "job-"

	// jobHardTimeoutCap is the hard ceiling for every ad-hoc job. The
	// scheduler's dispatch contexts are capped at 30m too; a larger
	// timeout_seconds is silently reduced to this value.
	jobHardTimeoutCap = 30 * time.Minute

	// jobDefaultTimeout matches the ShellJob default (jobs.go:407-410).
	jobDefaultTimeout = 5 * time.Minute

	// jobImmediateLeadTime gives the cron engine a tiny window to register
	// a "run now" one-shot before its due instant, so robfig/cron's Next()
	// returns a non-zero time and the entry actually fires.
	jobImmediateLeadTime = 100 * time.Millisecond
)

// JobStartTool starts an ad-hoc shell job and returns an opaque job- handle.
// It is creation-time UX only: the binding guard runs at execution time in
// the scheduler (wired by the daemon); an optional creation-time fence check
// is an early rejection for commands that are already known to be blocked.
type JobStartTool struct {
	tools.ToolDefaults
	sched        *scheduler.Scheduler
	fenceChecker FenceChecker
}

// NewJobStartTool creates a new job_start tool.
func NewJobStartTool(sched *scheduler.Scheduler) *JobStartTool {
	return &JobStartTool{sched: sched}
}

// SetFenceChecker sets the optional creation-time fence checker. Nil-safe per
// the repo setter invariant (every Set* needs a nil guard).
func (t *JobStartTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

func (t *JobStartTool) Name() string { return "job_start" }

func (t *JobStartTool) Category() string { return "scheduling" }

func (t *JobStartTool) Description() string {
	return "Start a shell command as an ad-hoc job and return a job- handle for job_status/job_stop. The command runs under a hard 30-minute timeout; a larger timeout_seconds is reduced to 30 minutes."
}

func (t *JobStartTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropCommand: {
				Type:        schemaTypeString,
				Description: "Shell command to execute (run via /bin/sh -c).",
			},
			"working_dir": {
				Type:        schemaTypeString,
				Description: "Working directory for the command (optional).",
			},
			"timeout_seconds": {
				Type:        schemaTypeNumber,
				Description: "Timeout in seconds (default 300 = 5 minutes). Hard-capped at 1800 seconds (30 minutes); larger values are reduced to the cap.",
			},
			"at": {
				Type:        schemaTypeString,
				Description: "Run once at an RFC3339 timestamp (e.g. '2026-11-20T15:00:00Z'). Must be within the configured one-shot horizon. Mutually exclusive with every and cron.",
			},
			"every": {
				Type:        schemaTypeString,
				Description: "Recurring interval as a Go duration with a 60s floor (e.g. '5m'). Mutually exclusive with at and cron.",
			},
			"cron": {
				Type:        schemaTypeString,
				Description: "Recurring cron expression (e.g. '0 9 * * *'). Mutually exclusive with at and every.",
			},
		},
		Required: []string{schemaPropCommand},
	}
}

// JobStartResult is the result of starting an ad-hoc shell job.
type JobStartResult struct {
	Handle string `json:"handle,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (t *JobStartTool) Execute(_ context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return JobStartResult{Error: errSchedulerNotAvailable}, nil
	}

	command, _ := args[schemaPropCommand].(string)
	if strings.TrimSpace(command) == "" {
		return JobStartResult{Error: "command is required"}, nil
	}

	// Creation-time UX guard: reject commands that are already known to be
	// blocked before we mint a handle. The binding check remains the
	// execution-time guard wired by the daemon; this is an early refusal.
	if t.fenceChecker != nil {
		workDir, _ := args["working_dir"].(string)
		if workDir == "" {
			workDir = "."
		}
		if err := t.fenceChecker.CheckCommand(command, workDir); err != nil {
			return JobStartResult{ //nolint:nilerr // tool contract: validation failures are reported via the result Error field
				Error: fmt.Sprintf("shell command rejected by fence: %v", err),
			}, nil
		}
	}

	// Effective timeout = min(timeout_seconds, 30m); default 5m.
	timeout := jobDefaultTimeout
	if raw, ok := args["timeout_seconds"]; ok {
		secs, ok := numericSeconds(raw)
		if !ok || secs <= 0 {
			return JobStartResult{Error: "timeout_seconds must be a positive number"}, nil
		}
		if secs > jobHardTimeoutCap.Seconds() {
			secs = jobHardTimeoutCap.Seconds()
		}
		timeout = time.Duration(secs * float64(time.Second))
	}

	workDir, _ := args["working_dir"].(string)
	// Build the shell job: run the user command through /bin/sh -c. The raw
	// command is ALSO carried in GuardCommand so the execution-time guard
	// classifies the real shell source rather than the literal "/bin/sh".
	shellCfg := &scheduler.ShellJobConfig{
		Command:      "/bin/sh",
		Args:         []string{"-c", command},
		GuardCommand: command,
		WorkDir:      strings.TrimSpace(workDir),
		Timeout:      timeout,
		CaptureOut:   true,
	}

	// at/every/cron are mutually exclusive. Resolve the timing spec, raw
	// cron schedule and whether the job is immediate (running) or deferred
	// (scheduled).
	timing, schedule, status, err := jobStartTiming(args)
	if err != nil {
		return JobStartResult{ //nolint:nilerr // tool contract: validation failures are reported via the result Error field
			Error: err.Error(),
		}, nil
	}
	if schedule != "" {
		if err := t.sched.ValidateSchedule(schedule); err != nil {
			return JobStartResult{ //nolint:nilerr // tool contract: validation failures are reported via the result Error field
				Error: fmt.Sprintf("invalid cron: %v", err),
			}, nil
		}
	}

	handle := id.Generate(jobHandlePrefix)
	now := time.Now().UTC()
	cfg := scheduler.JobConfig{
		ID:          handle,
		Name:        handle,
		Type:        scheduler.JobTypeShell,
		Schedule:    schedule,
		Enabled:     true,
		CreatedAt:   now,
		Timing:      timing,
		ShellConfig: shellCfg,
	}

	if err := scheduler.ValidateJobConfig(cfg); err != nil {
		return JobStartResult{ //nolint:nilerr // tool contract: validation failures are reported via the result Error field
			Error: err.Error(),
		}, nil
	}
	if _, err := t.sched.ScheduleConfig(cfg); err != nil {
		// ErrOneShotBeyondHorizon (and any other schedule rejection) is a
		// validation failure: surface the scheduler's message verbatim.
		slog.Warn("job_start: failed to schedule job",
			"error", err, "handle", handle)
		return JobStartResult{ //nolint:nilerr // tool contract: validation failures are reported via the result Error field
			Error: err.Error(),
		}, nil
	}

	return JobStartResult{Handle: handle, Status: status}, nil
}

// jobStartTiming resolves the mutually-exclusive at/every/cron arguments into
// a TimingSpec (or raw cron Schedule). With no schedule argument it returns a
// TimingAt a hair in the future so the cron engine registers and fires an
// immediate one-shot; status is "running". Explicit at/every/cron yield
// "scheduled".
func jobStartTiming(args map[string]any) (timing *scheduler.TimingSpec, schedule, status string, err error) {
	at, _ := args["at"].(string)
	every, _ := args["every"].(string)
	cron, _ := args["cron"].(string)

	count := 0
	for _, s := range []string{at, every, cron} {
		if strings.TrimSpace(s) != "" {
			count++
		}
	}
	if count > 1 {
		return nil, "", "", fmt.Errorf("at, every and cron are mutually exclusive; provide at most one")
	}

	switch {
	case strings.TrimSpace(at) != "":
		spec, perr := scheduler.ParseTiming("at " + strings.TrimSpace(at))
		if perr != nil {
			return nil, "", "", fmt.Errorf("invalid at: %w", perr)
		}
		return spec, "", "scheduled", nil
	case strings.TrimSpace(every) != "":
		spec, perr := scheduler.ParseTiming("every " + strings.TrimSpace(every))
		if perr != nil {
			return nil, "", "", fmt.Errorf("invalid every: %w", perr)
		}
		return spec, "", "scheduled", nil
	case strings.TrimSpace(cron) != "":
		// A raw cron expression is carried in JobConfig.Schedule, not in the
		// Timing spec. A nil timing with a cron schedule is a recurring job.
		return nil, strings.TrimSpace(cron), "scheduled", nil
	default:
		now := time.Now().UTC().Add(jobImmediateLeadTime)
		return &scheduler.TimingSpec{Kind: scheduler.TimingAt, At: &now}, "", "running", nil
	}
}

// numericSeconds converts a JSON number (float64) or a numeric string into
// seconds. Tool arguments arrive as float64 from JSON decoding.
func numericSeconds(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// Ensure JobStartTool implements the Tool interface.
var _ tools.Tool = (*JobStartTool)(nil)
