// Package lite provides the lightweight meept-lite TUI components.
package lite

import (
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// RPCClient defines the interface for RPC communication with the daemon.
// This interface is compatible with internal/tui.RPCClient.
type RPCClient interface {
	ListTasks(state string, limit int) (*types.TaskListResponse, error)
	GetTask(taskID string) (*types.Task, error)
	ListTasksExtended() (*types.TaskExtendedListResponse, error)
	ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)
	IsConnected() bool
}

// TaskManager handles task-related commands for meept-lite.
type TaskManager struct {
	rpc RPCClient
}

// NewTaskManager creates a new TaskManager with the given RPC client.
func NewTaskManager(rpc RPCClient) *TaskManager {
	return &TaskManager{rpc: rpc}
}

// List returns all tasks, optionally filtered by session.
// If sessionID is empty, returns all tasks.
func (t *TaskManager) List(sessionID string) ([]types.Task, error) {
	if !t.rpc.IsConnected() {
		return nil, fmt.Errorf("not connected to daemon")
	}

	// Try extended task list first for richer data
	extResp, err := t.rpc.ListTasksExtended()
	if err == nil && extResp != nil {
		var tasks []types.Task
		for _, ext := range extResp.Tasks {
			// Filter by session if specified
			if sessionID != "" {
				found := false
				for _, linked := range ext.LinkedSessions {
					if linked == sessionID {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			tasks = append(tasks, ext.Task)
		}
		return tasks, nil
	}

	// Fallback to regular task list
	resp, err := t.rpc.ListTasks("", 100)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	if sessionID == "" {
		return resp.Tasks, nil
	}

	// For regular task list, filter by LinkedSessions
	var filtered []types.Task
	for _, task := range resp.Tasks {
		for _, linked := range task.LinkedSessions {
			if linked == sessionID {
				filtered = append(filtered, task)
				break
			}
		}
	}
	return filtered, nil
}

// Get returns a specific task by ID.
func (t *TaskManager) Get(taskID string) (*types.Task, error) {
	if !t.rpc.IsConnected() {
		return nil, fmt.Errorf("not connected to daemon")
	}

	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	task, err := t.rpc.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return task, nil
}

// Cancel cancels a task.
// Note: This requires the Cancel RPC method which may need to be added to the RPC client.
// For now, this returns an error indicating the feature is not yet available.
func (t *TaskManager) Cancel(taskID string) error {
	if !t.rpc.IsConnected() {
		return fmt.Errorf("not connected to daemon")
	}

	if taskID == "" {
		return fmt.Errorf("task ID is required")
	}

	// TODO: Implement when task.cancel RPC method is available
	return fmt.Errorf("task cancellation not yet implemented")
}

// FormatTaskList formats tasks for display in viewport.
func (t *TaskManager) FormatTaskList(tasks []types.Task) string {
	if len(tasks) == 0 {
		return "no tasks found"
	}

	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("%-36s  %-8s  %-20s  %s\n",
		"id", "state", "name", "progress"))
	b.WriteString(strings.Repeat("-", 80) + "\n")

	for _, task := range tasks {
		name := task.Name
		if name == "" {
			name = truncate(task.ID, 20)
		} else {
			name = truncate(name, 20)
		}

		stateIcon := t.getStateIcon(task.State)
		progress := t.formatProgress(task.CompletedJobs, task.TotalJobs)

		b.WriteString(fmt.Sprintf("%-36s  %s  %-20s  %s\n",
			truncate(task.ID, 36),
			stateIcon,
			name,
			progress,
		))
	}

	b.WriteString(fmt.Sprintf("\n%d task(s) total", len(tasks)))
	return b.String()
}

// FormatTaskDetail formats a single task for display.
func (t *TaskManager) FormatTaskDetail(task *types.Task) string {
	if task == nil {
		return "task not found"
	}

	var b strings.Builder

	// Title
	name := task.Name
	if name == "" {
		name = task.ID
	}
	b.WriteString(fmt.Sprintf("task: %s\n", name))
	b.WriteString(strings.Repeat("=", 60) + "\n\n")

	// Basic info
	b.WriteString(fmt.Sprintf("%-14s %s\n", "id:", task.ID))
	b.WriteString(fmt.Sprintf("%-14s %s\n", "state:", t.getStateIcon(task.State)))

	if task.Description != "" {
		b.WriteString(fmt.Sprintf("%-14s %s\n", "description:", task.Description))
	}

	// Dates
	b.WriteString(fmt.Sprintf("%-14s %s\n", "created:", t.formatTimestamp(task.CreatedAt)))
	b.WriteString(fmt.Sprintf("%-14s %s\n", "updated:", t.formatTimestamp(task.UpdatedAt)))

	// Progress section
	b.WriteString("\n--- progress ---\n")
	progress := t.formatProgressBar(task.CompletedJobs, task.TotalJobs, 30)
	b.WriteString(fmt.Sprintf("%s\n", progress))
	b.WriteString(fmt.Sprintf("completed: %d  pending: %d  failed: %d\n",
		task.CompletedJobs,
		task.TotalJobs-task.CompletedJobs-task.FailedJobs,
		task.FailedJobs,
	))

	// Workspace info
	if task.ProjectDir != "" || task.WorkspaceDir != "" || task.GitRepo != "" {
		b.WriteString("\n--- workspace ---\n")
		if task.ProjectDir != "" {
			b.WriteString(fmt.Sprintf("project:   %s\n", task.ProjectDir))
		}
		if task.WorkspaceDir != "" {
			b.WriteString(fmt.Sprintf("workspace: %s\n", task.WorkspaceDir))
		}
		if task.GitRepo != "" {
			b.WriteString(fmt.Sprintf("git repo:  %s\n", task.GitRepo))
		}
	}

	// Linked sessions
	if len(task.LinkedSessions) > 0 {
		b.WriteString("\n--- linked sessions ---\n")
		for _, sess := range task.LinkedSessions {
			b.WriteString(fmt.Sprintf("  - %s\n", sess))
		}
	}

	// Memory zone
	if task.MemvidZone != "" {
		b.WriteString(fmt.Sprintf("\n--- memory ---\nmemvid zone: %s\n", task.MemvidZone))
	}

	// Try to get steps
	if t.rpc.IsConnected() {
		stepsResp, err := t.rpc.ListTaskSteps(task.ID)
		if err == nil && stepsResp != nil && len(stepsResp.Steps) > 0 {
			b.WriteString("\n--- steps ---\n")
			for _, step := range stepsResp.Steps {
				stepIcon := t.getStepStateIcon(step.State)
				agent := step.AgentID
				if agent == "" {
					agent = "-"
				}
				desc := truncate(step.Description, 40)
				b.WriteString(fmt.Sprintf("%2d. %s [%s] %s\n",
					step.Sequence, stepIcon, agent, desc))
			}
		}
	}

	return b.String()
}

// FormatTaskExtendedDetail formats a TaskExtended with memory context for display.
func (t *TaskManager) FormatTaskExtendedDetail(task *types.TaskExtended) string {
	if task == nil {
		return "task not found"
	}

	// Start with base task formatting
	var b strings.Builder

	// Title
	name := task.Name
	if name == "" {
		name = task.ID
	}
	b.WriteString(fmt.Sprintf("task: %s\n", name))
	b.WriteString(strings.Repeat("=", 60) + "\n\n")

	// Basic info
	b.WriteString(fmt.Sprintf("%-14s %s\n", "id:", task.ID))
	b.WriteString(fmt.Sprintf("%-14s %s\n", "state:", t.getStateIcon(task.State)))

	if task.AssignedAgent != "" {
		b.WriteString(fmt.Sprintf("%-14s %s\n", "agent:", task.AssignedAgent))
	}

	if task.Description != "" {
		b.WriteString(fmt.Sprintf("%-14s %s\n", "description:", task.Description))
	}

	// Dates
	b.WriteString(fmt.Sprintf("%-14s %s\n", "created:", t.formatTimestamp(task.CreatedAt)))
	b.WriteString(fmt.Sprintf("%-14s %s\n", "updated:", t.formatTimestamp(task.UpdatedAt)))

	// Progress section
	b.WriteString("\n--- progress ---\n")
	progress := t.formatProgressBar(task.CompletedJobs, task.TotalJobs, 30)
	b.WriteString(fmt.Sprintf("%s\n", progress))
	b.WriteString(fmt.Sprintf("completed: %d  pending: %d  failed: %d\n",
		task.CompletedJobs,
		task.TotalJobs-task.CompletedJobs-task.FailedJobs,
		task.FailedJobs,
	))

	// Memory context section
	hasMemoryContext := len(task.MemoryRefs) > 0 ||
		task.ContextQuery != "" ||
		task.InheritedFrom != "" ||
		len(task.CreatedMemories) > 0

	if hasMemoryContext {
		b.WriteString("\n--- memory context ---\n")

		if task.InheritedFrom != "" {
			b.WriteString(fmt.Sprintf("inherited from: %s\n", task.InheritedFrom))
		}

		if len(task.MemoryRefs) > 0 {
			b.WriteString(fmt.Sprintf("memory refs:    %d\n", len(task.MemoryRefs)))
			for _, ref := range task.MemoryRefs {
				b.WriteString(fmt.Sprintf("  - %s\n", truncate(ref, 50)))
			}
		}

		if task.ContextQuery != "" {
			b.WriteString(fmt.Sprintf("context query:  \"%s\"\n", task.ContextQuery))
		}

		if len(task.CreatedMemories) > 0 {
			b.WriteString(fmt.Sprintf("created:        %d memories\n", len(task.CreatedMemories)))
			for _, mem := range task.CreatedMemories {
				b.WriteString(fmt.Sprintf("  - %s\n", truncate(mem, 50)))
			}
		}
	}

	// Steps section (inline from TaskExtended)
	if len(task.Steps) > 0 {
		b.WriteString("\n--- steps ---\n")
		for _, step := range task.Steps {
			stepIcon := t.getStepStateIcon(step.State)
			agent := step.AgentID
			if agent == "" {
				agent = "-"
			}
			desc := truncate(step.Description, 40)

			// Include progress for running steps
			progressIndicator := ""
			switch step.State {
			case "running":
				progressIndicator = " [running]"
			case "reviewing":
				progressIndicator = " [review]"
			}

			b.WriteString(fmt.Sprintf("%2d. %s [%s] %s%s\n",
				step.Sequence, stepIcon, agent, desc, progressIndicator))

			// Show dependencies if blocked
			if step.State == "pending" && len(step.DependsOn) > 0 {
				b.WriteString(fmt.Sprintf("    (blocked by: %s)\n",
					strings.Join(step.DependsOn, ", ")))
			}
		}
	}

	// Workspace info
	if task.ProjectDir != "" || task.WorkspaceDir != "" || task.GitRepo != "" {
		b.WriteString("\n--- workspace ---\n")
		if task.ProjectDir != "" {
			b.WriteString(fmt.Sprintf("project:   %s\n", task.ProjectDir))
		}
		if task.WorkspaceDir != "" {
			b.WriteString(fmt.Sprintf("workspace: %s\n", task.WorkspaceDir))
		}
		if task.GitRepo != "" {
			b.WriteString(fmt.Sprintf("git repo:  %s\n", task.GitRepo))
		}
	}

	// Linked sessions
	if len(task.LinkedSessions) > 0 {
		b.WriteString("\n--- linked sessions ---\n")
		for _, sess := range task.LinkedSessions {
			b.WriteString(fmt.Sprintf("  - %s\n", sess))
		}
	}

	return b.String()
}

// getStateIcon returns a text state indicator for a task state.
func (t *TaskManager) getStateIcon(state string) string {
	switch state {
	case "pending":
		return "o pend"
	case "planning":
		return "* plan"
	case "executing":
		return "> exec"
	case "testing":
		return "? test"
	case "completed":
		return "+ done"
	case "failed":
		return "x fail"
	case "cancelled":
		return "- stop"
	default:
		return "? " + truncate(state, 4)
	}
}

// getStepStateIcon returns a text state indicator for a step state.
func (t *TaskManager) getStepStateIcon(state string) string {
	switch state {
	case "pending":
		return "o"
	case "ready":
		return ">"
	case "scheduled":
		return "*"
	case "running":
		return ">"
	case "reviewing":
		return "?"
	case "approved":
		return "+"
	case "rejected":
		return "~"
	case "completed":
		return "+"
	case "failed":
		return "x"
	case "skipped":
		return "-"
	default:
		return "?"
	}
}

// formatProgress returns a compact progress string.
func (t *TaskManager) formatProgress(completed, total int) string {
	if total == 0 {
		return "-/-"
	}
	percent := float64(completed) / float64(total) * 100
	return fmt.Sprintf("%d/%d (%.0f%%)", completed, total, percent)
}

// formatProgressBar renders an ASCII progress bar.
func (t *TaskManager) formatProgressBar(completed, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("-", width) + "] -/-"
	}

	percent := float64(completed) / float64(total)
	filled := int(percent * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := "[" + strings.Repeat("#", filled) + strings.Repeat("-", empty) + "]"
	return fmt.Sprintf("%s %d/%d (%.0f%%)", bar, completed, total, percent*100)
}

// formatTimestamp formats an ISO timestamp for display.
func (t *TaskManager) formatTimestamp(timestamp string) string {
	if timestamp == "" {
		return "n/a"
	}

	// Try to parse common formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	var parsed time.Time
	var err error
	for _, format := range formats {
		parsed, err = time.Parse(format, timestamp)
		if err == nil {
			break
		}
	}

	if err != nil {
		// Return truncated raw timestamp if parsing fails
		return truncate(timestamp, 19)
	}

	// Format as relative time if recent
	diff := time.Since(parsed)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	default:
		return parsed.Format("2006-01-02 15:04")
	}
}

// truncate truncates a string to the given length with ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
