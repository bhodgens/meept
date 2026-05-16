package llm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBudgetCheckBudget(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:    1000,
		DailyLimit:     5000,
		RateLimitRPM:   0,
		Aggressiveness: 1.0, // Full budget
	}, nil)

	// Initially within budget
	if !b.CheckBudget() {
		t.Error("Should be within budget initially")
	}

	// Record usage
	b.RecordUsage(TokenUsage{TotalTokens: 500})
	if !b.CheckBudget() {
		t.Error("Should still be within budget after 500 tokens")
	}

	// Exceed hourly limit
	b.RecordUsage(TokenUsage{TotalTokens: 600})
	if b.CheckBudget() {
		t.Error("Should exceed hourly budget after 1100 tokens")
	}
}

func TestBudgetAggressiveness(t *testing.T) {
	// Conservative budget (aggressiveness = 0)
	b := NewBudget(BudgetConfig{
		HourlyLimit:    1000,
		DailyLimit:     5000,
		RateLimitRPM:   0,
		Aggressiveness: 0.0, // Conservative - only 50% of budget usable
	}, nil)

	// Effective limit is 500 (50% of 1000)
	b.RecordUsage(TokenUsage{TotalTokens: 400})
	if !b.CheckBudget() {
		t.Error("Should be within conservative budget after 400 tokens")
	}

	b.RecordUsage(TokenUsage{TotalTokens: 150})
	if b.CheckBudget() {
		t.Error("Should exceed conservative budget after 550 tokens (limit 500)")
	}
}

func TestBudgetGetStatus(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:    1000,
		DailyLimit:     5000,
		RateLimitRPM:   10,
		Aggressiveness: 1.0,
	}, nil)

	b.RecordUsage(TokenUsage{TotalTokens: 200})

	status := b.GetStatus()

	if status.HourlyUsed != 200 {
		t.Errorf("HourlyUsed = %d, want 200", status.HourlyUsed)
	}

	if status.DailyUsed != 200 {
		t.Errorf("DailyUsed = %d, want 200", status.DailyUsed)
	}

	if status.HourlyLimit != 1000 {
		t.Errorf("HourlyLimit = %d, want 1000", status.HourlyLimit)
	}

	if status.HourlyRemaining != 800 {
		t.Errorf("HourlyRemaining = %d, want 800", status.HourlyRemaining)
	}

	if !status.WithinBudget {
		t.Error("Should be within budget")
	}

	if status.RPMLimit != 10 {
		t.Errorf("RPMLimit = %d, want 10", status.RPMLimit)
	}

	if status.RPMCurrent != 1 {
		t.Errorf("RPMCurrent = %d, want 1", status.RPMCurrent)
	}
}

func TestBudgetNegativeTokens(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:    1000,
		DailyLimit:     5000,
		Aggressiveness: 1.0,
	}, nil)

	// Negative tokens should be ignored
	b.RecordUsage(TokenUsage{TotalTokens: -100})

	status := b.GetStatus()
	if status.HourlyUsed != 0 {
		t.Errorf("HourlyUsed = %d, want 0 (negative should be ignored)", status.HourlyUsed)
	}
}

func TestBudgetWaitForRateLimitUnlimited(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:  1000,
		DailyLimit:   5000,
		RateLimitRPM: 0, // Unlimited
	}, nil)

	ctx := context.Background()
	start := time.Now()
	err := b.WaitForRateLimit(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("WaitForRateLimit should not error: %v", err)
	}

	if elapsed > 10*time.Millisecond {
		t.Errorf("Should return immediately for unlimited RPM, took %v", elapsed)
	}
}

func TestBudgetWaitForRateLimitBelowLimit(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:  1000,
		DailyLimit:   5000,
		RateLimitRPM: 10,
	}, nil)

	// Record a few requests (below limit)
	for range 5 {
		b.RecordUsage(TokenUsage{TotalTokens: 10})
	}

	ctx := context.Background()
	start := time.Now()
	err := b.WaitForRateLimit(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("WaitForRateLimit should not error: %v", err)
	}

	if elapsed > 10*time.Millisecond {
		t.Errorf("Should return immediately when below RPM limit, took %v", elapsed)
	}
}

func TestBudgetExceededError(t *testing.T) {
	err := &BudgetExceededError{Message: "test error"}
	if err.Error() != "test error" {
		t.Errorf("Error() = %q, want %q", err.Error(), "test error")
	}
}

func TestIsNonRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "regular error",
			err:  fmt.Errorf("some error"),
			want: false,
		},
		{
			name: "BudgetExceededError directly",
			err:  &BudgetExceededError{Message: "budget exceeded"},
			want: true,
		},
		{
			name: "wrapped BudgetExceededError",
			err:  fmt.Errorf("failed to process: %w", &BudgetExceededError{Message: "budget exceeded"}),
			want: true,
		},
		{
			name: "double-wrapped BudgetExceededError",
			err:  fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &BudgetExceededError{Message: "budget exceeded"})),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNonRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsNonRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBudgetZeroLimitsAllowAll(t *testing.T) {
	// Issue 0034: Zero limits should allow all requests (unconfigured budget)
	b := NewBudget(BudgetConfig{
		HourlyLimit:  0,
		DailyLimit:   0,
		RateLimitRPM: 0,
	}, nil)

	// Should be allowed even without recording any usage
	if !b.CheckBudget() {
		t.Error("Zero limits should allow all requests (unconfigured budget)")
	}

	// Record a huge amount of usage and still allow
	b.RecordUsage(TokenUsage{TotalTokens: 999999999})
	if !b.CheckBudget() {
		t.Error("Zero limits should allow all requests even after massive usage")
	}
}

func TestBudgetPartialZeroLimits(t *testing.T) {
	// When only one limit is set, the other should still be enforced
	b := NewBudget(BudgetConfig{
		HourlyLimit:    1000,
		DailyLimit:     0, // no daily cap
		RateLimitRPM:   0,
		Aggressiveness: 1.0, // full budget: effectiveLimit(1000) = 1000
	}, nil)

	// Daily is unconfigured, but hourly should still be enforced
	b.RecordUsage(TokenUsage{TotalTokens: 500})
	if !b.CheckBudget() {
		t.Error("Should be within budget after 500 tokens")
	}

	b.RecordUsage(TokenUsage{TotalTokens: 600})
	if b.CheckBudget() {
		t.Error("Should exceed hourly budget after 1100 tokens (limit 1000)")
	}

	// Now test with only daily set
	b2 := NewBudget(BudgetConfig{
		HourlyLimit:    0, // no hourly cap
		DailyLimit:     1000,
		RateLimitRPM:   0,
		Aggressiveness: 1.0, // full budget: effectiveLimit(1000) = 1000
	}, nil)

	// Should allow because daily has a limit (hourly=0 doesn't force block anymore)
	b2.RecordUsage(TokenUsage{TotalTokens: 500})
	if !b2.CheckBudget() {
		t.Error("Should be within daily budget after 500 tokens")
	}

	b2.RecordUsage(TokenUsage{TotalTokens: 600})
	if b2.CheckBudget() {
		t.Error("Should exceed daily budget after 1100 tokens")
	}
}

func TestBudgetPerTaskExhaustion(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:    100000,
		DailyLimit:     1000000,
		PerTaskBudget:  5000, // small per-task cap
		Aggressiveness: 1.0,
	}, nil)

	b.RecordUsage(TokenUsage{TotalTokens: 3000})
	b.RecordTaskUsage("task1", 3000)

	b.RecordUsage(TokenUsage{TotalTokens: 2500})
	b.RecordTaskUsage("task1", 2500) // total 5500 > 5000

	if b.CheckBudgetWithScope("task1", "session1") {
		t.Error("Should block exhausted task")
	}

	// Another task should still be fine
	if !b.CheckBudgetWithScope("task2", "session1") {
		t.Error("Should allow new task even if task1 is exhausted")
	}
}

func TestBudgetPerSessionExhaustion(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:      100000,
		DailyLimit:       1000000,
		PerSessionBudget: 8000,
		Aggressiveness:   1.0,
	}, nil)

	// Fill up session
	b.RecordUsage(TokenUsage{TotalTokens: 5000})
	b.RecordSessionUsage("session1", 5000)

	b.RecordUsage(TokenUsage{TotalTokens: 4000})
	b.RecordSessionUsage("session1", 4000) // total 9000 > 8000

	if b.CheckBudgetWithScope("task1", "session1") {
		t.Error("Should block exhausted session")
	}

	// Another session should still work
	if !b.CheckBudgetWithScope("task1", "session2") {
		t.Error("Should allow new session even if session1 is exhausted")
	}
}

func TestBudgetConcurrentScopeAccess(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:      100000,
		DailyLimit:       1000000,
		PerTaskBudget:    50000,
		PerSessionBudget: 100000,
		Aggressiveness:   1.0,
	}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			taskID := fmt.Sprintf("task-%d", id%3)
			sessionID := fmt.Sprintf("session-%d", id%2)
			b.CheckBudgetWithScope(taskID, sessionID)
			b.RecordTaskUsage(taskID, 100)
			b.RecordSessionUsage(sessionID, 100)
		}(i)
	}

	resultCh := make(chan bool, 1)
	go func() {
		wg.Wait()
		resultCh <- true
	}()

	select {
	case <-resultCh:
		// All goroutines completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for concurrent goroutines")
	}
}

func TestBudgetScopeRecordsUsage(t *testing.T) {
	b := NewBudget(BudgetConfig{
		HourlyLimit:      100000,
		DailyLimit:       1000000,
		PerTaskBudget:    10000,
		PerSessionBudget: 100000,
		Aggressiveness:   1.0,
	}, nil)

	b.RecordUsageWithScope(TokenUsage{TotalTokens: 5000}, "task1", "session1")

	status := b.GetStatus()
	if status.PerTaskUsed != 5000 {
		t.Errorf("PerTaskUsed = %d, want 5000", status.PerTaskUsed)
	}
	if status.PerSessionUsed != 5000 {
		t.Errorf("PerSessionUsed = %d, want 5000", status.PerSessionUsed)
	}
}
