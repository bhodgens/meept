package llm

import (
	"testing"
)

// TestBudgetStatusDisabledLimitsShape pins the budget-level disabled-limits
// shape (matrix rows F1/F3, Contract 4; gate leaf
// docs/plans/budget-status-consistency/00T-gate-tests.md).
//
// BUG B (internal/llm/budget.go ~:731): WithinBudget is computed as
// `hourlyUsed < effHourly && b.dailyUsed < effDaily && withinCostBudget`.
// With token limits disabled (0), effectiveLimit(0) == 0, so
// `hourlyUsed < 0` is false and the authoritative Status reports
// WithinBudget == false on an unlimited config — even though its cost twin
// (:707-712) correctly treats limit 0 as unconstrained.
//
// Expected AFTER the leaf-01 fix (Contract 4 / Delta log 2026-10-27):
// all limits 0 with recorded usage must report WithinBudget == true AND
// WithinCostBudget == true. The WithinBudget assertion is RED today.
func TestBudgetStatusDisabledLimitsShape(t *testing.T) {
	// The repo-shipped default posture: every limit 0 = disabled.
	b := NewBudget(BudgetConfig{}, nil)

	b.RecordUsage(TokenUsage{TotalTokens: 500})

	status := b.GetStatus()

	if status.HourlyLimit != 0 {
		t.Errorf("HourlyLimit = %d, want 0 (disabled limit stays 0, no sentinel)", status.HourlyLimit)
	}
	if status.DailyLimit != 0 {
		t.Errorf("DailyLimit = %d, want 0 (disabled limit stays 0, no sentinel)", status.DailyLimit)
	}
	if status.HourlyRemaining != 0 {
		t.Errorf("HourlyRemaining = %d, want 0 (disabled serializes as 0; disambiguation rides HourlyLimit)", status.HourlyRemaining)
	}
	if status.HourlyUsed != 500 {
		t.Errorf("HourlyUsed = %d, want 500", status.HourlyUsed)
	}

	// GREEN today: the cost half (:707-712) already guards limit > 0.
	if !status.WithinCostBudget {
		t.Error("WithinCostBudget = false with all cost limits disabled, want true (limit 0 = unconstrained)")
	}

	// RED today: BUG B — `hourlyUsed < effHourly` is `500 < 0` = false.
	if !status.WithinBudget {
		t.Error("WithinBudget = false with all token limits disabled, want true (limit 0 = unconstrained) — budget.go:731 lacks the limit>0 guards its cost twin has")
	}
}

// TestBudgetStatusThreeStates pins the Contract-4 three-state table at the
// budget level (matrix rows F1/F3): disabled / under-limit / at-limit.
// Aggressiveness is 1.0 so effectiveLimit(base) == base (factor 0.5+0.5*a),
// matching the convention in budget_test.go.
func TestBudgetStatusThreeStates(t *testing.T) {
	tests := []struct {
		name string
		cfg  BudgetConfig
		// usage is recorded via RecordUsage before GetStatus.
		usage           int
		wantWithin      bool
		wantHourlyLimit int
		wantRemaining   int
	}{
		{
			name:            "disabled: all limits 0, usage recorded -> unconstrained (RED today: BUG B)",
			cfg:             BudgetConfig{Aggressiveness: 1.0},
			usage:           500,
			wantWithin:      true,
			wantHourlyLimit: 0,
			wantRemaining:   0,
		},
		{
			name:            "under limit: hourly 1000, usage 100 -> within, remaining 900",
			cfg:             BudgetConfig{HourlyLimit: 1000, DailyLimit: 5000, Aggressiveness: 1.0},
			usage:           100,
			wantWithin:      true,
			wantHourlyLimit: 1000,
			wantRemaining:   900,
		},
		{
			name:            "at/over limit: hourly 1000, usage 1100 -> not within, remaining 0",
			cfg:             BudgetConfig{HourlyLimit: 1000, DailyLimit: 5000, Aggressiveness: 1.0},
			usage:           1100,
			wantWithin:      false,
			wantHourlyLimit: 1000,
			wantRemaining:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBudget(tt.cfg, nil)
			if tt.usage != 0 {
				b.RecordUsage(TokenUsage{TotalTokens: tt.usage})
			}
			status := b.GetStatus()

			if status.HourlyLimit != tt.wantHourlyLimit {
				t.Errorf("HourlyLimit = %d, want %d", status.HourlyLimit, tt.wantHourlyLimit)
			}
			if status.HourlyRemaining != tt.wantRemaining {
				t.Errorf("HourlyRemaining = %d, want %d", status.HourlyRemaining, tt.wantRemaining)
			}
			if status.WithinBudget != tt.wantWithin {
				t.Errorf("WithinBudget = %v, want %v", status.WithinBudget, tt.wantWithin)
			}
			if !status.WithinCostBudget {
				t.Error("WithinCostBudget = false with no cost limits configured, want true")
			}
		})
	}
}
