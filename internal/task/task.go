// Package task provides task management for multi-agent orchestration.
package task

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"
	"time"
)

// TaskState represents the current state of a task.
//
//nolint:revive // stutter with package name is intentional for API clarity
type TaskState string

const (
	StatePending          TaskState = "pending"
	StatePlanning         TaskState = "planning"
	StateAwaitingApproval TaskState = "awaiting_approval"
	StateExecuting        TaskState = "executing"
	StateTesting          TaskState = "testing"
	StateCompleted        TaskState = "completed"
	StateFailed           TaskState = "failed"
	StateRejected         TaskState = "rejected"
	StateCancelled        TaskState = "cancelled"
)

func (s TaskState) String() string {
	return string(s)
}

// IsTerminal returns true if the task is in a terminal state.
func (s TaskState) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled || s == StateRejected
}

// Task represents a unit of work that may spawn multiple jobs.
//
//nolint:revive // stutter with package name is intentional for API clarity
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
	StartedAt    time.Time       `json:"started_at,omitempty,omitzero"` //nolint:modernize // omitzero already applied
	UpdatedAt    time.Time       `json:"updated_at"`

	// Linked sessions
	LinkedSessions []string `json:"linked_sessions,omitempty"`

	// Job tracking
	TotalJobs     int `json:"total_jobs"`
	CompletedJobs int `json:"completed_jobs"`
	FailedJobs    int `json:"failed_jobs"`

	// Memory context for agent continuity
	// MemoryRefs are explicit memory IDs passed to the agent.
	MemoryRefs []string `json:"memory_refs,omitempty"`
	// ContextQuery is an auto-search query for additional context.
	ContextQuery string `json:"context_query,omitempty"`
	// InheritedFrom is the parent task ID this task was derived from.
	InheritedFrom string `json:"inherited_from,omitempty"`
	// CreatedMemories are memory IDs created during task execution.
	CreatedMemories []string `json:"created_memories,omitempty"`
	// AssignedAgent is the agent ID assigned to this task.
	AssignedAgent string `json:"assigned_agent,omitempty"`
	// TokenUsage tracks total tokens consumed during task execution.
	TokenUsage int `json:"token_usage,omitempty"`
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
	if slices.Contains(t.LinkedSessions, sessionID) {
		return // Already linked
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
	// Set StartedAt when transitioning from pending to an active state
	if t.State == StatePending && (state == StatePlanning || state == StateExecuting || state == StateAwaitingApproval) {
		t.StartedAt = time.Now().UTC()
	}
	t.State = state
	t.UpdatedAt = time.Now().UTC()
}

// ExecutionTime returns the duration since task started executing.
// Returns zero duration if task hasn't started yet.
func (t *Task) ExecutionTime() time.Duration {
	if t.StartedAt.IsZero() {
		return 0
	}
	if t.State.IsTerminal() {
		return t.UpdatedAt.Sub(t.StartedAt)
	}
	return time.Since(t.StartedAt)
}

// TaskSummary provides a lightweight view of a task.
//
//nolint:revive // stutter with package name is intentional for API clarity
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

// WithMemoryRefs sets explicit memory references for context.
func (t *Task) WithMemoryRefs(refs []string) *Task {
	t.MemoryRefs = refs
	return t
}

// WithContextQuery sets the auto-search query for additional context.
func (t *Task) WithContextQuery(query string) *Task {
	t.ContextQuery = query
	return t
}

// WithInheritedFrom sets the parent task ID.
func (t *Task) WithInheritedFrom(parentID string) *Task {
	t.InheritedFrom = parentID
	return t
}

// WithAssignedAgent sets the assigned agent.
func (t *Task) WithAssignedAgent(agentID string) *Task {
	t.AssignedAgent = agentID
	return t
}

// AddMemoryRef adds a memory reference to the task.
func (t *Task) AddMemoryRef(ref string) {
	if slices.Contains(t.MemoryRefs, ref) {
		return // Already exists
	}
	t.MemoryRefs = append(t.MemoryRefs, ref)
	t.UpdatedAt = time.Now().UTC()
}

// AddCreatedMemory records a memory created during execution.
func (t *Task) AddCreatedMemory(memoryID string) {
	if slices.Contains(t.CreatedMemories, memoryID) {
		return // Already exists
	}
	t.CreatedMemories = append(t.CreatedMemories, memoryID)
	t.UpdatedAt = time.Now().UTC()
}

// HasMemoryRefs returns true if the task has memory references.
func (t *Task) HasMemoryRefs() bool {
	return len(t.MemoryRefs) > 0
}

// HasContextQuery returns true if the task has a context query.
func (t *Task) HasContextQuery() bool {
	return t.ContextQuery != ""
}

// AddTokenUsage adds tokens to the task's running total.
func (t *Task) AddTokenUsage(tokens int) {
	t.TokenUsage += tokens
	t.UpdatedAt = time.Now().UTC()
}

var taskIDCounter uint64

func generateTaskID() string {
	seq := atomic.AddUint64(&taskIDCounter, 1)
	return fmt.Sprintf("task-%s-%04d", time.Now().UTC().Format("20060102150405.000000000"), seq)
}
