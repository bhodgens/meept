// Package task provides task management for multi-agent orchestration.
package task

import (
	"encoding/json"
	"time"
)

// TaskState represents the current state of a task.
type TaskState string

const (
	StatePending   TaskState = "pending"
	StatePlanning  TaskState = "planning"
	StateExecuting TaskState = "executing"
	StateTesting   TaskState = "testing"
	StateCompleted TaskState = "completed"
	StateFailed    TaskState = "failed"
	StateCancelled TaskState = "cancelled"
)

func (s TaskState) String() string {
	return string(s)
}

// IsTerminal returns true if the task is in a terminal state.
func (s TaskState) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

// Task represents a unit of work that may spawn multiple jobs.
type Task struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	ProjectDir   string          `json:"project_dir,omitempty"`
	WorkspaceDir string          `json:"workspace_dir,omitempty"`
	State        TaskState       `json:"state"`
	GitRepo      string          `json:"git_repo,omitempty"`
	MemvidZone   string          `json:"memvid_zone,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`

	// Linked sessions
	LinkedSessions []string `json:"linked_sessions,omitempty"`

	// Job tracking
	TotalJobs     int `json:"total_jobs"`
	CompletedJobs int `json:"completed_jobs"`
	FailedJobs    int `json:"failed_jobs"`
}

// NewTask creates a new task with default values.
func NewTask(name, description string) *Task {
	now := time.Now().UTC()
	return &Task{
		ID:          generateTaskID(),
		Name:        name,
		Description: description,
		State:       StatePending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// WithProjectDir sets the project directory.
func (t *Task) WithProjectDir(dir string) *Task {
	t.ProjectDir = dir
	return t
}

// WithWorkspaceDir sets the workspace directory.
func (t *Task) WithWorkspaceDir(dir string) *Task {
	t.WorkspaceDir = dir
	return t
}

// WithGitRepo sets the git repository URL.
func (t *Task) WithGitRepo(repo string) *Task {
	t.GitRepo = repo
	return t
}

// WithMemvidZone sets the memvid zone name.
func (t *Task) WithMemvidZone(zone string) *Task {
	t.MemvidZone = zone
	return t
}

// WithMetadata sets task metadata.
func (t *Task) WithMetadata(metadata any) *Task {
	data, _ := json.Marshal(metadata)
	t.Metadata = data
	return t
}

// Progress returns the completion percentage (0-100).
func (t *Task) Progress() float64 {
	if t.TotalJobs == 0 {
		return 0
	}
	return float64(t.CompletedJobs) / float64(t.TotalJobs) * 100
}

// IsPending returns true if the task hasn't started.
func (t *Task) IsPending() bool {
	return t.State == StatePending
}

// IsActive returns true if the task is being worked on.
func (t *Task) IsActive() bool {
	return t.State == StatePlanning || t.State == StateExecuting || t.State == StateTesting
}

// IsComplete returns true if the task is finished (success or failure).
func (t *Task) IsComplete() bool {
	return t.State.IsTerminal()
}

// LinkSession links a session to this task.
func (t *Task) LinkSession(sessionID string) {
	for _, s := range t.LinkedSessions {
		if s == sessionID {
			return // Already linked
		}
	}
	t.LinkedSessions = append(t.LinkedSessions, sessionID)
	t.UpdatedAt = time.Now().UTC()
}

// UnlinkSession removes a session link.
func (t *Task) UnlinkSession(sessionID string) {
	for i, s := range t.LinkedSessions {
		if s == sessionID {
			t.LinkedSessions = append(t.LinkedSessions[:i], t.LinkedSessions[i+1:]...)
			t.UpdatedAt = time.Now().UTC()
			return
		}
	}
}

// IncrementJobs updates job counters.
func (t *Task) IncrementJobs() {
	t.TotalJobs++
	t.UpdatedAt = time.Now().UTC()
}

// CompleteJob increments the completed job counter.
func (t *Task) CompleteJob() {
	t.CompletedJobs++
	t.UpdatedAt = time.Now().UTC()
}

// FailJob increments the failed job counter.
func (t *Task) FailJob() {
	t.FailedJobs++
	t.UpdatedAt = time.Now().UTC()
}

// SetState updates the task state.
func (t *Task) SetState(state TaskState) {
	t.State = state
	t.UpdatedAt = time.Now().UTC()
}

// TaskSummary provides a lightweight view of a task.
type TaskSummary struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	State          TaskState `json:"state"`
	Progress       float64   `json:"progress"`
	LinkedSessions int       `json:"linked_sessions"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Summary returns a lightweight summary of the task.
func (t *Task) Summary() TaskSummary {
	return TaskSummary{
		ID:             t.ID,
		Name:           t.Name,
		State:          t.State,
		Progress:       t.Progress(),
		LinkedSessions: len(t.LinkedSessions),
		UpdatedAt:      t.UpdatedAt,
	}
}

func generateTaskID() string {
	return "task-" + time.Now().UTC().Format("20060102150405.000000000")
}
