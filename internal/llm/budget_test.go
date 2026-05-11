package llm

import (
	"context"
	"fmt"
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
