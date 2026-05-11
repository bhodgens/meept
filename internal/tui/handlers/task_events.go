// Package handlers provides shared event handling logic for TUI components.
package handlers

import (
	"fmt"
	"strings"
)

// TaskEventPayload represents common task event data.
type TaskEventPayload struct {
	TaskID        string        `json:"task_id"`
	Name          string        `json:"name"`
	CompletedJobs int           `json:"completed_jobs"`
	TotalJobs     int           `json:"total_jobs"`
	FailedJobs    int           `json:"failed_jobs"`
	CurrentStep   string        `json:"current_step"`
	Result        string        `json:"result"`
	Error         string        `json:"error"`
	ExecutionTime string        `json:"execution_time"`
	Steps         []StepSummary `json:"steps"`
}

// StepSummary represents a step in task event payloads.
type StepSummary struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	State       string `json:"state"`
	Result      string `json:"result"`
	AgentID     string `json:"agent_id"`
}

// TaskNotification represents a processed notification ready for display.
type TaskNotification struct {
	Type    string // "completed", "failed", "progress", "step_completed"
	Message string
	TaskID  string
}

// TaskEventHandler processes task events and produces notifications.
type TaskEventHandler struct{}

// NewTaskEventHandler creates a new task event handler.
func NewTaskEventHandler() *TaskEventHandler {
	return &TaskEventHandler{}
}

// HandleTaskCompleted processes a task.completed event.
func (h *TaskEventHandler) HandleTaskCompleted(payload map[string]any) *TaskNotification {
	name := getString(payload, "name", "task")
	result := getString(payload, "result", "")
	executionTime := getString(payload, "execution_time", "")

	var sb strings.Builder
	fmt.Fprintf(&sb, "task completed: %s", strings.ToLower(name))
	if result != "" {
		fmt.Fprintf(&sb, "\n%s", result)
	}
	if executionTime != "" {
		fmt.Fprintf(&sb, " (%s)", executionTime)
	}

	return &TaskNotification{
		Type:    "completed",
		Message: sb.String(),
		TaskID:  getString(payload, "task_id", ""),
	}
}

// HandleTaskFailed processes a task.failed event.
func (h *TaskEventHandler) HandleTaskFailed(payload map[string]any) *TaskNotification {
	name := getString(payload, "name", "task")
	errMsg := getString(payload, "error", "")
	failed := getInt(payload, "failed_jobs", 0)
	completed := getInt(payload, "completed_jobs", 0)
	total := getInt(payload, "total_jobs", 0)

	var sb strings.Builder
	fmt.Fprintf(&sb, "task failed: %s", strings.ToLower(name))
	if total > 0 {
		fmt.Fprintf(&sb, "\nprogress: %d/%d completed, %d failed", completed, total, failed)
	}
	if errMsg != "" {
		errPreview := errMsg
		if len(errPreview) > 100 {
			errPreview = errPreview[:97] + "..."
		}
		fmt.Fprintf(&sb, "\nerror: %s", errPreview)
	}

	return &TaskNotification{
		Type:    "failed",
		Message: sb.String(),
		TaskID:  getString(payload, "task_id", ""),
	}
}

// HandleTaskProgress processes a task.progress event.
// No rate limiting - all progress events are delivered.
func (h *TaskEventHandler) HandleTaskProgress(payload map[string]any) *TaskNotification {
	taskID := getString(payload, "task_id", "")
	currentStep := getString(payload, "current_step", "")

	if currentStep == "" {
		return nil
	}

	completed := getInt(payload, "completed_jobs", 0)
	total := getInt(payload, "total_jobs", 0)

	var sb strings.Builder
	if total > 0 {
		fmt.Fprintf(&sb, "task progress [%d/%d]: ", completed, total)
	} else {
		sb.WriteString("task progress: ")
	}
	sb.WriteString(strings.ToLower(currentStep))

	return &TaskNotification{
		Type:    "progress",
		Message: sb.String(),
		TaskID:  taskID,
	}
}

// HandleStepCompleted processes a task.step_completed event.
// Returns nil if step info is insufficient for display.
func (h *TaskEventHandler) HandleStepCompleted(payload map[string]any) *TaskNotification {
	desc := getString(payload, "description", "")
	if desc == "" {
		return nil
	}

	result := getString(payload, "result", "")

	var sb strings.Builder
	fmt.Fprintf(&sb, "step completed: %s", strings.ToLower(desc))
	if result != "" && len(result) < 60 {
		fmt.Fprintf(&sb, "\n  %s", result)
	}

	return &TaskNotification{
		Type:    "step_completed",
		Message: sb.String(),
		TaskID:  getString(payload, "task_id", ""),
	}
}

// Helper functions for extracting values from payload maps

func getString(payload map[string]any, key, defaultVal string) string {
	if v, ok := payload[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func getInt(payload map[string]any, key string, defaultVal int) int {
	if v, ok := payload[key].(float64); ok {
		return int(v)
	}
	if v, ok := payload[key].(int); ok {
		return v
	}
	return defaultVal
}
