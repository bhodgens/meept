// Package queue provides a persistent job queue with SQLite backend.
package queue

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// Priority levels for jobs.
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 2
	PriorityHigh   Priority = 3
	PriorityUrgent Priority = 4
)

func (p Priority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityUrgent:
		return "urgent"
	default:
		return "unknown"
	}
}

// JobState represents the current state of a job.
type JobState string

const (
	StatePending    JobState = "pending"
	StateClaimed    JobState = "claimed"
	StateProcessing JobState = "processing"
	StateCompleted  JobState = "completed"
	StateFailed     JobState = "failed"
	StateDead       JobState = "dead" // Too many retries, moved to dead letter
)

// JobType categorizes the work to be done.
type JobType string

const (
	JobTypeOneOff      JobType = "one_off"      // Single execution job
	JobTypeProjectTask JobType = "project_task" // Part of a larger task
)

// Job represents a unit of work in the queue.
type Job struct {
	ID           string          `json:"id"`
	TaskID       string          `json:"task_id,omitempty"`  // Parent task (null for standalone)
	AgentID      string          `json:"agent_id,omitempty"` // Target agent (e.g., "coder", "planner")
	Type         JobType         `json:"type"`
	Priority     Priority        `json:"priority"`
	State        JobState        `json:"state"`
	Payload      json.RawMessage `json:"payload"`
	RequiredCaps []string        `json:"required_caps,omitempty"` // Required capabilities
	MaxRetries   int             `json:"max_retries"`
	RetryCount   int             `json:"retry_count"`
	ClaimedBy    string          `json:"claimed_by,omitempty"` // Worker ID
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DueAt        *time.Time      `json:"due_at,omitempty"`
	NextRetryAt  *time.Time      `json:"next_retry_at,omitempty"` // Retry backoff time
}

// NewJob creates a new job with default values.
func NewJob(jobType JobType, payload any) (*Job, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &Job{
		ID:         generateJobID(),
		Type:       jobType,
		Priority:   PriorityNormal,
		State:      StatePending,
		Payload:    payloadJSON,
		MaxRetries: 3,
		RetryCount: 0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// WithPriority sets the job priority.
func (j *Job) WithPriority(p Priority) *Job {
	j.Priority = p
	return j
}

// WithTaskID associates the job with a task.
func (j *Job) WithTaskID(taskID string) *Job {
	j.TaskID = taskID
	return j
}

// WithRequiredCaps sets the required capabilities.
func (j *Job) WithRequiredCaps(caps []string) *Job {
	j.RequiredCaps = caps
	return j
}

// WithMaxRetries sets the maximum retry count.
func (j *Job) WithMaxRetries(n int) *Job {
	j.MaxRetries = n
	return j
}

// WithAgentID sets the target agent for this job.
func (j *Job) WithAgentID(agentID string) *Job {
	j.AgentID = agentID
	return j
}

// WithDueAt sets when the job should be executed.
func (j *Job) WithDueAt(t time.Time) *Job {
	j.DueAt = &t
	return j
}

// CanBeClaimedBy checks if a worker with the given capabilities can claim this job.
func (j *Job) CanBeClaimedBy(workerCaps []string) bool {
	if len(j.RequiredCaps) == 0 {
		return true
	}

	capSet := make(map[string]bool)
	for _, capName := range workerCaps {
		capSet[capName] = true
	}

	for _, required := range j.RequiredCaps {
		if !capSet[required] {
			return false
		}
	}
	return true
}

// IsPending returns true if the job is waiting to be claimed.
func (j *Job) IsPending() bool {
	return j.State == StatePending
}

// IsComplete returns true if the job finished (successfully or not).
func (j *Job) IsComplete() bool {
	return j.State == StateCompleted || j.State == StateFailed || j.State == StateDead
}

// CanRetry returns true if the job can be retried.
func (j *Job) CanRetry() bool {
	return j.RetryCount < j.MaxRetries
}

var jobIDCounter atomic.Uint64

// generateJobID creates a unique job ID using timestamp + atomic counter.
func generateJobID() string {
	seq := jobIDCounter.Add(1)
	return fmt.Sprintf("job-%s-%04d", time.Now().UTC().Format("20060102150405.000000000"), seq)
}
