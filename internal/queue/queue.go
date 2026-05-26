package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Map key constants for queue operations.
const (
	KeyJobID  = "job_id"
	KeyStatus = "status"
)

// IsTaskCancelledFunc defines a function type for checking task cancellation.
type IsTaskCancelledFunc func(taskID string) (bool, string)

// Queue defines the interface for job queue operations.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job *Job) error

	// Claim claims the next available job for a worker.
	Claim(ctx context.Context, workerID string, caps []string) (*Job, error)

	// MarkProcessing marks a job as being processed.
	MarkProcessing(ctx context.Context, jobID string) error

	// Complete marks a job as completed with a result.
	Complete(ctx context.Context, jobID string, result any) error

	// Fail marks a job as failed with an error.
	Fail(ctx context.Context, jobID string, err error) error

	// Retry queues a failed job for retry.
	Retry(ctx context.Context, jobID string) error

	// Get retrieves a job by ID.
	Get(ctx context.Context, jobID string) (*Job, error)

	// ListByState returns jobs in a given state.
	ListByState(ctx context.Context, state JobState, limit int) ([]*Job, error)

	// ListByTaskID returns jobs associated with a task.
	ListByTaskID(ctx context.Context, taskID string) ([]*Job, error)

	// Stats returns queue statistics.
	Stats(ctx context.Context) (*QueueStats, error)

	// RecoverFromDeadLetter recovers a dead-lettered job back to the active queue.
	RecoverFromDeadLetter(ctx context.Context, jobID string) (*Job, error)

	// ListDeadLetter lists dead-lettered jobs.
	ListDeadLetter(ctx context.Context, limit int) ([]*Job, error)

	// DeadLetterStats returns dead-letter queue statistics.
	DeadLetterStats(ctx context.Context) (int, error)

	// Close closes the queue.
	Close() error
}

// PersistentQueue implements Queue with SQLite persistence and bus notifications.
type PersistentQueue struct {
	store           *Store
	bus             *bus.MessageBus
	logger          *slog.Logger
	isTaskCancelled IsTaskCancelledFunc

	mu     sync.RWMutex
	closed bool
}

// NewPersistentQueue creates a new persistent queue.
func NewPersistentQueue(dbPath string, msgBus *bus.MessageBus, logger *slog.Logger) (*PersistentQueue, error) {
	if logger == nil {
		logger = slog.Default()
	}

	store, err := NewStore(dbPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	q := &PersistentQueue{
		store:           store,
		bus:             msgBus,
		logger:          logger,
		isTaskCancelled: func(taskID string) (bool, string) { return false, "" }, // Default: no tasks cancelled
	}

	logger.Info("Persistent queue initialized", "path", dbPath)
	return q, nil
}

// SetTaskCancelledCallback sets the callback for checking if a task is cancelled.
func (q *PersistentQueue) SetTaskCancelledCallback(fn IsTaskCancelledFunc) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.isTaskCancelled = fn
}

// DB returns the underlying database connection for recovery operations.
func (q *PersistentQueue) DB() *sql.DB {
	return q.store.DB()
}

// Enqueue adds a job to the queue.
func (q *PersistentQueue) Enqueue(ctx context.Context, job *Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if err := q.store.Insert(job); err != nil {
		return err
	}

	// Publish event
	q.publishEvent("queue.enqueue", map[string]any{
		KeyJobID:   job.ID,
		"type":     job.Type,
		"priority": job.Priority.String(),
		"task_id":  job.TaskID,
	})

	return nil
}

// Claim claims the next available job for a worker.
// Skips jobs belonging to cancelled tasks.
func (q *PersistentQueue) Claim(ctx context.Context, workerID string, caps []string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	// List pending jobs and find first non-cancelled one
	pendingJobs, err := q.store.ListByState(StatePending, 50)
	if err != nil {
		return nil, err
	}

	// Find first claimable, non-cancelled job
	var targetJob *Job
	for _, job := range pendingJobs {
		// Skip cancelled tasks
		if job.TaskID != "" {
			if cancelled, _ := q.isTaskCancelled(job.TaskID); cancelled {
				q.logger.Debug("Skipping job from cancelled task", KeyJobID, job.ID, "task_id", job.TaskID)
				continue
			}
		}
		// Check if worker can claim this job
		if job.CanBeClaimedBy(caps) {
			targetJob = job
			break
		}
	}

	if targetJob == nil {
		return nil, ErrNoJobAvailable
	}

	// Claim the selected job atomically
	claimedJob, err := q.store.ClaimNextByID(targetJob.ID, workerID)
	if err != nil {
		return nil, err
	}

	if claimedJob != nil {
		q.publishEvent("queue.job.claimed", map[string]any{
			KeyJobID:    claimedJob.ID,
			"worker_id": workerID,
		})
	}

	return claimedJob, nil
}

// MarkProcessing marks a job as being processed.
func (q *PersistentQueue) MarkProcessing(ctx context.Context, jobID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	return q.store.UpdateState(jobID, StateProcessing)
}

// Complete marks a job as completed with a result.
func (q *PersistentQueue) Complete(ctx context.Context, jobID string, result any) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if err := q.store.Complete(jobID, result); err != nil {
		return err
	}

	q.publishEvent("queue.job.completed", map[string]any{
		KeyJobID: jobID,
		"result": result,
	})

	return nil
}

// Fail marks a job as failed with an error.
func (q *PersistentQueue) Fail(ctx context.Context, jobID string, err error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if storeErr := q.store.Fail(jobID, err.Error()); storeErr != nil {
		return storeErr
	}

	q.publishEvent("queue.job.failed", map[string]any{
		KeyJobID: jobID,
		"error":  err.Error(),
	})

	return nil
}

// Retry queues a failed job for retry.
func (q *PersistentQueue) Retry(ctx context.Context, jobID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if err := q.store.Retry(jobID); err != nil {
		return err
	}

	q.publishEvent("queue.job.retry", map[string]any{
		KeyJobID: jobID,
	})

	return nil
}

// Get retrieves a job by ID.
func (q *PersistentQueue) Get(ctx context.Context, jobID string) (*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.GetByID(jobID)
}

// ListByState returns jobs in a given state.
func (q *PersistentQueue) ListByState(ctx context.Context, state JobState, limit int) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.ListByState(state, limit)
}

// ListByTaskID returns jobs associated with a task.
func (q *PersistentQueue) ListByTaskID(ctx context.Context, taskID string) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.ListByTaskID(taskID)
}

// Stats returns queue statistics.
func (q *PersistentQueue) Stats(ctx context.Context) (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.GetStats()
}

// RecoverFromDeadLetter recovers a dead-lettered job back to the active queue.
func (q *PersistentQueue) RecoverFromDeadLetter(ctx context.Context, jobID string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	recovered, err := q.store.RecoverFromDeadLetter(jobID)
	if err != nil {
		return nil, err
	}

	q.publishEvent("queue.job.recovered", map[string]any{
		KeyJobID: jobID,
	})

	return recovered, nil
}

// ListDeadLetter lists dead-lettered jobs.
func (q *PersistentQueue) ListDeadLetter(ctx context.Context, limit int) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.ListDeadLetter(limit)
}

// DeadLetterStats returns dead-letter queue statistics.
func (q *PersistentQueue) DeadLetterStats(ctx context.Context) (int, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.DeadLetterStats()
}

// Close closes the queue.
func (q *PersistentQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	return q.store.Close()
}

func (q *PersistentQueue) publishEvent(topic string, data map[string]any) {
	if q.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "queue", data)
	if err != nil {
		q.logger.Error("Failed to create bus message", "error", err)
		return
	}

	q.bus.Publish(topic, msg)
}

// Ensure PersistentQueue implements Queue interface.
var _ Queue = (*PersistentQueue)(nil)

// Handler handles queue-related requests on the message bus.
type Handler struct {
	handler *bus.SubscriptionHandler
	queue   Queue
	bus     *bus.MessageBus
	logger  *slog.Logger
}

// NewHandler creates a new queue handler.
func NewHandler(queue Queue, msgBus *bus.MessageBus, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		handler: bus.NewSubscriptionHandler(msgBus, logger.With("component", "queue-handler")),
		queue:   queue,
		bus:     msgBus,
		logger:  logger,
	}

	// Subscribe to all queue topics
	topics := map[string]bus.MessageCallback{
		"queue.enqueue":     h.handleQueueEnqueue,
		"queue.claim":       h.handleQueueClaim,
		"queue.complete":    h.handleQueueComplete,
		"queue.fail":        h.handleQueueFail,
		"queue.retry":       h.handleQueueRetry,
		"queue.get":         h.handleQueueGet,
		"queue.list":        h.handleQueueList,
		"queue.stats":       h.handleQueueStats,
		"queue.recover":     h.handleQueueRecover,
		"queue.dead_letter": h.handleQueueDeadLetter,
		"queue.dead_stats":  h.handleQueueDeadStats,
	}

	for topic, callback := range topics {
		h.handler.Subscribe(topic, callback)
	}

	return h
}

// Start begins listening for queue requests.
func (h *Handler) Start(ctx context.Context) error {
	h.handler.Start(ctx)
	h.logger.Info("Queue handler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	h.handler.Stop()
	return nil
}

func (h *Handler) handleQueueEnqueue(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueClaim(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueComplete(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueFail(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueRetry(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueGet(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueList(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueStats(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueRecover(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueDeadLetter(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleQueueDeadStats(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleMessage(ctx context.Context, topic string, msg *models.BusMessage) {
	var response any
	var err error

	switch topic {
	case "queue.enqueue":
		response, err = h.handleEnqueue(ctx, msg)
	case "queue.claim":
		response, err = h.handleClaim(ctx, msg)
	case "queue.complete":
		response, err = h.handleComplete(ctx, msg)
	case "queue.fail":
		response, err = h.handleFail(ctx, msg)
	case "queue.retry":
		response, err = h.handleRetry(ctx, msg)
	case "queue.get":
		response, err = h.handleGet(ctx, msg)
	case "queue.list":
		response, err = h.handleList(ctx, msg)
	case "queue.stats":
		response, err = h.handleStats(ctx, msg)
	case "queue.recover":
		response, err = h.handleRecover(ctx, msg)
	case "queue.dead_letter":
		response, err = h.handleDeadLetter(ctx, msg)
	case "queue.dead_stats":
		response, err = h.handleDeadStats(ctx, msg)
	default:
		err = fmt.Errorf("unknown topic: %s", topic)
	}

	h.sendResponse(msg.ID, "queue.result", response, err)
}

func (h *Handler) handleEnqueue(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		Type         string   `json:"type"`
		Priority     int      `json:"priority"`
		TaskID       string   `json:"task_id,omitempty"`
		Prompt       string   `json:"prompt"`
		SessionID    string   `json:"session_id,omitempty"`
		RequiredCaps []string `json:"required_caps,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	jobType := JobTypeOneOff
	if params.Type == string(JobTypeProjectTask) {
		jobType = JobTypeProjectTask
	}

	payload := map[string]string{
		"prompt":     params.Prompt,
		"session_id": params.SessionID,
	}

	job, err := NewJob(jobType, payload)
	if err != nil {
		return nil, err
	}

	if params.Priority > 0 {
		job.WithPriority(Priority(params.Priority))
	}
	if params.TaskID != "" {
		job.WithTaskID(params.TaskID)
	}
	if len(params.RequiredCaps) > 0 {
		job.WithRequiredCaps(params.RequiredCaps)
	}

	if err := h.queue.Enqueue(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

func (h *Handler) handleClaim(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		WorkerID     string   `json:"worker_id"`
		Capabilities []string `json:"capabilities,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	return h.queue.Claim(ctx, params.WorkerID, params.Capabilities)
}

func (h *Handler) handleComplete(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		JobID  string `json:"job_id"`
		Result any    `json:"result,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.queue.Complete(ctx, params.JobID, params.Result); err != nil {
		return nil, err
	}

	return map[string]string{KeyStatus: "completed"}, nil
}

func (h *Handler) handleFail(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		JobID string `json:"job_id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.queue.Fail(ctx, params.JobID, fmt.Errorf("%s", params.Error)); err != nil {
		return nil, err
	}

	return map[string]string{KeyStatus: "failed"}, nil
}

func (h *Handler) handleRetry(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.queue.Retry(ctx, params.JobID); err != nil {
		return nil, err
	}

	return map[string]string{KeyStatus: "retried"}, nil
}

func (h *Handler) handleGet(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	return h.queue.Get(ctx, params.JobID)
}

func (h *Handler) handleList(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		State string `json:"state,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	var state JobState
	if params.State != "" {
		state = JobState(params.State)
	} else {
		state = StatePending
	}

	jobs, err := h.queue.ListByState(ctx, state, limit)
	if err != nil {
		return nil, err
	}

	return map[string]any{"jobs": jobs}, nil
}

func (h *Handler) handleStats(ctx context.Context, _ *models.BusMessage) (any, error) {
	stats, err := h.queue.Stats(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to string-keyed maps for JSON serialization
	byState := make(map[string]int)
	for state, count := range stats.ByState {
		byState[string(state)] = count
	}

	byPriority := make(map[string]int)
	for priority, count := range stats.ByPriority {
		byPriority[priority.String()] = count
	}

	return map[string]any{
		"by_state":    byState,
		"by_priority": byPriority,
		"dead_count":  stats.DeadCount,
	}, nil
}

func (h *Handler) handleRecover(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	job, err := h.queue.RecoverFromDeadLetter(ctx, params.JobID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"job": job, KeyStatus: "recovered"}, nil
}

func (h *Handler) handleDeadLetter(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		Limit int `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	jobs, err := h.queue.ListDeadLetter(ctx, limit)
	if err != nil {
		return nil, err
	}

	return map[string]any{"jobs": jobs}, nil
}

func (h *Handler) handleDeadStats(ctx context.Context, _ *models.BusMessage) (any, error) {
	count, err := h.queue.DeadLetterStats(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]any{"dead_count": count}, nil
}

func (h *Handler) sendResponse(replyTo, topic string, response any, err error) {
	var payload []byte

	if err != nil {
		payload, _ = json.Marshal(map[string]string{"error": err.Error()})
	} else {
		payload, _ = json.Marshal(response)
	}

	respMsg := &models.BusMessage{
		ID:        fmt.Sprintf("queue-resp-%d", time.Now().UnixNano()),
		Type:      models.MessageTypeResponse,
		Topic:     topic,
		Source:    "queue-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish(topic, respMsg)
}

// WaitForJob waits for a job to complete or timeout.
func WaitForJob(ctx context.Context, q Queue, jobID string, pollInterval time.Duration) (*Job, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			job, err := q.Get(ctx, jobID)
			if err != nil {
				return nil, err
			}
			if job == nil {
				return nil, fmt.Errorf("job not found: %s", jobID)
			}
			if job.IsComplete() {
				return job, nil
			}
		}
	}
}

// Ensure PersistentQueue implements io.Closer
var _ io.Closer = (*PersistentQueue)(nil)
