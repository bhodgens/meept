// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/tools"
)

// ScheduleListTool lists scheduled jobs.
type ScheduleListTool struct {
	sched *scheduler.Scheduler
}

// NewScheduleListTool creates a new schedule list tool.
func NewScheduleListTool(sched *scheduler.Scheduler) *ScheduleListTool {
	return &ScheduleListTool{sched: sched}
}

func (t *ScheduleListTool) Name() string { return "schedule_list" }

func (t *ScheduleListTool) Category() string { return "scheduling" }

func (t *ScheduleListTool) Description() string {
	return "List all scheduled jobs with their schedules, next run times, and status. Optionally filter by job type."
}

func (t *ScheduleListTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropJobType: {
				Type:        schemaTypeString,
				Description: "Optional filter by job type: agent, shell, reminder, optimization, security, learning.",
				Enum:        []string{"agent", schemaJobTypeShell, "reminder", "optimization", "security", "learning", ""},
			},
			"enabled_only": {
				Type:        schemaTypeBoolean,
				Description: "If true, only return enabled jobs.",
			},
		},
		Required: []string{},
	}
}

// ScheduledJobInfo represents a scheduled job.
type ScheduledJobInfo struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Schedule  string  `json:"schedule"`
	Enabled   bool    `json:"enabled"`
	NextRun   *string `json:"next_run,omitempty"`
	LastRun   *string `json:"last_run,omitempty"`
	LastError string  `json:"last_error,omitempty"`
	RunCount  int64   `json:"run_count"`
	IsRunning bool    `json:"is_running"`
}

// ScheduleListResult is the result of listing scheduled jobs.
type ScheduleListResult struct {
	Jobs  []ScheduledJobInfo `json:"jobs"`
	Count int                `json:"count"`
}

func (t *ScheduleListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return ScheduleListResult{
			Jobs:  []ScheduledJobInfo{},
			Count: 0,
		}, nil
	}

	jobTypeFilter, _ := args["job_type"].(string)
	enabledOnly, _ := args["enabled_only"].(bool)

	allJobs := t.sched.ListJobs()

	jobs := make([]ScheduledJobInfo, 0, len(allJobs))
	for _, job := range allJobs {
		// Filter by job type if specified
		if jobTypeFilter != "" && string(job.Type) != jobTypeFilter {
			continue
		}

		// Filter by enabled status if specified
		if enabledOnly && !job.Enabled {
			continue
		}

		info := ScheduledJobInfo{
			ID:        job.ID,
			Name:      job.Name,
			Type:      string(job.Type),
			Schedule:  job.Schedule,
			Enabled:   job.Enabled,
			LastError: job.LastError,
			RunCount:  job.RunCount,
			IsRunning: job.IsRunning,
		}

		if job.NextRun != nil {
			nextRun := job.NextRun.Format(time.RFC3339)
			info.NextRun = &nextRun
		}

		if job.LastRun != nil {
			lastRun := job.LastRun.Format(time.RFC3339)
			info.LastRun = &lastRun
		}

		jobs = append(jobs, info)
	}

	return ScheduleListResult{
		Jobs:  jobs,
		Count: len(jobs),
	}, nil
}

// ScheduleGetTool gets details of a specific scheduled job.
type ScheduleGetTool struct {
	sched *scheduler.Scheduler
}

// NewScheduleGetTool creates a new schedule get tool.
func NewScheduleGetTool(sched *scheduler.Scheduler) *ScheduleGetTool {
	return &ScheduleGetTool{sched: sched}
}

func (t *ScheduleGetTool) Name() string { return "schedule_get" }

func (t *ScheduleGetTool) Category() string { return "scheduling" }

func (t *ScheduleGetTool) Description() string {
	return "Get detailed information about a specific scheduled job by its ID."
}

func (t *ScheduleGetTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropJobID: {
				Type:        schemaTypeString,
				Description: "The job ID to retrieve.",
			},
		},
		Required: []string{schemaPropJobID},
	}
}

func (t *ScheduleGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return nil, fmt.Errorf("scheduler not available")
	}

	jobID, _ := args[schemaPropJobID].(string)
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	job, ok := t.sched.GetJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	info := ScheduledJobInfo{
		ID:        job.ID,
		Name:      job.Name,
		Type:      string(job.Type),
		Schedule:  job.Schedule,
		Enabled:   job.Enabled,
		LastError: job.LastError,
		RunCount:  job.RunCount,
		IsRunning: job.IsRunning,
	}

	if job.NextRun != nil {
		nextRun := job.NextRun.Format(time.RFC3339)
		info.NextRun = &nextRun
	}

	if job.LastRun != nil {
		lastRun := job.LastRun.Format(time.RFC3339)
		info.LastRun = &lastRun
	}

	return info, nil
}

// TerminateHint implements tools.TerminatingTool -- schedule listing returns
// read-only data that does not need LLM follow-up processing.
func (t *ScheduleListTool) TerminateHint(args map[string]any) bool { return true }

// TerminateHint implements tools.TerminatingTool.
func (t *ScheduleGetTool) TerminateHint(args map[string]any) bool { return true }

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*ScheduleListTool)(nil)
	_ tools.Tool = (*ScheduleGetTool)(nil)

	// Ensure schedule listing tools implement TerminatingTool
	_ tools.TerminatingTool = (*ScheduleListTool)(nil)
	_ tools.TerminatingTool = (*ScheduleGetTool)(nil)
)
