package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// QuotaParkedTurn captures a chat turn interrupted by a provider quota wall
// and is waiting for the quota window to lift before being retried. Mirrors
// ParkedTurn (budget_resume.go) with quota-specific resume metadata.
type QuotaParkedTurn struct {
	SessionID      string            // original session ID (for persistence + push)
	ConversationID string            // resolved conversation ID (for the agent loop)
	Message        string            // user message text
	Parts          []llm.ContentPart // multimodal parts, if any
	AgentID        string            // agent override, if any
	SourceClient   string            // originating client identifier
	ProviderID     string            // provider that hit the quota
	CredentialKey  string            // credential fingerprint of the blocked pool
	UnblockAt      time.Time         // earliest time the quota window lifts
	ParkedAt       time.Time         // when the turn was parked
}

// DefaultQuotaResumePollInterval is the re-check cadence for parked turns.
// Mirrors config llm.quota_retry.defer_check_interval's 10m default; the
// field is exported on the watcher for tests and config wiring.
const DefaultQuotaResumePollInterval = 10 * time.Minute

// QuotaResumeWatcher parks chat turns interrupted by provider quota errors
// and retries them once the quota window lifts. It polls on a fixed
// interval; when the earliest unblock time for parked turns has passed, it
// drains the queue (oldest first) by invoking the resume callback supplied
// by the ChatHandler (quota-reset-resilience plan leaf 06: task deferral +
// auto-resume).
//
// MaxWait soft-stop: turns whose remaining wait exceeds MaxWait are NOT
// parked (the caller surfaces the error instead), matching contract 7's
// "schedule requeue at min(unblockAt, now+MaxWait)".
//
// The watcher is best-effort: turns parked when the daemon shuts down are
// dropped (logged at warn) and are not persisted across restarts. Quota
// blocks are in-memory, so a restart re-probes providers anyway.
type QuotaResumeWatcher struct {
	logger       *slog.Logger
	pollInterval time.Duration
	maxWait      time.Duration

	mu         sync.Mutex
	parked     []QuotaParkedTurn
	resumeFunc func(ctx context.Context, turn QuotaParkedTurn)

	cancel   context.CancelFunc
	wg       sync.WaitGroup // polling + resume goroutines (Stop barrier)
	resumeWG sync.WaitGroup // resume goroutines only (drainDue barrier)
}

// NewQuotaResumeWatcher creates a watcher. resumeFunc is invoked (in a
// background goroutine) for each parked turn once its unblock time passes;
// it is responsible for re-running the turn and pushing the result to the
// session. A nil resumeFunc disables auto-resume (Park becomes a no-op).
// maxWait <= 0 falls back to llm.DefaultQuotaMaxWait.
func NewQuotaResumeWatcher(logger *slog.Logger, resumeFunc func(ctx context.Context, turn QuotaParkedTurn), maxWait time.Duration) *QuotaResumeWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if maxWait <= 0 {
		maxWait = llm.DefaultQuotaMaxWait
	}
	return &QuotaResumeWatcher{
		logger:       logger,
		pollInterval: DefaultQuotaResumePollInterval,
		maxWait:      maxWait,
		resumeFunc:   resumeFunc,
	}
}

// SetPollInterval overrides the re-check cadence (config plumbing seam).
// Must be called before Start.
func (w *QuotaResumeWatcher) SetPollInterval(d time.Duration) {
	if w == nil || d <= 0 {
		return
	}
	w.pollInterval = d
}

// Start begins the background polling loop. Safe to call once; subsequent
// calls are no-ops.
func (w *QuotaResumeWatcher) Start(ctx context.Context) {
	if w == nil || w.resumeFunc == nil {
		return
	}
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return // already started
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.mu.Unlock()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				w.drainDue(runCtx)
			}
		}
	}()
	w.logger.Info("quota resume watcher started",
		"poll_interval", w.pollInterval,
		"max_wait", w.maxWait,
	)
}

// Stop halts the polling loop and waits for it to exit. Turns that have not
// yet resumed are logged and dropped.
func (w *QuotaResumeWatcher) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	remaining := len(w.parked)
	w.mu.Unlock()

	w.wg.Wait()
	if remaining > 0 {
		w.logger.Warn("quota resume watcher stopped with parked turns dropped", "dropped", remaining)
	}
}

// Park enqueues a turn for later retry once the quota window lifts.
// Returns false (and does nothing) when:
//   - auto-resume is not configured, or
//   - the wait exceeds MaxWait (soft-stop: caller surfaces the error), or
//   - the unblock time is unknown/zero (cannot schedule a resume).
func (w *QuotaResumeWatcher) Park(turn QuotaParkedTurn) bool {
	if w == nil || w.resumeFunc == nil {
		return false
	}
	if turn.UnblockAt.IsZero() {
		return false
	}
	wait := time.Until(turn.UnblockAt)
	if wait <= 0 {
		return false // already unblocked; caller should retry directly
	}
	if wait > w.maxWait {
		w.logger.Info("quota wait exceeds max_wait — not parking",
			"provider", turn.ProviderID,
			"wait", wait,
			"max_wait", w.maxWait,
		)
		return false
	}
	if turn.ParkedAt.IsZero() {
		turn.ParkedAt = time.Now()
	}
	w.mu.Lock()
	w.parked = append(w.parked, turn)
	n := len(w.parked)
	// Keep the queue ordered by unblock time so drainDue can pop from the
	// front without scanning (typical case: homogeneous unblock times).
	for i := n - 1; i > 0; i-- {
		if w.parked[i].UnblockAt.Before(w.parked[i-1].UnblockAt) {
			w.parked[i], w.parked[i-1] = w.parked[i-1], w.parked[i]
		} else {
			break
		}
	}
	w.mu.Unlock()
	w.logger.Info("parked turn pending quota reset",
		"session_id", turn.SessionID,
		"provider", turn.ProviderID,
		"unblock_at", turn.UnblockAt.Format(time.RFC3339),
		"queued", n,
	)
	return true
}

// Pending returns the number of currently parked turns.
func (w *QuotaResumeWatcher) Pending() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.parked)
}

// drainDue resumes every parked turn whose unblock time has passed,
// oldest-first, sequentially: a slow resume delays later resumes rather
// than reordering them. Called from the polling loop; its cadence bounds
// the delay. Turns whose window has not lifted stay parked.
func (w *QuotaResumeWatcher) drainDue(ctx context.Context) {
	w.mu.Lock()
	resume := w.resumeFunc
	now := time.Now()
	dueIdx := 0
	for dueIdx < len(w.parked) && !w.parked[dueIdx].UnblockAt.After(now) {
		dueIdx++
	}
	if dueIdx == 0 {
		w.mu.Unlock()
		return
	}
	due := w.parked[:dueIdx]
	remaining := make([]QuotaParkedTurn, len(w.parked)-dueIdx)
	copy(remaining, w.parked[dueIdx:])
	w.parked = remaining
	w.mu.Unlock()
	w.logger.Info("quota window lifted — resuming parked turns", "count", len(due))
	// Resume sequentially: the doc contract (and the ordering test) is
	// oldest-first, which parallel goroutine launches cannot guarantee.
	// Each resume runs synchronously; a slow resume delays later resumes
	// rather than reordering them. resumeWG is retained for Stop()'s
	// barrier and is already complete when this returns.
	for _, turn := range due {
		w.wg.Add(1)
		w.resumeWG.Add(1)
		func() {
			defer w.wg.Done()
			defer w.resumeWG.Done()
			resume(ctx, turn)
		}()
	}
	// Wait for the resumed turns so tests can observe the resume
	// synchronously. resumeWG is disjoint from the polling goroutine's wg
	// (waiting on wg here would deadlock with Stop).
	w.resumeWG.Wait()
}
