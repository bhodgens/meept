package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
)

// QueueService wraps the queue.Queue interface for cross-transport access.
type QueueService struct {
	q queue.Queue
	// sessions resolves the enqueue request's session_id for the interactive
	// stamp (D11). Nil = stamps false; wired from the registry's session
	// store when available.
	sessions sessionReader
}

// NewQueueService creates a queue service.
func NewQueueService(q queue.Queue) *QueueService {
	return &QueueService{q: q}
}

// sessionReader is the narrow session lookup the interactive stamp needs
// (D11). A local interface keeps the dependency light for tests.
type sessionReader interface {
	Get(id string) *session.Session
}

// StampInteractive sets job.Interactive from the originating session's live
// signal (tree 04 leaf 02, Contract 2; D11: recent user message within
// window OR foreground flag). The flag is evaluated ONCE at enqueue and
// never re-classified (audit R4 accepted semantics). A nil session, unknown
// session ID, or nil window source stamps false — never an error.
//
// LAYERING NOTE: this helper lives in services, not internal/queue, because
// the queue package is session-free by design; services already imports both.
func StampInteractive(job *queue.Job, origin *session.Session, now time.Time, window time.Duration) {
	if job == nil {
		return
	}
	job.WithInteractive(session.IsInteractive(origin, now, window))
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

// Enqueue adds a job to the queue. When the request carries a session_id
// the interactive flag is stamped from that session's live signal (D11);
// session-less requests stamp false by construction (audit R4).
func (s *QueueService) Enqueue(ctx context.Context, req EnqueueRequest) (*queue.Job, error) {
	return s.enqueueWithStamp(ctx, req, s.sessions, time.Now(), config.DefaultConfig().InteractiveWindow())
}

// enqueueWithStamp builds the job, stamps Interactive from the originating
// session's live signal evaluated at `now` (injected so tests control the
// clock, SHARED-CONVENTIONS §5), and enqueues.
func (s *QueueService) enqueueWithStamp(ctx context.Context, req EnqueueRequest, sessions sessionReader, now time.Time, window time.Duration) (*queue.Job, error) {
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

	StampInteractive(job, lookupSession(sessions, req.SessionID), now, window)

	if err := s.q.Enqueue(ctx, job); err != nil {
		return nil, wrapError("queue", "Enqueue", err)
	}

	return job, nil
}

// lookupSession resolves the origin session for the stamp, tolerating a nil
// reader or empty ID (both stamp false per R4).
func lookupSession(sessions sessionReader, sessionID string) *session.Session {
	if sessions == nil || sessionID == "" {
		return nil
	}
	return sessions.Get(sessionID)
}

// ClaimRequest contains claim parameters.
type ClaimRequest struct {
	WorkerID     string   `json:"worker_id"`
	Capabilities []string `json:"capabilities,omitempty"`
	AgentID      string   `json:"agent_id,omitempty"`
}

// Claim claims the next available job for a worker.
func (s *QueueService) Claim(ctx context.Context, req ClaimRequest) (*queue.Job, error) {
	if req.WorkerID == "" {
		return nil, wrapError("queue", "Claim", ErrInvalidInput)
	}

	job, err := s.q.Claim(ctx, req.WorkerID, req.Capabilities, req.AgentID)
	if err != nil {
		if errors.Is(err, queue.ErrNoJobAvailable) {
			return nil, wrapError("queue", "Claim", ErrNotFound)
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
	failErr := fmt.Errorf("%s", req.Error)
	if req.Error == "" {
		failErr = ErrInternal
	}
	return wrapError("queue", "Fail", s.q.Fail(ctx, req.JobID, failErr))
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
