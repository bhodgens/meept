package bot

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// CostTracker records execution costs and checks budget limits per-bot.
type CostTracker struct {
	store  *Store
	logger *slog.Logger
}

// NewCostTracker creates a new CostTracker backed by the given store.
func NewCostTracker(store *Store, logger *slog.Logger) *CostTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CostTracker{store: store, logger: logger}
}

// ExecutionCost represents the cost of a single bot execution in cents.
// Token-based costing requires an external price catalog; for now callers
// provide the cost directly when available, or pass 0 if unknown.
type ExecutionCost struct {
	TokensUsed int // total tokens consumed by the LLM call
	CostCents  int // monetary cost of this execution in cents (0 if unknown)
}

// RecordExecution loads the bot state, increments cost/token/run counters,
// resets daily counters on date rollover, and persists the updated state.
// If the bot definition cannot be loaded, the state update is skipped and
// the error is returned.
func (ct *CostTracker) RecordExecution(ctx context.Context, botID string, result *BotExecutionResult, cost ExecutionCost) error {
	state, err := ct.store.GetState(ctx, botID)
	if err != nil {
		return fmt.Errorf("load state for bot %q: %w", botID, err)
	}

	today := time.Now().Format("2006-01-02")

	// Reset daily counters on date change.
	if state.TodayDate != today {
		ct.logger.Info("rolling over daily counters", "bot_id", botID, "prev_date", state.TodayDate, "new_date", today)
		state.TodayRuns = 0
		state.TodayCostCents = 0
		state.TodayDate = today
	}

	// Persist last-run timestamp.
	now := time.Now().UTC()
	state.LastRunAt = &now

	state.TotalRuns++
	state.TotalTokensUsed += result.TokensUsed
	state.TotalCostCents += cost.CostCents
	state.TodayRuns++
	state.TodayCostCents += cost.CostCents

	if result.Error != "" {
		state.ConsecutiveFailures++
		state.LastError = result.Error
	} else {
		state.ConsecutiveFailures = 0
		state.LastError = ""
	}

	if err := ct.store.UpdateState(ctx, *state); err != nil {
		return fmt.Errorf("save state for bot %q: %w", botID, err)
	}

	ct.logger.Debug("recorded execution cost",
		"bot_id", botID,
		"tokens", result.TokensUsed,
		"cost_cents", cost.CostCents,
		"total_cost_cents", state.TotalCostCents,
		"today_cost_cents", state.TodayCostCents,
	)

	return nil
}

// IsBudgetExhausted returns true if the bot has exceeded its daily budget.
// If the bot definition cannot be loaded or no daily budget is configured,
// it returns false.
func (ct *CostTracker) IsBudgetExhausted(ctx context.Context, botID string) (bool, error) {
	def, err := ct.store.Get(ctx, botID)
	if err != nil {
		return false, fmt.Errorf("load definition for bot %q: %w", botID, err)
	}
	if def.Constraints.DailyBudgetCents <= 0 {
		return false, nil
	}

	state, err := ct.store.GetState(ctx, botID)
	if err != nil {
		return false, fmt.Errorf("load state for bot %q: %w", botID, err)
	}

	today := time.Now().Format("2006-01-02")
	if state.TodayDate != today {
		return false, nil
	}

	return state.TodayCostCents >= def.Constraints.DailyBudgetCents, nil
}
