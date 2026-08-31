package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestIsTimeWindowedBudget verifies the classification of which budget limits
// are eligible for auto-resume (time-windowed) vs. not (cumulative).
func TestIsTimeWindowedBudget(t *testing.T) {
	windowed := []llm.BudgetLimit{
		llm.BudgetLimitHourlyTokens,
		llm.BudgetLimitDailyTokens,
		llm.BudgetLimitHourlyCost,
		llm.BudgetLimitDailyCost,
	}
	for _, r := range windowed {
		if !isTimeWindowedBudget(r) {
			t.Errorf("expected %v to be time-windowed", r)
		}
	}

	cumulative := []llm.BudgetLimit{
		llm.BudgetLimitPerTask,
		llm.BudgetLimitPerSession,
		llm.BudgetLimitPerTaskCost,
		llm.BudgetLimitPerSessionCost,
	}
	for _, r := range cumulative {
		if isTimeWindowedBudget(r) {
			t.Errorf("expected %v to NOT be time-windowed", r)
		}
	}
}

// TestBudgetResumeWatcher_ParkAndDrain verifies that a parked turn is held
// while the budget is exceeded, then resumed once the budget clears.
func TestBudgetResumeWatcher_ParkAndDrain(t *testing.T) {
	// Tiny hourly limit so the first usage record exceeds it.
	budget := llm.NewBudget(llm.BudgetConfig{
		HourlyLimit:    1,
		Aggressiveness: 1.0, // effective limit = 1 * (0.5 + 0.5*1.0) = 1
	}, nil)

	// Push usage over the limit.
	budget.RecordUsage(llm.TokenUsage{TotalTokens: 100})

	if !budget.CheckBudget().Exceeded {
		t.Fatal("expected budget to be exceeded after recording usage")
	}

	var (
		mu      sync.Mutex
		resumed []ParkedTurn
	)
	resumeCh := make(chan ParkedTurn, 1)

	w := NewBudgetResumeWatcher(budget, nil, func(ctx context.Context, turn ParkedTurn) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
		resumeCh <- turn
	})
	// Use a short poll interval for the test.
	w.pollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	turn := ParkedTurn{
		SessionID:      "session-test",
		ConversationID: "conv-test",
		Message:        "hello",
	}
	if !w.Park(turn) {
		t.Fatal("expected Park to succeed")
	}
	if w.Pending() != 1 {
		t.Fatalf("expected 1 pending turn, got %d", w.Pending())
	}

	// While budget is exceeded, the turn must NOT resume. Give the watcher a
	// few poll cycles to (incorrectly) drain if the logic were broken.
	select {
	case <-resumeCh:
		t.Fatal("turn resumed while budget still exceeded")
	case <-time.After(100 * time.Millisecond):
		// good — still parked
	}

	// Now clear the budget by creating a fresh tracker with no usage and
	// swapping it in. (The watcher polls w.budget, so we reset usage by
	// replacing the window — simplest is a new budget the watcher reads.)
	// Since the watcher holds a pointer, simulate window expiry by resetting
	// the daily counter path is not possible; instead build a fresh budget
	// that reports clear and point the watcher at it.
	clearBudget := llm.NewBudget(llm.BudgetConfig{HourlyLimit: 100000}, nil)
	w.mu.Lock()
	w.budget = clearBudget
	w.mu.Unlock()

	// The watcher should now drain and resume the parked turn.
	select {
	case got := <-resumeCh:
		if got.SessionID != "session-test" {
			t.Errorf("resumed wrong turn: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not resume after budget cleared")
	}

	if w.Pending() != 0 {
		t.Errorf("expected 0 pending after drain, got %d", w.Pending())
	}
}

// TestBudgetResumeWatcher_NilBudget verifies that a nil budget disables
// auto-resume (Park returns false, no panic).
func TestBudgetResumeWatcher_NilBudget(t *testing.T) {
	w := NewBudgetResumeWatcher(nil, nil, func(ctx context.Context, turn ParkedTurn) {})
	if w.Park(ParkedTurn{SessionID: "x"}) {
		t.Error("expected Park to return false with nil budget")
	}
	// Start/Stop must be safe no-ops.
	w.Start(context.Background())
	w.Stop()
}
