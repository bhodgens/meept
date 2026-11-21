// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/tools"
)

// JobStopTool cancels the in-flight execution of an ad-hoc job by handle. It
// never deletes a recurring schedule -- use schedule_delete for that.
type JobStopTool struct {
	tools.ToolDefaults
	sched *scheduler.Scheduler
}

// NewJobStopTool creates a new job_stop tool.
func NewJobStopTool(sched *scheduler.Scheduler) *JobStopTool {
	return &JobStopTool{sched: sched}
}

func (t *JobStopTool) Name() string { return "job_stop" }

func (t *JobStopTool) Category() string { return "scheduling" }

func (t *JobStopTool) Description() string {
	return "Cancel an in-flight job- handle returned by job_start. Idempotent: cancelling an already-settled job reports already_done. Does not delete recurring schedules (use schedule_delete)."
}

func (t *JobStopTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"handle": {
				Type:        schemaTypeString,
				Description: "The job- handle to cancel.",
			},
			"reason": {
				Type:        schemaTypeString,
				Description: "Optional cancellation reason recorded on the settled result.",
			},
		},
		Required: []string{"handle"},
	}
}

// JobStopResult is the result of cancelling a job's in-flight execution.
type JobStopResult struct {
	Handle   string `json:"handle"`
	Status   string `json:"status"`
	Stopped  bool   `json:"stopped"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (t *JobStopTool) Execute(_ context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return JobStopResult{Error: errSchedulerNotAvailable}, nil
	}

	handle, _ := args["handle"].(string)
	if strings.TrimSpace(handle) == "" {
		return JobStopResult{Error: "handle is required"}, nil
	}
	reason, _ := args["reason"].(string)

	status, stopped, exitCode, err := t.sched.CancelJob(handle, reason)
	if err != nil {
		// A not_found handle is a normal idempotent outcome, not an execute
		// error: surface it in the result like every other validation case.
		//nolint:nilerr // tool contract: failures are reported via the result Error field
		return JobStopResult{
			Handle:  handle,
			Status:  "not_found",
			Stopped: stopped,
			Error:   err.Error(),
		}, nil
	}

	// CancelJob returns the settled status when the run finished within the
	// grace period. The tool contract exposes only cancelled/already_done/
	// not_found: a job that settled on its own (succeeded/failed) reads as
	// already_done; an actually-cancelled run reads as cancelled.
	switch status {
	case scheduler.JobStatusSucceeded, scheduler.JobStatusFailed:
		status = "already_done"
	case scheduler.JobStatusCancelled, "already_done", "not_found":
	default:
		status = "already_done"
	}

	out := JobStopResult{
		Handle:  handle,
		Status:  status,
		Stopped: stopped,
	}
	// Pointer keeps a genuine 0 in JSON rather than dropped by omitempty.
	code := exitCode
	out.ExitCode = &code
	return out, nil
}

// Ensure JobStopTool implements the Tool interface.
var _ tools.Tool = (*JobStopTool)(nil)
