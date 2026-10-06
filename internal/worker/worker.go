package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/pkg/id"
)

// JobProcessor defines the interface for processing jobs.
type JobProcessor interface {
	// Process executes a job and returns the result.
	Process(ctx context.Context, job *queue.Job) (any, error)
}

// Worker represents a single worker that processes jobs.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Worker struct {
	ID           string
	Capabilities []string
	AgentID      string
	State        State
	CurrentJob   *queue.Job
	LastActive   time.Time
	StartTime    time.Time
	JobsComplete int
	JobsFailed   int

	queue     queue.Queue
	processor JobProcessor
	logger    *slog.Logger
	// wakeCh is the queue-driven wake signal (see Config.WakeCh). Stored
	// as a nil-able channel: a nil channel in a select blocks forever,
	// which is exactly the legacy poll-only behavior.
	wakeCh <-chan struct{}
	// providerPolicyCfg is the tree-03 failure-policy config (same
	// values the LLM clients use; set once at wiring via
	// SetProviderPolicy). Nil = package defaults.
	providerPolicyCfg *llm.FailurePolicyConfig

	mu     sync.RWMutex
	cancel context.CancelFunc
	done   chan struct{}
	wg     *sync.WaitGroup // optional: pool WaitGroup for tracking actual goroutine lifecycle
	// claimToken is the attempt token of the job currently in flight
	// (audit H4), taken from the claim. It is what the completion presents,
	// so the queue can tell this worker's completion apart from a superseded
	// attempt's. Guarded by mu like CurrentJob.
	claimToken string
}

// Config holds worker configuration.
type Config struct {
	ID           string
	Capabilities []string
	AgentID      string
	Queue        queue.Queue
	Processor    JobProcessor
	Logger       *slog.Logger
	// WakeCh, when non-nil, is signaled by the queue (WakeNotifier) when a
	// job is enqueued: the run loop claims immediately instead of waiting
	// out its poll timer. Nil = poll-only legacy behavior (a nil channel
	// in a select blocks forever, so the wake case is simply never ready —
	// no special-casing needed).
	WakeCh <-chan struct{}
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
		AgentID:      cfg.AgentID,
		State:        StateStopped,
		StartTime:    time.Now(),
		queue:        cfg.Queue,
		processor:    cfg.Processor,
		logger:       cfg.Logger,
		wakeCh:       cfg.WakeCh,
		done:         make(chan struct{}),
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
	// Recreate the done channel so a restarted worker doesn't
	// double-close the original channel.
	w.done = make(chan struct{})
	ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()

	w.logger.Info("Worker started", "id", w.ID, "capabilities", w.Capabilities)

	if w.wg != nil {
		w.wg.Add(1)
	}
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
		AgentID:      w.AgentID,
		State:        w.State,
		Capabilities: w.Capabilities,
		StartTime:    w.StartTime,
		LastActive:   w.LastActive,
		JobsComplete: w.JobsComplete,
		JobsFailed:   w.JobsFailed,
		CurrentJobID: w.getCurrentJobID(),
	}
}

func (w *Worker) run(ctx context.Context) {
	defer func() {
		w.mu.Lock()
		w.setState(StateStopped)
		w.mu.Unlock()
		close(w.done)
		if w.wg != nil {
			w.wg.Done()
		}
	}()

	pollInterval := 1 * time.Second
	backoff := pollInterval
	idleBackoff := 1 * time.Second
	maxIdleBackoff := 15 * time.Second

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
			idleBackoff = 1 * time.Second
		} else {
			// No jobs available - exponential backoff for idle polling
			idleBackoff = min(idleBackoff*2, maxIdleBackoff)
		}

		// Wait before next poll
		waitTime := backoff
		if !processed {
			waitTime = idleBackoff
		}

		// Wait before next poll — interrupted early by a wake signal
		// (queue signaled an Enqueue) or context cancellation. A nil
		// wakeCh (legacy poll-only config) blocks forever in its select
		// case, leaving the timer as the only wake source.
		wakeTimer := time.NewTimer(waitTime)
		select {
		case <-ctx.Done():
			wakeTimer.Stop()
			return
		case <-w.wakeCh:
			wakeTimer.Stop()
		case <-wakeTimer.C:
		}
	}
}

func (w *Worker) tryProcessJob(ctx context.Context) (bool, error) {
	// Check if the worker is available to claim work.
	// Don't transition to claiming yet -- only do so after a job is found.
	w.mu.Lock()
	if !w.State.CanClaim() {
		w.mu.Unlock()
		return false, nil
	}
	// Reset complete/error to idle if needed so we're in a valid state
	// for the claiming transition later.
	if w.State == StateComplete || w.State == StateError {
		w.setState(StateIdle)
	}
	w.mu.Unlock()

	// Try to claim a job -- this is the actual work check
	job, err := w.queue.Claim(ctx, w.ID, w.Capabilities, w.AgentID)
	if err != nil {
		if errors.Is(err, queue.ErrNoJobAvailable) {
			return false, nil // Stay idle, no transition needed
		}
		w.mu.Lock()
		w.setStateWithError(StateError, "", err)
		w.mu.Unlock()
		return false, err
	}

	if job == nil {
		return false, nil // Stay idle, no transition needed
	}

	// Only now transition to claiming -- a job was actually found
	w.mu.Lock()
	w.setState(StateClaiming)
	w.CurrentJob = job
	w.claimToken = job.ClaimToken
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

	// Mark as processing. ErrJobStateLocked means this attempt lost the race
	// to a terminal state (the job was completed or dead-lettered by someone
	// else, audit L8) — the work is pointless, so skip execution entirely
	// rather than running a job nobody will accept a result for. Any other
	// error is non-fatal and logged, exactly as before: the claim is already
	// ours and Complete's own guard is the authority on the write.
	markErr := w.queue.MarkProcessing(ctx, job.ID)
	if markErr != nil && errors.Is(markErr, queue.ErrJobStateLocked) {
		w.mu.Lock()
		w.CurrentJob = nil
		w.claimToken = ""
		w.LastActive = time.Now()
		w.setStateWithError(StateError, job.ID, markErr)
		w.mu.Unlock()
		w.logger.Warn("Job reached a terminal state before processing began; dropping attempt",
			"job", job.ID, "worker_id", w.ID)
		return true, nil
	}
	if markErr != nil {
		w.logger.Error("Failed to mark job as processing", "job", job.ID, "error", markErr)
	}

	// Start heartbeat for cluster queue if supported.
	// This extends the claim timeout so long-running jobs are not
	// reclaimed by the cluster stale-claim sweeper.
	heartbeatDone := make(chan struct{})
	if hb, ok := w.queue.(queue.Heartbeater); ok {
		hb.Heartbeat(job.ID) // initial heartbeat
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatDone:
					return
				case <-ticker.C:
					hb.Heartbeat(job.ID)
				}
			}
		}()
	}

	// Execute the job
	result, processErr := w.processor.Process(ctx, job)

	close(heartbeatDone)

	w.mu.Lock()
	w.CurrentJob = nil
	claimToken := w.claimToken
	w.claimToken = ""
	w.LastActive = time.Now()

	if processErr != nil {
		// Tree 03 leaf 03 (D9): a PROVIDER WAIT is not a job failure.
		// Requeue the job at the wait's scheduled time WITHOUT
		// incrementing retry_count and release the worker slot (the
		// normal return below does that). Give-up (schedule unknown or
		// beyond the BackoffPlan horizon) and genuine failures keep the
		// existing Fail/retry path byte-identically below.
		//
		// AUDIT FIX (auditor E, mutex leak): the requeue return path
		// used to `return true, nil` while still holding w.mu and with
		// the worker left in StateProcessing — the next run-loop poll,
		// Stop(), and every GetState/GetStats/GetCurrentJob reader
		// deadlocked, and (post-unlock) the FSM could never leave
		// Processing, permanently starving the worker. Mirror the
		// success path's Processing → Complete transition (valid per
		// ValidTransitions; the next tryProcessJob resets Complete →
		// Idle) and release the lock BEFORE returning.
		if requeued := w.requeueOnProviderWait(ctx, job, processErr); requeued {
			w.setStateWithJob(StateComplete, job.ID)
			w.mu.Unlock()
			return true, nil
		}

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

	// Success — the ACTUAL queue outcome decides the counters and the state.
	//
	// Ordering matters (audit H4): JobsComplete++ used to run BEFORE Complete,
	// so a rejected completion (ErrJobNotClaimable — a superseded attempt) both
	// counted a job the queue never accepted AND returned an error that skipped
	// the run loop's `else if processed` branch, leaving the error backoff
	// un-reset and logging a successful job as a processing failure.
	//
	// So: release w.mu, ask the queue, then record what the queue actually did.
	// The claim token (captured with the CurrentJob clear above) rides along so
	// the queue can tell this attempt apart from a superseded one — and so a
	// retry of the SAME request is idempotent success rather than a 409.
	//
	// w.mu is dropped across the queue call: it is I/O (SQLite + a bus
	// publish), and holding the worker lock across it is exactly what the
	// mutex-scope rule forbids — every concurrent GetState/GetStats/
	// GetCurrentJob reader would stall for the duration of the write.
	w.mu.Unlock()

	completeErr := w.completeJob(ctx, job.ID, result, claimToken)

	w.mu.Lock()
	w.setStateWithJob(StateComplete, job.ID)
	w.mu.Unlock()

	if completeErr != nil {
		// The queue refused the completion: this attempt is superseded (the job
		// was requeued, reclaimed, or re-executed). The work genuinely ran, so
		// it is not a JobsFailed, and it is not a JobsComplete either — the
		// queue never recorded it. Returning nil keeps the run loop's
		// `else if processed` branch (backoff reset) on the honest path that the
		// processor actually took.
		w.logger.Warn("Job completion refused by the queue; attempt superseded",
			"worker_id", w.ID, "job_id", job.ID, "error", completeErr)
		return true, nil
	}

	w.mu.Lock()
	w.JobsComplete++
	w.mu.Unlock()

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
	w.State = state
}

func (w *Worker) setStateWithJob(state State, jobID string) {
	if !IsValidTransition(w.State, state) {
		w.logger.Warn("Invalid state transition", "worker", w.ID, "from", w.State, "to", state, "job", jobID)
		return
	}
	w.State = state
}

func (w *Worker) setStateWithError(state State, jobID string, err error) {
	if !IsValidTransition(w.State, state) {
		w.logger.Warn("Invalid state transition", "worker", w.ID, "from", w.State, "to", state, "job", jobID)
		return
	}
	w.State = state
}

func (w *Worker) getCurrentJobID() string {
	if w.CurrentJob != nil {
		return w.CurrentJob.ID
	}
	return ""
}

// WorkerStats holds worker statistics.
//
//nolint:revive // stutter with package name is intentional for API clarity
type WorkerStats struct {
	ID           string
	AgentID      string
	State        State
	Capabilities []string
	StartTime    time.Time
	LastActive   time.Time
	JobsComplete int
	JobsFailed   int
	CurrentJobID string
}

func generateWorkerID() string {
	return id.Generate("worker-")
}

// SetProviderPolicy injects the failure-policy config (tree 03 leaf 03
// wiring): the daemon passes the SAME cfg.LLM.FailurePolicy mapping the
// LLM clients receive, so job requeue schedules and client backoff
// schedules agree. Nil is ignored.
func (w *Worker) SetProviderPolicy(cfg *llm.FailurePolicyConfig) {
	if w == nil || cfg == nil {
		return
	}
	w.mu.Lock()
	w.providerPolicyCfg = cfg
	w.mu.Unlock()
}

// requeueOnProviderWait classifies processErr as a provider wait
// (llm.QuotaResetError / llm.ErrAllModelsQuotaBlocked /
// llm.ThrottleBackoffError — tree 03 leaf 03, D9) and, when the queue
// supports queue.Requeueable and the wait is schedulable within the
// failure-policy horizon, requeues the job at that time WITHOUT
// consuming a retry. Returns true when the job was requeued; the worker
// slot is released by the caller's normal return and the next eligible
// Claim wins the job (its next_retry_at gate keeps it parked until the
// schedule). Returns false on give-up / non-provider errors / queues
// without requeue support, so the existing Fail → retry/dead-letter
// path runs unchanged.
//
// policy is the SAME failure-policy config the LLM clients use (daemon
// wiring passes cfg.LLM.FailurePolicy; nil-safe defaults mirror
// llm.Client.policyCfg).
func (w *Worker) requeueOnProviderWait(ctx context.Context, job *queue.Job, processErr error) bool {
	if w == nil || processErr == nil || job == nil {
		return false
	}
	var class llm.FailureClass
	_, throttleShaped := llm.AsThrottleBackoffError(processErr)
	switch {
	case llm.IsQuotaResetError(processErr) || errors.Is(processErr, llm.ErrAllModelsQuotaBlocked):
		class = llm.FailureQuota
	case throttleShaped:
		class = llm.FailureThrottle
	default:
		return false // genuine failure — legacy path
	}
	// llm.IsNonRetryable covers QuotaResetError; a provider wait is
	// still not a failure, so the requeue decision precedes that check
	// at the call site. Failures the classifier does not recognize fall
	// through to the legacy path above.

	now := time.Now().UTC()
	var resumeAt time.Time
	var giveUp bool
	if quotaErr, ok := llm.AsQuotaResetError(processErr); ok {
		resumeAt = quotaErr.ResetAt
		if resumeAt.IsZero() && quotaErr.RetryAfter > 0 {
			resumeAt = now.Add(quotaErr.RetryAfter)
		}
		if resumeAt.IsZero() || !resumeAt.After(now) {
			return false
		}
		plan := llm.DefaultBackoffPlan(llm.FailureQuota, now, w.providerPolicy())
		if resumeAt.After(plan.GiveUpAt) {
			return false
		}
	} else if throttleErr, ok := llm.AsThrottleBackoffError(processErr); ok {
		if throttleErr.RetryAt.IsZero() {
			return false
		}
		resumeAt = throttleErr.RetryAt
		plan := llm.DefaultBackoffPlan(llm.FailureThrottle, now, w.providerPolicy())
		if resumeAt.After(plan.GiveUpAt) {
			return false
		}
	} else {
		// ErrAllModelsQuotaBlocked: no attached schedule — the quota
		// plan's first step.
		plan := llm.DefaultBackoffPlan(llm.FailureQuota, now, w.providerPolicy())
		if plan.ShouldGiveUp(now) {
			return false
		}
		resumeAt = plan.NextAttempt(now, 0, time.Time{})
		giveUp = resumeAt.IsZero()
	}
	if giveUp || resumeAt.IsZero() {
		return false
	}

	rq, ok := w.queue.(queue.Requeueable)
	if !ok {
		w.logger.Debug("provider wait on job but queue lacks requeue support — legacy failure path",
			"job", job.ID, "class", class)
		return false
	}
	if err := rq.Requeue(ctx, job.ID, resumeAt); err != nil {
		w.logger.Error("Failed to requeue job on provider wait", "job", job.ID, "error", err)
		return false
	}
	w.logger.Info("job requeued on provider wait (retry count unchanged)",
		"worker_id", w.ID,
		"job_id", job.ID,
		"class", class,
		"resume_at", resumeAt.Format(time.RFC3339),
	)
	return true
}

// completeJob completes a job on behalf of THIS execution attempt (audit H4).
//
// Queues that understand claim tokens get CompleteAttempt, so the store can
// distinguish an idempotent retry of this attempt's completion (success,
// nothing republished) from a completion whose attempt has been superseded
// (ErrJobNotClaimable). Queues without token support keep the token-less
// Complete — the pre-H4 contract — so the optional-interface probe never
// changes behaviour for an implementation that did not opt in.
//
// Returns the queue's error verbatim; the caller decides what it means for the
// counters (tryProcessJob treats a refusal as a superseded attempt, not a
// processing failure).
func (w *Worker) completeJob(ctx context.Context, jobID string, result any, claimToken string) error {
	if claimToken != "" {
		if ac, ok := w.queue.(queue.AttemptCompleter); ok {
			return ac.CompleteAttempt(ctx, jobID, result, claimToken)
		}
	}
	return w.queue.Complete(ctx, jobID, result)
}

// providerPolicy resolves the injected failure-policy config or the
// nil-safe defaults (mirrors llm.Client.policyCfg).
func (w *Worker) providerPolicy() llm.FailurePolicyConfig {
	if w != nil && w.providerPolicyCfg != nil {
		return *w.providerPolicyCfg
	}
	return llm.FailurePolicyConfig{
		Horizon:      24 * time.Hour,
		BaseThrottle: 30 * time.Second,
		PollFloor:    time.Hour,
	}
}
