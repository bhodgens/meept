package services

import (
	"context"
	"errors"

	"github.com/caimlas/meept/internal/queue"
)

// QueueService wraps the queue.Queue interface for cross-transport access.
type QueueService struct {
	q queue.Queue
}

// NewQueueService creates a queue service.
func NewQueueService(q queue.Queue) *QueueService {
	return &QueueService{q: q}
}

// EnqueueRequest contains queue enqueue parameters.
type EnqueueRequest struct {
	Type         string         `json:"type"`
	Priority     int            `json:"priority,omitempty"`
	TaskID       string         `json:"task_id,omitempty"`
	Prompt       string         `json:"prompt"`
	SessionID    string         `json:"session_id,omitempty"`
	RequiredCaps []string       `json:"required_caps,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
}

// Enqueue adds a job to the queue.
func (s *QueueService) Enqueue(ctx context.Context, req EnqueueRequest) (*queue.Job, error) {
	if req.Prompt == "" {
		return nil, wrapError("queue", "Enqueue", ErrInvalidInput)
	}

	jobType := queue.JobTypeOneOff
	if req.Type == string(queue.JobTypeProjectTask) {
		jobType = queue.JobTypeProjectTask
	}

	payload := req.Payload
	if payload == nil {
		payload = make(map[string]any)
	}
	if req.Prompt != "" {
		payload["prompt"] = req.Prompt
	}
	if req.SessionID != "" {
		payload["session_id"] = req.SessionID
	}

	job, err := queue.NewJob(jobType, payload)
	if err != nil {
		return nil, wrapError("queue", "Enqueue", err)
	}

	if req.Priority > 0 {
		job.WithPriority(queue.Priority(req.Priority))
	}
	if req.TaskID != "" {
		job.WithTaskID(req.TaskID)
	}
	if len(req.RequiredCaps) > 0 {
		job.WithRequiredCaps(req.RequiredCaps)
	}

	if err := s.q.Enqueue(ctx, job); err != nil {
		return nil, wrapError("queue", "Enqueue", err)
	}

	return job, nil
}

// ClaimRequest contains claim parameters.
type ClaimRequest struct {
	WorkerID     string   `json:"worker_id"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Claim claims the next available job for a worker.
func (s *QueueService) Claim(ctx context.Context, req ClaimRequest) (*queue.Job, error) {
	if req.WorkerID == "" {
		return nil, wrapError("queue", "Claim", ErrInvalidInput)
	}

	job, err := s.q.Claim(ctx, req.WorkerID, req.Capabilities)
	if err != nil {
		if errors.Is(err, queue.ErrNoJobAvailable) {
			return nil, nil
		}
		return nil, wrapError("queue", "Claim", err)
	}
	return job, nil
}

// CompleteRequest contains completion parameters.
type CompleteRequest struct {
	JobID  string `json:"job_id"`
	Result any    `json:"result,omitempty"`
}

// Complete marks a job as completed.
func (s *QueueService) Complete(ctx context.Context, req CompleteRequest) error {
	if req.JobID == "" {
		return wrapError("queue", "Complete", ErrInvalidInput)
	}
	return wrapError("queue", "Complete", s.q.Complete(ctx, req.JobID, req.Result))
}

// FailRequest contains failure parameters.
type FailRequest struct {
	JobID string `json:"job_id"`
	Error string `json:"error"`
}

// Fail marks a job as failed.
func (s *QueueService) Fail(ctx context.Context, req FailRequest) error {
	if req.JobID == "" {
		return wrapError("queue", "Fail", ErrInvalidInput)
	}
	return wrapError("queue", "Fail", s.q.Fail(ctx, req.JobID, wrapError("queue", "Fail", ErrInternal)))
}

// RetryRequest contains retry parameters.
type RetryRequest struct {
	JobID string `json:"job_id"`
}

// Retry queues a failed job for retry.
func (s *QueueService) Retry(ctx context.Context, req RetryRequest) error {
	if req.JobID == "" {
		return wrapError("queue", "Retry", ErrInvalidInput)
	}
	return wrapError("queue", "Retry", s.q.Retry(ctx, req.JobID))
}

// GetRequest contains get parameters.
type GetRequest struct {
	JobID string `json:"job_id"`
}

// Get retrieves a job by ID.
func (s *QueueService) Get(ctx context.Context, req GetRequest) (*queue.Job, error) {
	if req.JobID == "" {
		return nil, wrapError("queue", "Get", ErrInvalidInput)
	}

	job, err := s.q.Get(ctx, req.JobID)
	if err != nil {
		return nil, wrapError("queue", "Get", err)
	}
	if job == nil {
		return nil, wrapError("queue", "Get", ErrNotFound)
	}
	return job, nil
}

// ListRequest contains list parameters.
type ListRequest struct {
	State string `json:"state,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// ListByState returns jobs in a given state.
func (s *QueueService) ListByState(ctx context.Context, req ListRequest) ([]*queue.Job, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	state := queue.JobState(req.State)
	if state == "" {
		state = queue.StatePending
	}

	jobs, err := s.q.ListByState(ctx, state, limit)
	if err != nil {
		return nil, wrapError("queue", "ListByState", err)
	}
	return jobs, nil
}

// Stats returns queue statistics.
func (s *QueueService) Stats(ctx context.Context) (*queue.QueueStats, error) {
	stats, err := s.q.Stats(ctx)
	if err != nil {
		return nil, wrapError("queue", "Stats", err)
	}
	return stats, nil
}
