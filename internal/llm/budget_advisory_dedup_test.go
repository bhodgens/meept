package llm

import (
	"log/slog"
	"testing"
)

// One global budget breach episode must emit exactly one advisory signal,
// even when it is observed first by the unscoped async handler pre-check
// and again by the scoped client budget gate. The dedup key used to include
// task/session for ALL limits, producing two signals for one daily breach.
func TestSteerAdvisory_GlobalBreachDedupAcrossScopes(t *testing.T) {
	var emitted []BudgetAdvisorySignal
	b := NewBudget(BudgetConfig{
		DailyCostLimit: 1.0,
		Mode:           BudgetModeSteer,
	}, slog.New(slog.DiscardHandler))
	b.SetAdvisoryPublisher(func(sig BudgetAdvisorySignal) {
		emitted = append(emitted, sig)
	})

	breach := BudgetCheckResult{
		Exceeded: true,
		Reason:   BudgetLimitDailyCost,
		Used:     2.0,
		Limit:    1.0,
	}

	// Handler pre-check (empty scope), then client gate (real scope).
	_, sigs := b.applyModeGateLocked(breach, "", "")
	if len(sigs) != 1 {
		t.Fatalf("first check emitted %d signals, want 1", len(sigs))
	}
	b.emitPending(sigs)

	_, sigs = b.applyModeGateLocked(breach, "task-1", "sess-1")
	if len(sigs) != 0 {
		t.Fatalf("scoped check re-emitted %d signals for the same global breach, want 0", len(sigs))
	}
	b.emitPending(sigs)

	if len(emitted) != 1 {
		t.Fatalf("total advisory signals = %d, want 1 (got %+v)", len(emitted), emitted)
	}
}
