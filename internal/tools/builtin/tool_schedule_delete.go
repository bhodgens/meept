// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/tools"
)

// ScheduleDeleteTool deletes a scheduled job.
type ScheduleDeleteTool struct {
	sched *scheduler.Scheduler
}

// NewScheduleDeleteTool creates a new schedule deletion tool.
func NewScheduleDeleteTool(sched *scheduler.Scheduler) *ScheduleDeleteTool {
	return &ScheduleDeleteTool{sched: sched}
}

func (t *ScheduleDeleteTool) Name() string { return "schedule_delete" }

func (t *ScheduleDeleteTool) Description() string {
	return "Delete a scheduled job by its ID. This removes the job from the scheduler permanently."
}

func (t *ScheduleDeleteTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"job_id": {
				Type:        "string",
				Description: "The job ID to delete.",
			},
		},
		Required: []string{"job_id"},
	}
}

// ScheduleDeleteResult is the result of deleting a scheduled job.
type ScheduleDeleteResult struct {
	Success bool   `json:"success"`
	JobID   string `json:"job_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (t *ScheduleDeleteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return ScheduleDeleteResult{
			Success: false,
			Error:   "scheduler not available",
		}, nil
	}

	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return ScheduleDeleteResult{
			Success: false,
			Error:   "job_id is required",
		}, nil
	}

	err := t.sched.Unschedule(jobID)
	if err != nil {
		return ScheduleDeleteResult{
			Success: false,
			JobID:   jobID,
			Error:   err.Error(),
		}, nil
	}

	return ScheduleDeleteResult{
		Success: true,
		JobID:   jobID,
	}, nil
}

// SchedulePauseTool pauses a scheduled job without deleting it.
type SchedulePauseTool struct {
	sched *scheduler.Scheduler
}

// NewSchedulePauseTool creates a new schedule pause tool.
func NewSchedulePauseTool(sched *scheduler.Scheduler) *SchedulePauseTool {
	return &SchedulePauseTool{sched: sched}
}

func (t *SchedulePauseTool) Name() string { return "schedule_pause" }

func (t *SchedulePauseTool) Description() string {
	return "Pause a scheduled job. The job remains configured but won't run until resumed."
}

func (t *SchedulePauseTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"job_id": {
				Type:        "string",
				Description: "The job ID to pause.",
			},
		},
		Required: []string{"job_id"},
	}
}

// SchedulePauseResult is the result of pausing a scheduled job.
type SchedulePauseResult struct {
	Success bool   `json:"success"`
	JobID   string `json:"job_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (t *SchedulePauseTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return SchedulePauseResult{
			Success: false,
			Error:   "scheduler not available",
		}, nil
	}

	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return SchedulePauseResult{
			Success: false,
			Error:   "job_id is required",
		}, nil
	}

	err := t.sched.PauseJob(jobID)
	if err != nil {
		return SchedulePauseResult{
			Success: false,
			JobID:   jobID,
			Error:   err.Error(),
		}, nil
	}

	return SchedulePauseResult{
		Success: true,
		JobID:   jobID,
	}, nil
}

// ScheduleResumeTool resumes a paused scheduled job.
type ScheduleResumeTool struct {
	sched *scheduler.Scheduler
}

// NewScheduleResumeTool creates a new schedule resume tool.
func NewScheduleResumeTool(sched *scheduler.Scheduler) *ScheduleResumeTool {
	return &ScheduleResumeTool{sched: sched}
}

func (t *ScheduleResumeTool) Name() string { return "schedule_resume" }

func (t *ScheduleResumeTool) Description() string {
	return "Resume a previously paused scheduled job."
}

func (t *ScheduleResumeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"job_id": {
				Type:        "string",
				Description: "The job ID to resume.",
			},
		},
		Required: []string{"job_id"},
	}
}

// ScheduleResumeResult is the result of resuming a scheduled job.
type ScheduleResumeResult struct {
	Success bool   `json:"success"`
	JobID   string `json:"job_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (t *ScheduleResumeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return ScheduleResumeResult{
			Success: false,
			Error:   "scheduler not available",
		}, nil
	}

	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return ScheduleResumeResult{
			Success: false,
			Error:   "job_id is required",
		}, nil
	}

	err := t.sched.ResumeJob(jobID)
	if err != nil {
		return ScheduleResumeResult{
			Success: false,
			JobID:   jobID,
			Error:   err.Error(),
		}, nil
	}

	return ScheduleResumeResult{
		Success: true,
		JobID:   jobID,
	}, nil
}

// ScheduleRunNowTool triggers immediate execution of a scheduled job.
type ScheduleRunNowTool struct {
	sched *scheduler.Scheduler
}

// NewScheduleRunNowTool creates a new schedule run-now tool.
func NewScheduleRunNowTool(sched *scheduler.Scheduler) *ScheduleRunNowTool {
	return &ScheduleRunNowTool{sched: sched}
}

func (t *ScheduleRunNowTool) Name() string { return "schedule_run_now" }

func (t *ScheduleRunNowTool) Description() string {
	return "Trigger immediate execution of a scheduled job, independent of its schedule."
}

func (t *ScheduleRunNowTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"job_id": {
				Type:        "string",
				Description: "The job ID to run immediately.",
			},
		},
		Required: []string{"job_id"},
	}
}

// ScheduleRunNowResult is the result of running a job immediately.
type ScheduleRunNowResult struct {
	Success bool   `json:"success"`
	JobID   string `json:"job_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (t *ScheduleRunNowTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return ScheduleRunNowResult{
			Success: false,
			Error:   "scheduler not available",
		}, nil
	}

	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return ScheduleRunNowResult{
			Success: false,
			Error:   "job_id is required",
		}, nil
	}

	err := t.sched.RunNow(jobID)
	if err != nil {
		return ScheduleRunNowResult{
			Success: false,
			JobID:   jobID,
			Error:   err.Error(),
		}, nil
	}

	return ScheduleRunNowResult{
		Success: true,
		JobID:   jobID,
	}, nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*ScheduleDeleteTool)(nil)
	_ tools.Tool = (*SchedulePauseTool)(nil)
	_ tools.Tool = (*ScheduleResumeTool)(nil)
	_ tools.Tool = (*ScheduleRunNowTool)(nil)
)
