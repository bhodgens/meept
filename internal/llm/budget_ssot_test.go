package llm

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestBudgetZeroConfigIsUnlimited pins the disabled posture at the enforcement
// layer: the all-zero BudgetConfig that the daemon builds when
// llm.budget.enabled is false (see daemon.llmBudgetConfigFromConfig) enforces
// nothing and reports unlimited limits, even after heavy usage.
func TestBudgetZeroConfigIsUnlimited(t *testing.T) {
	b := NewBudget(BudgetConfig{}, slog.New(slog.DiscardHandler))

	if res := b.CheckBudget(); res.Exceeded {
		t.Fatalf("CheckBudget() exceeded before any usage: %+v", res)
	}

	// Record far more than any historical default cap would allow, plus a big
	// dollar cost: with the switch off there is no limit to hit.
	b.RecordUsage(TokenUsage{TotalTokens: 10_000_000})
	b.RecordCost(CostRecord{CostUSD: 999.99})

	if res := b.CheckBudget(); res.Exceeded {
		t.Errorf("CheckBudget() exceeded with zero config: %+v", res)
	}
	if res := b.CheckBudgetWithScope("task-x", "sess-y"); res.Exceeded {
		t.Errorf("CheckBudgetWithScope() exceeded with zero config: %+v", res)
	}

	st := b.GetStatus()
	if st.HourlyLimit != 0 {
		t.Errorf("HourlyLimit = %d, want 0 (unlimited)", st.HourlyLimit)
	}
	if st.DailyLimit != 0 {
		t.Errorf("DailyLimit = %d, want 0 (unlimited)", st.DailyLimit)
	}
	if st.RPMLimit != 0 {
		t.Errorf("RPMLimit = %d, want 0 (unlimited)", st.RPMLimit)
	}
	if st.HourlyCostLimit != 0 || st.DailyCostLimit != 0 {
		t.Errorf("cost limits = (%v, %v), want (0, 0) (unlimited)", st.HourlyCostLimit, st.DailyCostLimit)
	}
	if st.PerTaskBudget != 0 || st.PerSessionBudget != 0 {
		t.Errorf("scope budgets = (%d, %d), want (0, 0) (no cap)", st.PerTaskBudget, st.PerSessionBudget)
	}

	// No rate limiting either: the call returns immediately rather than
	// blocking on a window (a blocked call would need the context to expire).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := b.WaitForRateLimit(ctx); err != nil {
		t.Errorf("WaitForRateLimit() with zero config = %v, want nil (unlimited)", err)
	}
}

// TestBudgetEnabledHourlyLimitEnforced covers the switch-on path: with
// aggressiveness 1.0 the effective limit equals the configured hourly limit,
// and reaching it makes CheckBudget report the hourly-token reason.
func TestBudgetEnabledHourlyLimitEnforced(t *testing.T) {
	b := NewBudget(BudgetConfig{HourlyLimit: 1000, Aggressiveness: 1.0}, slog.New(slog.DiscardHandler))

	if res := b.CheckBudget(); res.Exceeded {
		t.Fatalf("CheckBudget() exceeded before any usage: %+v", res)
	}

	b.RecordUsage(TokenUsage{TotalTokens: 1000})

	res := b.CheckBudget()
	if !res.Exceeded {
		t.Fatal("CheckBudget() not exceeded after reaching the configured hourly limit")
	}
	if res.Reason != BudgetLimitHourlyTokens {
		t.Errorf("Reason = %q, want %q", res.Reason, BudgetLimitHourlyTokens)
	}
	if st := b.GetStatus(); st.HourlyLimit != 1000 {
		t.Errorf("HourlyLimit = %d, want 1000", st.HourlyLimit)
	}
}

// TestBudgetEnabledPerTaskLimitEnforced covers a scope-based cap, proving the
// configured per-task budget is applied when enabled.
func TestBudgetEnabledPerTaskLimitEnforced(t *testing.T) {
	b := NewBudget(BudgetConfig{PerTaskBudget: 500, Aggressiveness: 1.0}, slog.New(slog.DiscardHandler))

	if res := b.CheckBudgetWithScope("task-1", "sess-1"); res.Exceeded {
		t.Fatalf("CheckBudgetWithScope() exceeded before any usage: %+v", res)
	}

	b.RecordTaskUsage("task-1", 500)

	res := b.CheckBudgetWithScope("task-1", "sess-1")
	if !res.Exceeded {
		t.Fatal("CheckBudgetWithScope() not exceeded after reaching the per-task cap")
	}
	if res.Reason != BudgetLimitPerTask {
		t.Errorf("Reason = %q, want %q", res.Reason, BudgetLimitPerTask)
	}
}

// TestBudgetEnabledRateLimitBlocks covers the RPM rail: with rate_limit_rpm 1 a
// second request within the minute window must block (not be rejected) until
// the context expires.
func TestBudgetEnabledRateLimitBlocks(t *testing.T) {
	b := NewBudget(BudgetConfig{RateLimitRPM: 1, Aggressiveness: 1.0}, slog.New(slog.DiscardHandler))

	if err := b.WaitForRateLimit(context.Background()); err != nil {
		t.Fatalf("first WaitForRateLimit() = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := b.WaitForRateLimit(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("second WaitForRateLimit() returned immediately with a full RPM window; want a block")
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("returned after %v; expected to block on the RPM window (~150ms)", elapsed)
	}
}
