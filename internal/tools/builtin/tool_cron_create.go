// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/tools"
)

// CronCreateTool creates a cron-style recurring job.
// This is a convenience wrapper around schedule_create with helpful cron expression parsing.
type CronCreateTool struct {
	sched *scheduler.Scheduler
}

// NewCronCreateTool creates a new cron creation tool.
func NewCronCreateTool(sched *scheduler.Scheduler) *CronCreateTool {
	return &CronCreateTool{sched: sched}
}

func (t *CronCreateTool) Name() string { return "cron_create" }

func (t *CronCreateTool) Description() string {
	return "Create a cron-style recurring job with human-readable scheduling options. Supports common intervals like 'daily', 'hourly', 'weekly', or custom cron expressions."
}

func (t *CronCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropName: {
				Type:        schemaTypeString,
				Description: "Human-readable name for the job.",
			},
			"interval": {
				Type:        schemaTypeString,
				Description: "Pre-defined interval: minutely, hourly, daily, weekly, monthly, or 'custom' to use cron_expr.",
				Enum:        []string{"minutely", "hourly", "daily", "weekly", "monthly", "custom"},
			},
			"cron_expr": {
				Type:        schemaTypeString,
				Description: "Custom cron expression (required if interval='custom'). Format: sec min hour dom month dow (e.g., '0 9 * * *' for daily at 9am).",
			},
			"at_time": {
				Type:        schemaTypeString,
				Description: "For daily/weekly/monthly: time to run (e.g., '9:00am', '14:30'). Uses 24-hour format if no am/pm specified.",
			},
			"day_of_week": {
				Type:        schemaTypeString,
				Description: "For weekly: day of week (monday, tuesday, etc.). Default: monday.",
			},
			"day_of_month": {
				Type:        schemaTypeNumber,
				Description: "For monthly: day of month (1-31). Default: 1.",
			},
			schemaPropJobType: {
				Type:        schemaTypeString,
				Description: "Type of job: agent, shell, or reminder.",
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
		},
		Required: []string{"name", "interval", "job_type"},
	}
}

// CronCreateResult is the result of creating a cron job.
type CronCreateResult struct {
	Success  bool   `json:"success"`
	JobID    string `json:"job_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (t *CronCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.sched == nil {
		return CronCreateResult{
			Success: false,
			Error:   errSchedulerNotAvailable,
		}, nil
	}

	name, _ := args[schemaPropName].(string)
	interval, _ := args["interval"].(string)
	jobType, _ := args["job_type"].(string)

	if name == "" {
		return CronCreateResult{
			Success: false,
			Error:   "name is required",
		}, nil
	}
	if interval == "" {
		return CronCreateResult{
			Success: false,
			Error:   "interval is required",
		}, nil
	}
	if jobType == "" {
		return CronCreateResult{
			Success: false,
			Error:   "job_type is required",
		}, nil
	}

	// Build cron expression from interval
	cronExpr, err := t.buildCronExpression(args)
	if err != nil {
		return CronCreateResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Validate the cron expression
	if err := t.sched.ValidateSchedule(cronExpr); err != nil {
		return CronCreateResult{
			Success: false,
			Error:   fmt.Sprintf("invalid cron expression: %v", err),
		}, nil
	}

	// Generate job ID
	jobID := fmt.Sprintf("cron-%d", time.Now().UnixNano())

	// Build job config
	cfg := scheduler.JobConfig{
		ID:        jobID,
		Name:      name,
		Schedule:  cronExpr,
		Type:      scheduler.JobType(jobType),
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
	}

	// Type-specific configuration
	switch scheduler.JobType(jobType) {
	case scheduler.JobTypeAgent:
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return CronCreateResult{
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
			return CronCreateResult{
				Success: false,
				Error:   "command is required for shell jobs",
			}, nil
		}
		cfg.ShellConfig = &scheduler.ShellJobConfig{
			Command:    command,
			CaptureOut: true,
		}

	case scheduler.JobTypeReminder:
		message, _ := args["message"].(string)
		if message == "" {
			return CronCreateResult{
				Success: false,
				Error:   "message is required for reminder jobs",
			}, nil
		}
		cfg.ReminderConfig = &scheduler.ReminderJobConfig{
			Message:  message,
			Priority: "normal",
		}
	}

	// Validate and schedule
	if err := scheduler.ValidateJobConfig(cfg); err != nil {
		return CronCreateResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	scheduledID, err := t.sched.ScheduleConfig(cfg)
	if err != nil {
		return CronCreateResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	return CronCreateResult{
		Success:  true,
		JobID:    scheduledID,
		Name:     name,
		Schedule: cronExpr,
	}, nil
}

// buildCronExpression builds a cron expression from the interval and optional args.
func (t *CronCreateTool) buildCronExpression(args map[string]any) (string, error) {
	interval, _ := args["interval"].(string)

	switch interval {
	case "minutely":
		return "* * * * *", nil // Every minute

	case "hourly":
		return "0 * * * *", nil // At the top of every hour

	case "daily":
		atTime, _ := args["at_time"].(string)
		if atTime == "" {
			atTime = "9:00am"
		}
		hour, minute, err := parseTime(atTime)
		if err != nil {
			return "", fmt.Errorf("invalid at_time: %w", err)
		}
		return fmt.Sprintf("%d %d * * *", minute, hour), nil

	case "weekly":
		atTime, _ := args["at_time"].(string)
		if atTime == "" {
			atTime = "9:00am"
		}
		hour, minute, err := parseTime(atTime)
		if err != nil {
			return "", fmt.Errorf("invalid at_time: %w", err)
		}
		dow, _ := args["day_of_week"].(string)
		if dow == "" {
			dow = "monday"
		}
		dowNum, err := dayOfWeekToNumber(dow)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * %d", minute, hour, dowNum), nil

	case "monthly":
		atTime, _ := args["at_time"].(string)
		if atTime == "" {
			atTime = "9:00am"
		}
		hour, minute, err := parseTime(atTime)
		if err != nil {
			return "", fmt.Errorf("invalid at_time: %w", err)
		}
		domFloat, _ := args["day_of_month"].(float64)
		dom := int(domFloat)
		if dom < 1 || dom > 31 {
			dom = 1
		}
		return fmt.Sprintf("%d %d %d * *", minute, hour, dom), nil

	case "custom":
		cronExpr, _ := args["cron_expr"].(string)
		if cronExpr == "" {
			return "", fmt.Errorf("cron_expr is required when interval='custom'")
		}
		return cronExpr, nil

	default:
		return "", fmt.Errorf("unknown interval: %s", interval)
	}
}

// parseTime parses a time string like "9:00am", "14:30", "9am" into hour and minute.
func parseTime(timeStr string) (hour, minute int, err error) {
	timeStr = strings.ToLower(strings.TrimSpace(timeStr))

	// Parse hour:minute format
	var hourStr, minStr string
	if strings.Contains(timeStr, ":") {
		parts := strings.Split(timeStr, ":")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("invalid time format: %s", timeStr)
		}
		hourStr = parts[0]
		minPart := parts[1]

		// Minute part might have am/pm attached (e.g., "30am")
		if strings.Contains(minPart, "am") || strings.Contains(minPart, "pm") {
			// Parse minute number before am/pm
			var minNum strings.Builder
			for _, ch := range minPart {
				if ch >= '0' && ch <= '9' {
					minNum.WriteString(string(ch))
				} else {
					break
				}
			}
			minStr = minNum.String()
		} else {
			minStr = minPart
		}
	} else {
		// No colon, just hour with possible am/pm (e.g., "9am")
		hourStr = timeStr
		minStr = "0"
	}

	// Parse hour
	h, err := parseInt(hourStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid hour: %s", hourStr)
	}

	// Parse minute
	m, err := parseInt(minStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minute: %s", minStr)
	}

	// Handle am/pm for hour-only formats
	if strings.Contains(timeStr, "pm") && !strings.Contains(timeStr, ":") && h != 12 {
		h += 12
	}
	if strings.Contains(timeStr, "am") && h == 12 {
		h = 0
	}

	return h, m, nil
}

// parseInt parses an integer from a string with support for trailing non-digits.
// Returns an error if no digits are found at the start of the string.
func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}

	var result int
	var foundDigit bool
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			result = result*10 + int(ch-'0')
			foundDigit = true
		} else {
			break
		}
	}

	if !foundDigit {
		return 0, fmt.Errorf("no numeric digits found in %q", s)
	}
	return result, nil
}

// dayOfWeekToNumber converts a day name to cron dow number (0-6, Sunday=0).
func dayOfWeekToNumber(day string) (int, error) {
	day = strings.ToLower(strings.TrimSpace(day))
	switch day {
	case "sunday", "sun":
		return 0, nil
	case "monday", "mon":
		return 1, nil
	case "tuesday", "tue":
		return 2, nil
	case "wednesday", "wed":
		return 3, nil
	case "thursday", "thu":
		return 4, nil
	case "friday", "fri":
		return 5, nil
	case "saturday", "sat":
		return 6, nil
	default:
		return 0, fmt.Errorf("invalid day of week: %s", day)
	}
}

// TerminateHint implements tools.TerminatingTool -- cron creation returns
// a confirmation that does not need LLM follow-up processing.
func (t *CronCreateTool) TerminateHint(args map[string]any) bool { return true }

// Ensure CronCreateTool implements the Tool interface
var _ tools.Tool = (*CronCreateTool)(nil)

// Ensure CronCreateTool implements TerminatingTool.
var _ tools.TerminatingTool = (*CronCreateTool)(nil)
