package employee

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// budget_checker.go — C26 (bughunt): constitution DailyBudgetCents /
// MaxInvocationsPerDay were dead letter on the goal-round path. The
// PreExecChecker's budget step ran against noopBudgetChecker (SetBudgetChecker
// had zero production callers), and no counter survived a restart re-arm, so a
// restarted daemon forgot the day's spend and re-drove autonomous rounds to
// the caps.
//
// botStoreBudgetChecker is the store-backed BudgetChecker: it reads the
// per-employee bot_states row the CostTracker already persists on every
// execution (TodayRuns / TodayCostCents / TotalTokensUsed, with date-rollover
// reset), so the counters survive restarts by construction. Goal-round
// triggers that bypass the bot runner are counted through RecordGoalRound,
// which mirrors the increment into the same persisted row. The noop default
// stays in force whenever limits are zero/unset (the checker always reports;
// enforcement step 5 gates on the constitution limits).
type botStoreBudgetChecker struct {
	store *bot.Store
	mu    sync.Mutex
	// roundCounts counts goal-round triggers this process recorded per
	// employee; the persisted mirror in RecordGoalRound is authoritative,
	// this is the fallback memory when the state row is absent.
	roundCounts map[string]int
	logger      *slog.Logger
}

// newBotStoreBudgetChecker builds the checker over the live bot store. A nil
// store yields nil (unwired — the noop default stays).
func newBotStoreBudgetChecker(store *bot.Store, logger *slog.Logger) *botStoreBudgetChecker {
	if store == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &botStoreBudgetChecker{
		store:       store,
		roundCounts: make(map[string]int),
		logger:      logger,
	}
}

// SpentToday implements BudgetChecker. Read errors report the in-process
// round count only (fail open, matching the disabled posture) — a missing
// state row is a not-yet-hired employee, not an enforcement failure.
func (b *botStoreBudgetChecker) SpentToday(employeeID string) (tokens int, cents int, invocations int) {
	if b == nil || b.store == nil || employeeID == "" {
		return 0, 0, 0
	}
	state, err := b.store.GetState(context.Background(), employeeID)
	if err != nil || state == nil {
		return 0, 0, b.inProcessRounds(employeeID)
	}
	today := time.Now().Format("2006-01-02")
	if state.TodayDate != today {
		// Persisted row is from an earlier day: fresh-day zero plus
		// this process's own recorded rounds (not yet mirrored).
		return 0, 0, b.inProcessRounds(employeeID)
	}
	// RecordGoalRound mirrors every in-process round into TodayRuns, so
	// the persisted counter is authoritative on the current day.
	return state.TotalTokensUsed, state.TodayCostCents, state.TodayRuns
}

// inProcessRounds returns this process's goal-round count for the employee.
func (b *botStoreBudgetChecker) inProcessRounds(employeeID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.roundCounts[employeeID]
}

// RecordGoalRound counts one goal-round trigger against the persisted state
// so restart re-arms respect the invocation cap. Best-effort: state-row
// errors log and keep the in-process count only. Safe for concurrent use.
func (b *botStoreBudgetChecker) RecordGoalRound(employeeID string) {
	if b == nil || employeeID == "" {
		return
	}
	b.mu.Lock()
	b.roundCounts[employeeID]++
	b.mu.Unlock()

	state, err := b.store.GetState(context.Background(), employeeID)
	if err != nil || state == nil {
		// No state row (employee never ran a bot execution): the
		// in-process count is the only memory. Debug, not warn —
		// talk-only employees legitimately have no state row.
		b.logger.Debug("budget checker: no bot state row for round count",
			"employee_id", employeeID)
		return
	}
	today := time.Now().Format("2006-01-02")
	if state.TodayDate != today {
		state.TodayRuns = 0
		state.TodayCostCents = 0
		state.TodayDate = today
	}
	state.TodayRuns++
	if err := b.store.UpdateState(context.Background(), *state); err != nil {
		b.logger.Warn("budget checker: round count persist failed",
			"employee_id", employeeID, "error", err)
	}
}

// NewBotStoreBudgetChecker is the exported constructor the daemon wiring
// uses; nil store returns nil so wiring stays a clean no-op.
func NewBotStoreBudgetChecker(store *bot.Store, logger *slog.Logger) *botStoreBudgetChecker {
	return newBotStoreBudgetChecker(store, logger)
}

// WithBudgetChecker is a PreExecOption attaching the budget checker at
// RegisterPreExecCheckers time (C26 wiring seam). Nil is skipped by the
// standard options loop.
func WithBudgetChecker(bc BudgetChecker) PreExecOption {
	return func(p *PreExecChecker) {
		p.SetBudgetChecker(bc)
	}
}

// RecordGoalRound counts one goal-round invocation against the persisted
// budget counters (manager-level passthrough for the round driver's
// deferred-turn path; nil-checker safe).
func (m *Manager) RecordGoalRound(employeeID string) {
	if m == nil || m.roundBudget == nil {
		return
	}
	m.roundBudget.RecordGoalRound(employeeID)
}

// SetRoundBudgetChecker stores the checker the goal-round path records
// against. Nil is ignored (typed-nil guard).
func (m *Manager) SetRoundBudgetChecker(bc *botStoreBudgetChecker) {
	if m == nil || bc == nil {
		return
	}
	m.mu.Lock()
	m.roundBudget = bc
	m.mu.Unlock()
}

// BotStore exposes the manager's bot store for daemon wiring seams (C26:
// the budget checker construction needs the persisted counter store). Nil
// when no store was attached.
func (m *Manager) BotStore() *bot.Store {
	if m == nil {
		return nil
	}
	return m.botStore
}
