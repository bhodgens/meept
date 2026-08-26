package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// isTimeWindowedBudget reports whether a budget limit clears on its own over
// time (hourly/daily token or cost windows). Per-task and per-session limits
// are cumulative and never clear on a timer, so they are not auto-resumed.
func isTimeWindowedBudget(reason llm.BudgetLimit) bool {
	switch reason {
	case llm.BudgetLimitHourlyTokens, llm.BudgetLimitDailyTokens,
		llm.BudgetLimitHourlyCost, llm.BudgetLimitDailyCost:
		return true
	default:
		return false
	}
}

// ParkedTurn captures a chat turn that was interrupted by budget exhaustion
// and is waiting for the budget window to clear before being retried.
type ParkedTurn struct {
	SessionID      string            // original session ID (for persistence + push)
	ConversationID string            // resolved conversation ID (for the agent loop)
	Message        string            // user message text
	Parts          []llm.ContentPart // multimodal parts, if any
	AgentID        string            // agent override, if any
	SourceClient   string            // originating client identifier
	ParkedAt       time.Time         // when the turn was parked
}

// BudgetResumeWatcher parks chat turns interrupted by budget exhaustion and
// retries them once the budget window clears. It polls the budget on a fixed
// interval; when the budget is no longer exceeded, it drains the parked queue
// (oldest first) by invoking the resume callback supplied by the ChatHandler.
//
// The watcher is best-effort: if the daemon shuts down while turns are parked,
// those turns are dropped (logged at warn). Turns are not persisted across
// restarts.
type BudgetResumeWatcher struct {
	budget       *llm.Budget
	logger       *slog.Logger
	pollInterval time.Duration

	mu         sync.Mutex
	parked     []ParkedTurn
	resumeFunc func(ctx context.Context, turn ParkedTurn)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewBudgetResumeWatcher creates a watcher. resumeFunc is invoked (in a
// background goroutine) for each parked turn once the budget clears; it is
// responsible for re-running the turn and pushing the result to the session.
// A nil budget or nil resumeFunc disables auto-resume (Park becomes a no-op).
func NewBudgetResumeWatcher(budget *llm.Budget, logger *slog.Logger, resumeFunc func(ctx context.Context, turn ParkedTurn)) *BudgetResumeWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	interval := 30 * time.Second
	return &BudgetResumeWatcher{
		budget:       budget,
		logger:       logger,
		pollInterval: interval,
		resumeFunc:   resumeFunc,
	}
}

// Start begins the background polling loop. Safe to call once; subsequent
// calls are no-ops.
func (w *BudgetResumeWatcher) Start(ctx context.Context) {
	if w == nil || w.budget == nil || w.resumeFunc == nil {
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
				w.drainIfClear(runCtx)
			}
		}
	}()
	w.logger.Info("budget resume watcher started", "poll_interval", w.pollInterval)
}

// Stop halts the polling loop and waits for it to exit. Parked turns that have
// not yet resumed are logged and dropped.
func (w *BudgetResumeWatcher) Stop() {
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
		w.logger.Warn("budget resume watcher stopped with parked turns dropped", "dropped", remaining)
	}
}

// Park enqueues a turn for later retry. Returns false (and does nothing) if
// auto-resume is not configured.
func (w *BudgetResumeWatcher) Park(turn ParkedTurn) bool {
	if w == nil || w.budget == nil || w.resumeFunc == nil {
		return false
	}
	if turn.ParkedAt.IsZero() {
		turn.ParkedAt = time.Now()
	}
	w.mu.Lock()
	w.parked = append(w.parked, turn)
	n := len(w.parked)
	w.mu.Unlock()
	w.logger.Info("parked turn pending budget clearance",
		"session_id", turn.SessionID,
		"queued", n,
	)
	return true
}

// Pending returns the number of currently parked turns.
func (w *BudgetResumeWatcher) Pending() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.parked)
}

// drainIfClear checks the budget and, if it is no longer exceeded, resumes all
// parked turns oldest-first. Each resume runs in its own goroutine so a slow
// turn does not block the polling loop.
func (w *BudgetResumeWatcher) drainIfClear(ctx context.Context) {
	// Read the budget pointer under mu: tests (and hot-swaps) may replace
	// w.budget concurrently with this read on the polling goroutine.
	w.mu.Lock()
	budget := w.budget
	resume := w.resumeFunc
	w.mu.Unlock()
	if budget == nil || budget.CheckBudget().Exceeded {
		return // still over budget; wait for next tick
	}

	w.mu.Lock()
	pending := w.parked
	w.parked = nil
	w.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	w.logger.Info("budget cleared — resuming parked turns", "count", len(pending))
	for _, turn := range pending {
		turn := turn
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			resume(ctx, turn)
		}()
	}
}
