package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
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
	// If agentID is non-empty, only jobs targeted to that agent (or unassigned
	// jobs) are claimable. If agentID is empty, any pending job is claimable.
	Claim(ctx context.Context, workerID string, caps []string, agentID string) (*Job, error)

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

// Heartbeater is an optional interface that queues can implement to support
// extending claim timeouts for long-running jobs. The worker calls Heartbeat
// periodically while processing to prevent the cluster reclaim mechanism
// from re-claiming in-flight work.
type Heartbeater interface {
	Heartbeat(jobID string)
}

// PersistentQueue implements Queue with SQLite persistence and bus notifications.
type PersistentQueue struct {
	store           *Store
	bus             *bus.MessageBus
	logger          *slog.Logger
	isTaskCancelled IsTaskCancelledFunc
	// hasCancelFilter is true when SetTaskCancelledCallback has installed a
	// real (non-default) callback. When false, Claim can take the fast
	// atomic path via store.ClaimNextForAgent without listing pending jobs.
	hasCancelFilter bool

	mu     sync.RWMutex
	closed bool

	// scanBoundWarnAt is the next time the Claim scan-bound Warn may be
	// emitted (audit L1). Claim runs per worker per poll, so an unbounded
	// Warn fires once per worker per poll — exactly while an operator is
	// diagnosing the starvation it reports. Rate-limited, not latched: the
	// signal must stay visible for as long as the condition persists.
	scanBoundWarnAt time.Time

	// Wake plumbing: Enqueue non-blockingly signals registered waiter
	// channels so workers claim immediately instead of polling. The send
	// loop snapshots the waiter set under wakeMu but sends OUTSIDE the
	// lock (mutexio: no channel ops under mutex).
	wakeMu      sync.Mutex
	wakeWaiters map[chan<- struct{}]struct{}
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
		hasCancelFilter: false,
		wakeWaiters:     make(map[chan<- struct{}]struct{}),
	}

	logger.Info("Persistent queue initialized", "path", dbPath)
	return q, nil
}

// SetTaskCancelledCallback sets the callback for checking if a task is cancelled.
func (q *PersistentQueue) SetTaskCancelledCallback(fn IsTaskCancelledFunc) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if fn == nil {
		q.isTaskCancelled = func(taskID string) (bool, string) { return false, "" }
		q.hasCancelFilter = false
		return
	}
	q.isTaskCancelled = fn
	q.hasCancelFilter = true
}

// Store returns the underlying store for cluster-aware operations.
func (q *PersistentQueue) Store() *Store {
	return q.store
}

// DB returns the underlying database connection for recovery operations.
func (q *PersistentQueue) DB() *sql.DB {
	return q.store.DB()
}

// signalClaimable fans a wake signal out to every registered waiter
// (non-blocking). ONE method, called from EVERY transition that leaves a job
// claimable (audit M2) — not just Enqueue. Previously only Enqueue woke
// waiters, so a job returned to 'pending' by Retry/Requeue/
// RecoverFromDeadLetter sat unnoticed until the worker's poll timer fired,
// adding up to maxIdleBackoff (15s) of latency with no log line anywhere.
//
// Sends are dropped when a waiter channel is full: that worker is already
// awake (or mid-job and will claim on its next loop), so dropping is
// strictly better than blocking the writer. Sends happen OUTSIDE wakeMu
// (mutexio: no channel ops under a mutex).
//
// Safe to call while holding q.mu: the send is non-blocking and the snapshot
// lock (wakeMu) is separate, so it cannot extend the critical section by
// anything the scheduler does not already account for.
func (q *PersistentQueue) signalClaimable() {
	q.wakeMu.Lock()
	waiters := make([]chan<- struct{}, 0, len(q.wakeWaiters))
	for ch := range q.wakeWaiters {
		waiters = append(waiters, ch)
	}
	q.wakeMu.Unlock()
	for _, ch := range waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// signalClaimableAt schedules a wake fan-out for the moment a job parked until
// notBefore BECOMES claimable (audit M2).
//
// Why this exists rather than a bare signalClaimable on the retry path: Retry
// and Requeue park a job at a FUTURE next_retry_at (exponential backoff, or a
// provider-wait resume time). Waking waiters at the moment of the transition is
// useless for them — every woken worker re-polls, finds the claim gate closed,
// and goes back to sleep, so the job is STILL discovered only on a poll timer
// (up to maxIdleBackoff = 15s of dead time after the gate opens). The wake has
// to land on the far side of the gate, which is what this does.
//
// The immediate signalClaimable at transition time is still emitted as well:
// a worker that happens to be mid-idle may already be about to poll, and the
// extra claim attempt costs nothing when the gate is closed.
//
// A pending timer outliving Close is harmless: Close empties the waiter set,
// so a late fan-out signals nobody. Timer handles are deliberately not tracked
// or cancelled — an unbounded registry would be its own leak, and the failure
// mode is a no-op wake.
func (q *PersistentQueue) signalClaimableAt(notBefore time.Time) {
	delay := time.Until(notBefore)
	if delay <= 0 {
		// Already claimable; the caller's immediate signal covers it.
		return
	}
	time.AfterFunc(delay, func() {
		q.signalClaimable()
		q.logger.Debug("Woke waiters for a claim gate that opened",
			"not_before", notBefore.UTC().Format(time.RFC3339))
	})
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

	// A new job is claimable — wake registered waiters.
	q.signalClaimable()

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
//
// Fast path: when no real cancellation filter is installed (the common
// case), Claim delegates to store.ClaimNextForAgent which performs the
// SELECT + UPDATE atomically inside a single transaction. This eliminates
// the list-then-claim race where two workers could both see the same
// pending job.
//
// Slow path: when SetTaskCancelledCallback has installed a real callback,
// we fall back to the list-then-skip-then-claim flow so cancelled-task
// jobs can be bypassed before claiming.
func (q *PersistentQueue) Claim(ctx context.Context, workerID string, caps []string, agentID string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	// Fast path: no cancellation filter installed.
	if !q.hasCancelFilter {
		claimedJob, err := q.store.ClaimNextForAgent(workerID, caps, agentID)
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

	// Slow path: page through pending jobs (keyset pagination) and skip
	// cancelled-task ones. A single fixed window (ListByState(..., 50))
	// starves claimable jobs parked behind 50+ head-of-line pending jobs
	// (quota-deferred jobs carry a future next_retry_at), so we keep
	// fetching the next page until a claimable job is found, a partial page
	// ends the table, or the hard scan bound is hit.
	cancelFn := q.isTaskCancelled

	const (
		pageSize    = 50
		maxScanRows = 500
	)

	now := time.Now().UTC()
	var targetJob *Job
	scanned := 0

	var (
		afterInteractive bool
		afterPriority    int
		afterCreatedAt   time.Time
		afterID          string
	)

	for targetJob == nil {
		page, err := q.store.ListByStateAfter(StatePending, pageSize, afterInteractive, afterPriority, afterCreatedAt, afterID)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}

		for _, job := range page {
			scanned++
			// Skip jobs scheduled for the future (due_at not yet reached)
			if job.DueAt != nil && !job.DueAt.IsZero() && job.DueAt.After(now) {
				continue
			}
			// Skip jobs with retry backoff not yet elapsed
			if job.NextRetryAt != nil && !job.NextRetryAt.IsZero() && job.NextRetryAt.After(now) {
				continue
			}
			// Skip jobs targeted to a different agent
			if agentID != "" && job.AgentID != "" && job.AgentID != agentID {
				continue
			}
			// Skip cancelled tasks
			if job.TaskID != "" {
				if cancelled, _ := cancelFn(job.TaskID); cancelled {
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

		if targetJob != nil {
			break
		}

		// Stop on a partial page (end of the pending table) or the hard
		// scan bound — Claim must terminate even when every pending row is
		// parked.
		if len(page) < pageSize || scanned >= maxScanRows {
			if scanned >= maxScanRows {
				// Warn, not Debug: hitting the bound means claimable work may
				// exist past the scan window (bounded starvation tradeoff) —
				// operators should see it. Rate-limited (audit L1) because
				// this runs once per worker per poll.
				q.warnScanBound(scanned, maxScanRows)
			}
			break
		}

		last := page[len(page)-1]
		afterInteractive = last.Interactive
		afterPriority = int(last.Priority)
		afterCreatedAt = last.CreatedAt
		afterID = last.ID
	}

	if targetJob == nil {
		return nil, ErrNoJobAvailable
	}

	// Claim the selected job atomically
	claimedJob, err := q.store.ClaimNextByID(targetJob.ID, workerID)
	if err != nil {
		// If the job was already claimed by another worker (race condition
		// in the slow path), translate to ErrNoJobAvailable so the worker
		// treats it as "try again" instead of an error.
		if errors.Is(err, ErrJobAlreadyClaimed) {
			return nil, ErrNoJobAvailable
		}
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
//
// LEGACY, TOKEN-LESS entry point: it completes "whatever attempt currently
// holds the job" (the pre-H4 contract), which the bus/RPC/HTTP surfaces depend
// on. Workers hold a claim token and use CompleteAttempt.
func (q *PersistentQueue) Complete(ctx context.Context, jobID string, result any) error {
	return q.CompleteAttempt(ctx, jobID, result, "")
}

// CompleteAttempt marks the job completed on behalf of the execution attempt
// identified by claimToken (audit H4) — the token the claim returned on
// Job.ClaimToken.
//
// Publication rule, which is the whole point of the disposition:
//
//   - applied    → publish queue.job.completed (a fresh completion).
//   - idempotent → return nil WITHOUT publishing. The identical request
//     arriving twice from the same attempt is a success the caller can act on;
//     republishing would double-count the job downstream (task finalization,
//     memory sync). This is what a retrying client gets instead of a 409.
//   - superseded (ErrJobNotClaimable) → return the error, publish nothing.
//     The job moved on (requeued, reclaimed, re-claimed), so the presented
//     token belongs to an attempt nobody is executing any more.
func (q *PersistentQueue) CompleteAttempt(ctx context.Context, jobID string, result any, claimToken string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	disposition, err := q.store.CompleteAttempt(jobID, result, claimToken)
	if err != nil {
		// Stale/duplicate completion (job requeued or already completed):
		// do NOT publish queue.job.completed — subscribers (the tactical
		// scheduler) must not process attempt-1 results as fresh ones. The
		// worker logs the error; that is the acceptable outcome.
		return err
	}
	if !disposition.CompletionIsFresh() {
		return nil
	}

	q.publishEvent("queue.job.completed", map[string]any{
		KeyJobID: jobID,
		"result": result,
		// The attempt that produced this completion (bughunt H2). Consumers
		// whose own attempt is still live can now ask the attempt-freshness
		// question with a REAL token instead of degrading to the state-only
		// predicate. Omitted when empty so the token-less legacy shape is
		// byte-identical on the wire.
		"claim_token": claimToken,
	})

	return nil
}

// CompletionIsFresh forwards the attempt-freshness predicate to the store.
//
// Without this forwarder the agent's stale-completion guard is DEAD in
// production (bughunt H2): `completionFreshChecker` is declared structurally in
// internal/agent, and `PersistentQueue` holds `store` as an unexported field
// WITHOUT embedding it, so `Store.CompletionIsFresh` was never promoted to the
// queue's method set. The guard's type assertion therefore failed for every real
// queue and OnJobCompleted silently fell back to its pre-H5 state-only branch —
// the exact behaviour the guard was added to replace.
//
// Declared on the concrete type (not on the Queue interface) so every existing
// implementation of Queue keeps compiling; the guard probes for it
// structurally, exactly as it probes for AttemptCompleter.
func (q *PersistentQueue) CompletionIsFresh(ctx context.Context, jobID, claimToken string) (fresh, ok bool) {
	_ = ctx // the store's predicate is a single-row read; no context plumbing yet
	return q.store.CompletionIsFresh(jobID, claimToken)
}

// Fail marks a job as failed with an error. It is the token-less legacy shape:
// no attempt identity is presented, so the state-only predicate applies and
// every pre-existing caller keeps its current behaviour.
func (q *PersistentQueue) Fail(ctx context.Context, jobID string, err error) error {
	return q.FailAttempt(ctx, jobID, err, "")
}

// FailAttempt marks the job failed on behalf of the execution attempt
// identified by claimToken (bughunt H1) — the failure-path twin of
// CompleteAttempt. A worker that holds an attempt MUST pass it: the store
// refuses a failure presented for a SUPERSEDED attempt, so a late failure from
// an abandoned execution cannot move the LIVE attempt's job to failed.
//
// Publication rule mirrors CompleteAttempt: a superseded attempt
// (ErrJobNotClaimable) publishes NO queue.job.failed event, so subscribers do
// not process a stale failure as current.
func (q *PersistentQueue) FailAttempt(ctx context.Context, jobID string, err error, claimToken string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	disposition, storeErr := q.store.FailAttempt(jobID, err.Error(), claimToken)
	if storeErr != nil {
		return storeErr
	}
	if !disposition.CompletionIsFresh() {
		return nil
	}

	q.publishEvent("queue.job.failed", map[string]any{
		KeyJobID:      jobID,
		"error":       err.Error(),
		"claim_token": claimToken,
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

	// The job is back to pending but parked at a future next_retry_at (the
	// retry backoff). Wake now for a worker that is about to poll anyway, and
	// wake AGAIN when the gate opens — the second one is the one that removes
	// the poll-timer discovery latency (audit M2).
	q.signalClaimable()
	q.signalClaimableAt(q.store.PendingGate(jobID))

	q.publishEvent("queue.job.retry", map[string]any{
		KeyJobID: jobID,
	})

	return nil
}

// Requeueable is the optional queue surface for provider-wait requeues
// (tree 03 leaf 03, D9): reset the job to pending for a claim at
// notBefore WITHOUT consuming a retry — a provider wait is not a job
// failure. The worker checks for this interface alongside Heartbeater.
type Requeueable interface {
	Requeue(ctx context.Context, jobID string, notBefore time.Time) error
}

// Requeue implements Requeueable over the store's Requeue.
func (q *PersistentQueue) Requeue(ctx context.Context, jobID string, notBefore time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if err := q.store.Requeue(jobID, notBefore); err != nil {
		return err
	}

	// Claimable again once notBefore arrives (the next_retry_at gate) — wake
	// now for a worker already polling, and again when the gate opens, which is
	// what removes the poll-timer discovery latency (audit M2).
	q.signalClaimable()
	q.signalClaimableAt(notBefore)

	q.publishEvent("queue.job.requeue", map[string]any{
		KeyJobID:     jobID,
		"not_before": notBefore.UTC().Format(time.RFC3339),
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

	// A recovered job is pending and immediately claimable — wake waiters
	// (audit M2: this transition had no wake signal at all).
	q.signalClaimable()

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

// ResetStaleClaimsAtStartup resets crash-orphaned job claims (claimed/processing
// rows updated strictly before claimsBefore) back to pending. Single-node
// startup recovery for jobs whose owning process died mid-flight; see
// Store.ResetStaleClaimsAtStartup for semantics.
func (q *PersistentQueue) ResetStaleClaimsAtStartup(ctx context.Context, claimsBefore time.Time) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return 0, fmt.Errorf("queue is closed")
	}

	reset, err := q.store.ResetStaleClaimsAtStartup(ctx, claimsBefore)
	if err != nil {
		return reset, err
	}
	// A live path (daemon boot), and every reset job is claimable again —
	// wake waiters so the pool starts on the work instead of after its first
	// idle poll (audit M2).
	if reset > 0 {
		q.signalClaimable()
	}
	return reset, nil
}

// Close closes the queue.
func (q *PersistentQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	// Clear wake waiters: closed queue never enqueues again, so channels
	// must not outlive it.
	q.wakeMu.Lock()
	q.wakeWaiters = make(map[chan<- struct{}]struct{})
	q.wakeMu.Unlock()
	return q.store.Close() //nolint:mutexio // one-time teardown guarded by closed flag
}

// scanBoundWarnInterval bounds how often the Claim scan-bound Warn may fire
// (audit L1). Long enough that a starvation episode produces a readable handful
// of lines instead of one per worker per poll; short enough that an operator
// watching a still-starved queue keeps seeing it move.
const scanBoundWarnInterval = 30 * time.Second

// warnScanBound emits the scan-bound Warn at most once per
// scanBoundWarnInterval, and logs every suppressed hit at Debug so the flood
// stays diagnosable in a debug-level capture without polluting Warn. Callers
// hold q.mu (the whole Claim does), which is also what serializes the throttle:
// no extra lock, and no I/O outside the existing critical section.
func (q *PersistentQueue) warnScanBound(scanned, bound int) {
	now := time.Now()
	if now.Before(q.scanBoundWarnAt) {
		q.logger.Debug("Claim scan hit row bound (suppressed)",
			"scanned", scanned, "bound", bound,
			"next_warn_after", q.scanBoundWarnAt)
		return
	}
	q.scanBoundWarnAt = now.Add(scanBoundWarnInterval)
	q.logger.Warn("Claim scan hit row bound with no claimable job",
		"scanned", scanned, "bound", bound,
		"warn_throttled_to", scanBoundWarnInterval)
}

// WakeNotifier is an optional interface a Queue may implement to let
// workers register a channel that Enqueue signals (non-blocking) on every
// new job — event-driven wake-up instead of fixed-interval polling.
// Deliberately separate from Queue so queue implementations without wake
// support stay drop-in (the pool type-asserts).
type WakeNotifier interface {
	// WakeWaiter registers ch for wake signals on Enqueue and returns an
	// unregister func that removes it (call on worker stop; idempotent).
	WakeWaiter(ch chan<- struct{}) (unregister func())
}

// WakeWaiter registers ch as a wake waiter signaled (non-blocking) by
// Enqueue. The returned unregister func removes ch from the waiter set;
// it is safe to call multiple times.
func (q *PersistentQueue) WakeWaiter(ch chan<- struct{}) (unregister func()) {
	q.wakeMu.Lock()
	q.wakeWaiters[ch] = struct{}{}
	q.wakeMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			q.wakeMu.Lock()
			delete(q.wakeWaiters, ch)
			q.wakeMu.Unlock()
		})
	}
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

// PersistentQueue is the reference implementation of both optional
// attempt-scoped surfaces: the worker probes for AttemptCompleter to present a
// claim token, and ClusterQueue forwards WakeNotifier to it.
var (
	_ AttemptCompleter = (*PersistentQueue)(nil)
	_ WakeNotifier     = (*PersistentQueue)(nil)
)

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
		AgentID      string   `json:"agent_id,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	return h.queue.Claim(ctx, params.WorkerID, params.Capabilities, params.AgentID)
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
		ID:        id.Generate("queue-resp-"),
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
