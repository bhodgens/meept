package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
)

// JobProcessor defines the interface for processing jobs.
type JobProcessor interface {
	// Process executes a job and returns the result.
	Process(ctx context.Context, job *queue.Job) (any, error)
}

// Worker represents a single worker that processes jobs.
type Worker struct {
	ID           string
	Capabilities []string
	State        State
	CurrentJob   *queue.Job
	LastActive   time.Time
	StartTime    time.Time
	JobsComplete int
	JobsFailed   int

	queue     queue.Queue
	processor JobProcessor
	logger    *slog.Logger

	mu     sync.RWMutex
	cancel context.CancelFunc
	done   chan struct{}

	// State change notifications
	stateChanges chan StateTransition
}

// Config holds worker configuration.
type Config struct {
	ID           string
	Capabilities []string
	Queue        queue.Queue
	Processor    JobProcessor
	Logger       *slog.Logger
}

// NewWorker creates a new worker.
func NewWorker(cfg Config) (*Worker, error) {
	if cfg.ID == "" {
		cfg.ID = generateWorkerID()
	}
	if cfg.Queue == nil {
		return nil, fmt.Errorf("queue is required")
	}
	if cfg.Processor == nil {
		return nil, fmt.Errorf("processor is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Worker{
		ID:           cfg.ID,
		Capabilities: cfg.Capabilities,
		State:        StateStopped,
		StartTime:    time.Now(),
		queue:        cfg.Queue,
		processor:    cfg.Processor,
		logger:       cfg.Logger,
		done:         make(chan struct{}),
		stateChanges: make(chan StateTransition, 10),
	}, nil
}

// Start begins the worker's processing loop.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.State != StateStopped {
		w.mu.Unlock()
		return fmt.Errorf("worker already running")
	}
	w.StartTime = time.Now()
	w.setState(StateIdle)
	ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()

	w.logger.Info("Worker started", "id", w.ID, "capabilities", w.Capabilities)

	go w.run(ctx)
	return nil
}

// Stop gracefully stops the worker.
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if w.State == StateStopped || w.State == StateStopping {
		w.mu.Unlock()
		return nil
	}
	w.setState(StateStopping)
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()

	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetState returns the current worker state.
func (w *Worker) GetState() State {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.State
}

// GetCurrentJob returns the currently processing job, if any.
func (w *Worker) GetCurrentJob() *queue.Job {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.CurrentJob
}

// GetStats returns worker statistics.
func (w *Worker) GetStats() WorkerStats {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return WorkerStats{
		ID:           w.ID,
		State:        w.State,
		Capabilities: w.Capabilities,
		StartTime:    w.StartTime,
		LastActive:   w.LastActive,
		JobsComplete: w.JobsComplete,
		JobsFailed:   w.JobsFailed,
		CurrentJobID: w.getCurrentJobID(),
	}
}

// StateChanges returns a channel that receives state change notifications.
func (w *Worker) StateChanges() <-chan StateTransition {
	return w.stateChanges
}

func (w *Worker) run(ctx context.Context) {
	defer func() {
		w.mu.Lock()
		w.setState(StateStopped)
		w.mu.Unlock()
		close(w.done)
	}()

	pollInterval := 1 * time.Second
	backoff := pollInterval

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Try to claim and process a job
		processed, err := w.tryProcessJob(ctx)
		if err != nil {
			w.logger.Error("Error processing job", "worker", w.ID, "error", err)
			// Exponential backoff on errors
			backoff = min(backoff*2, 30*time.Second)
		} else if processed {
			// Reset backoff on successful processing
			backoff = pollInterval
		}

		// Wait before next poll
		waitTime := backoff
		if !processed {
			// Longer wait if no work was found
			waitTime = pollInterval
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(waitTime):
		}
	}
}

func (w *Worker) tryProcessJob(ctx context.Context) (bool, error) {
	// Transition to claiming state
	w.mu.Lock()
	if !w.State.CanClaim() {
		w.mu.Unlock()
		return false, nil
	}

	// Force transition to Idle first if in Complete or Error state.
	// This ensures we follow the valid state transition path:
	// Complete/Error -> Idle -> Claiming
	if w.State == StateComplete || w.State == StateError {
		w.setState(StateIdle)
	}

	w.setState(StateClaiming)
	w.mu.Unlock()

	// Try to claim a job
	job, err := w.queue.Claim(ctx, w.ID, w.Capabilities)
	if err != nil {
		w.mu.Lock()
		w.setStateWithError(StateError, "", err)
		w.mu.Unlock()
		return false, err
	}

	if job == nil {
		// No jobs available
		w.mu.Lock()
		w.setState(StateIdle)
		w.mu.Unlock()
		return false, nil
	}

	// Process the job
	w.mu.Lock()
	w.CurrentJob = job
	w.setStateWithJob(StateProcessing, job.ID)
	w.mu.Unlock()

	// Extract step/task context from job payload for logging
	var stepID, taskID, agentID string
	if job.Payload != nil {
		var payload struct {
			StepID string `json:"step_id"`
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err == nil {
			stepID = payload.StepID
			taskID = payload.TaskID
		}
	}
	if job.TaskID != "" {
		taskID = job.TaskID
	}
	if job.AgentID != "" {
		agentID = job.AgentID
	}

	jobStartTime := time.Now()
	w.logger.Info("ASSIGN job claimed",
		"worker_id", w.ID,
		"job_id", job.ID,
		"step_id", stepID,
		"task_id", taskID,
		"agent_id", agentID,
	)

	// Mark as processing
	if err := w.queue.MarkProcessing(ctx, job.ID); err != nil {
		w.logger.Error("Failed to mark job as processing", "job", job.ID, "error", err)
	}

	// Execute the job
	result, processErr := w.processor.Process(ctx, job)

	w.mu.Lock()
	w.CurrentJob = nil
	w.LastActive = time.Now()

	if processErr != nil {
		w.JobsFailed++
		w.setStateWithError(StateError, job.ID, processErr)
		w.mu.Unlock()

		// Mark job as failed
		if err := w.queue.Fail(ctx, job.ID, processErr); err != nil {
			w.logger.Error("Failed to mark job as failed", "job", job.ID, "error", err)
		}

		// Check if we can retry - but NOT for non-retryable errors
		// Non-retryable errors (like budget exhaustion) should go directly to dead letter
		if llm.IsNonRetryable(processErr) {
			w.logger.Info("Non-retryable error - skipping retry",
				"job", job.ID,
				"error", processErr,
			)
		} else if job.CanRetry() {
			if err := w.queue.Retry(ctx, job.ID); err != nil {
				w.logger.Error("Failed to queue job for retry", "job", job.ID, "error", err)
			}
		}

		return true, processErr
	}

	// Success
	w.JobsComplete++
	w.setStateWithJob(StateComplete, job.ID)
	w.mu.Unlock()

	// Mark job as completed
	if err := w.queue.Complete(ctx, job.ID, result); err != nil {
		w.logger.Error("Failed to mark job as completed", "job", job.ID, "error", err)
		return true, err
	}

	w.logger.Info("DONE job completed",
		"worker_id", w.ID,
		"job_id", job.ID,
		"step_id", stepID,
		"task_id", taskID,
		"agent_id", agentID,
		"duration_ms", time.Since(jobStartTime).Milliseconds(),
	)
	return true, nil
}

func (w *Worker) setState(state State) {
	if !IsValidTransition(w.State, state) {
		w.logger.Warn("Invalid state transition", "worker", w.ID, "from", w.State, "to", state)
		return
	}
	w.emitTransition(w.State, state, "", nil)
	w.State = state
}

func (w *Worker) setStateWithJob(state State, jobID string) {
	if !IsValidTransition(w.State, state) {
		w.logger.Warn("Invalid state transition", "worker", w.ID, "from", w.State, "to", state, "job", jobID)
		return
	}
	w.emitTransition(w.State, state, jobID, nil)
	w.State = state
}

func (w *Worker) setStateWithError(state State, jobID string, err error) {
	if !IsValidTransition(w.State, state) {
		w.logger.Warn("Invalid state transition", "worker", w.ID, "from", w.State, "to", state, "job", jobID)
		return
	}
	w.emitTransition(w.State, state, jobID, err)
	w.State = state
}

func (w *Worker) emitTransition(from, to State, jobID string, err error) {
	transition := StateTransition{
		WorkerID:  w.ID,
		FromState: from,
		ToState:   to,
		JobID:     jobID,
		Error:     err,
		Timestamp: time.Now(),
	}

	select {
	case w.stateChanges <- transition:
	default:
		// Channel full, drain oldest and retry
		select {
		case <-w.stateChanges:
		default:
		}
		select {
		case w.stateChanges <- transition:
		default:
		}
	}
}

func (w *Worker) getCurrentJobID() string {
	if w.CurrentJob != nil {
		return w.CurrentJob.ID
	}
	return ""
}

// WorkerStats holds worker statistics.
type WorkerStats struct {
	ID           string
	State        State
	Capabilities []string
	StartTime    time.Time
	LastActive   time.Time
	JobsComplete int
	JobsFailed   int
	CurrentJobID string
}

func generateWorkerID() string {
	return fmt.Sprintf("worker-%d", time.Now().UnixNano())
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
