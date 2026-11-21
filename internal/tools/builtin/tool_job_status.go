// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/tools"
)

// JobStatusTool reads a settled ad-hoc job result and a bounded output window
// from the job's spill file (the full output never lives in memory).
type JobStatusTool struct {
	tools.ToolDefaults
	sched *scheduler.Scheduler
}

// NewJobStatusTool creates a new job_status tool.
func NewJobStatusTool(sched *scheduler.Scheduler) *JobStatusTool {
	return &JobStatusTool{sched: sched}
}

func (t *JobStatusTool) Name() string { return "job_status" }

func (t *JobStatusTool) Category() string { return "scheduling" }

func (t *JobStatusTool) Description() string {
	return "Read the settled status, exit code and a bounded stdout/stderr window for a job- handle returned by job_start."
}

func (t *JobStatusTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"handle": {
				Type:        schemaTypeString,
				Description: "The job- handle returned by job_start.",
			},
		},
		Required: []string{"handle"},
	}
}

// JobStatusResult is the result of reading a job's settled state.
type JobStatusResult struct {
	Handle     string `json:"handle"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

func (t *JobStatusTool) Execute(_ context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return JobStatusResult{Error: errSchedulerNotAvailable}, nil
	}

	handle, _ := args["handle"].(string)
	if strings.TrimSpace(handle) == "" {
		return JobStatusResult{Error: "handle is required"}, nil
	}

	result, ok := t.sched.GetJobResult(handle)
	if !ok {
		// A known job with no settled result is simply still running; an
		// unknown handle is a genuine not-found. Both omit output/exit code.
		if _, known := t.sched.GetJob(handle); known {
			return JobStatusResult{Handle: handle, Status: "running"}, nil
		}
		return JobStatusResult{ //nolint:nilerr // tool contract: validation failures are reported via the result Error field
			Handle: handle,
			Status: "not_found",
			Error:  "no result for job: " + handle,
		}, nil
	}

	out := JobStatusResult{
		Handle: handle,
		Status: result.Status,
		Error:  result.Error,
	}
	if !result.StartedAt.IsZero() {
		out.StartedAt = result.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !result.FinishedAt.IsZero() {
		out.FinishedAt = result.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	// Exit code is a pointer so a genuine 0 is emitted in JSON rather than
	// omitted by omitempty (the e2e gate asserts `"exit_code":0`).
	exitCode := result.ExitCode
	out.ExitCode = &exitCode

	// Read at most the bounded window from the spill file on demand. A
	// missing/evicted spill file is not a failure: the settled row above
	// still carries status + exit code.
	output, err := t.sched.JobResultOutput(handle, scheduler.DefaultJobResultOutputBytes)
	if err != nil {
		slog.Warn("job_status: read job output", "error", err, "handle", handle)
	} else if len(output) > 0 {
		out.Output = string(output)
	}

	return out, nil
}

// Ensure JobStatusTool implements the Tool interface.
var _ tools.Tool = (*JobStatusTool)(nil)
