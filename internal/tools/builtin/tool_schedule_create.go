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

// ScheduleCreateTool creates a new scheduled job.
type ScheduleCreateTool struct {
	sched *scheduler.Scheduler
}

// NewScheduleCreateTool creates a new schedule creation tool.
func NewScheduleCreateTool(sched *scheduler.Scheduler) *ScheduleCreateTool {
	return &ScheduleCreateTool{sched: sched}
}

func (t *ScheduleCreateTool) Name() string { return "schedule_create" }

func (t *ScheduleCreateTool) Description() string {
	return "Create a new scheduled job. Jobs run on a cron-like schedule and can trigger agent tasks, shell commands, reminders, or other recurring operations."
}

func (t *ScheduleCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropName: {
				Type:        schemaTypeString,
				Description: "Human-readable name for the job.",
			},
			"schedule": {
				Type:        schemaTypeString,
				Description: "Cron expression for when to run the job (e.g., '0 9 * * *' for daily at 9am, '*/5 * * * *' for every 5 minutes). Supports 6-field cron with seconds.",
			},
			schemaPropJobType: {
				Type:        schemaTypeString,
				Description: "Type of job: agent (runs an agent prompt), shell (executes a shell command), reminder (sends a reminder message).",
				Enum:        []string{"agent", schemaJobTypeShell, "reminder"},
			},
			"prompt": {
				Type:        schemaTypeString,
				Description: "For agent jobs: the prompt/message to send to the agent.",
			},
			schemaPropCommand: {
				Type:        schemaTypeString,
				Description: "For shell jobs: the shell command to execute.",
			},
			schemaPropMessage: {
				Type:        schemaTypeString,
				Description: "For reminder jobs: the reminder message to send.",
			},
			"channels": {
				Type:        "array",
				Description: "For reminder jobs: list of channels to send to (e.g., ['notification', 'telegram']).",
			},
			"working_dir": {
				Type:        schemaTypeString,
				Description: "For shell jobs: working directory for the command.",
			},
			"enabled": {
				Type:        schemaTypeBoolean,
				Description: "Whether the job is enabled immediately (default true).",
			},
		},
		Required: []string{"name", "schedule", "job_type"},
	}
}

// ScheduleCreateResult is the result of creating a scheduled job.
type ScheduleCreateResult struct {
	Success bool   `json:"success"`
	JobID   string `json:"job_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (t *ScheduleCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return ScheduleCreateResult{
			Success: false,
			Error:   errSchedulerNotAvailable,
		}, nil
	}

	name, _ := args[schemaPropName].(string)
	schedule, _ := args["schedule"].(string)
	jobType, _ := args["job_type"].(string)

	if name == "" {
		return ScheduleCreateResult{
			Success: false,
			Error:   "name is required",
		}, nil
	}
	if schedule == "" {
		return ScheduleCreateResult{
			Success: false,
			Error:   "schedule is required",
		}, nil
	}
	if jobType == "" {
		return ScheduleCreateResult{
			Success: false,
			Error:   "job_type is required",
		}, nil
	}

	// Generate job ID
	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())

	// Build job config based on type
	cfg := scheduler.JobConfig{
		ID:        jobID,
		Name:      name,
		Schedule:  schedule,
		Type:      scheduler.JobType(jobType),
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
	}

	// Set enabled flag if provided
	if enabled, ok := args["enabled"].(bool); ok {
		cfg.Enabled = enabled
	}

	// Type-specific configuration
	switch scheduler.JobType(jobType) {
	case scheduler.JobTypeAgent:
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return ScheduleCreateResult{
				Success: false,
				Error:   "prompt is required for agent jobs",
			}, nil
		}
		cfg.AgentConfig = &scheduler.AgentJobConfig{
			Prompt: prompt,
		}

	case scheduler.JobTypeShell:
		command, _ := args["command"].(string)
		if command == "" {
			return ScheduleCreateResult{
				Success: false,
				Error:   "command is required for shell jobs",
			}, nil
		}
		shellCfg := &scheduler.ShellJobConfig{
			Command:    command,
			CaptureOut: true,
		}
		if workDir, ok := args["working_dir"].(string); ok {
			shellCfg.WorkDir = workDir
		}
		cfg.ShellConfig = shellCfg

	case scheduler.JobTypeReminder:
		message, _ := args["message"].(string)
		if message == "" {
			return ScheduleCreateResult{
				Success: false,
				Error:   "message is required for reminder jobs",
			}, nil
		}
		reminderCfg := &scheduler.ReminderJobConfig{
			Message:  message,
			Priority: "normal",
		}
		if channels, ok := args["channels"].([]any); ok && len(channels) > 0 {
			chs := make([]string, 0, len(channels))
			for _, c := range channels {
				if cs, ok := c.(string); ok {
					chs = append(chs, cs)
				}
			}
			if len(chs) > 0 {
				reminderCfg.Channels = chs
			}
		}
		cfg.ReminderConfig = reminderCfg

	default:
		return ScheduleCreateResult{
			Success: false,
			Error:   fmt.Sprintf("unsupported job type: %s", jobType),
		}, nil
	}

	// Validate and schedule
	if err := scheduler.ValidateJobConfig(cfg); err != nil {
		return ScheduleCreateResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	scheduledID, err := t.sched.ScheduleConfig(cfg)
	if err != nil {
		return ScheduleCreateResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	return ScheduleCreateResult{
		Success: true,
		JobID:   scheduledID,
		Name:    name,
	}, nil
}

// Ensure ScheduleCreateTool implements the Tool interface
var _ tools.Tool = (*ScheduleCreateTool)(nil)

// TerminateHint implements tools.TerminatingTool -- schedule creation returns
// a confirmation that does not need LLM follow-up processing.
func (t *ScheduleCreateTool) TerminateHint(args map[string]any) bool { return true }

// Ensure ScheduleCreateTool implements TerminatingTool.
var _ tools.TerminatingTool = (*ScheduleCreateTool)(nil)
