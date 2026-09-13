package daemon

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// TestLLMBudgetConfigFromConfigGatedBySwitch pins the single global switch at
// the one place budget values cross from meept.json5 into enforcement: when
// llm.budget.enabled is false, a config full of limits maps to the all-zero
// BudgetConfig (unlimited) and NOTHING is enforced.
func TestLLMBudgetConfigFromConfigGatedBySwitch(t *testing.T) {
	configured := config.BudgetConfig{
		Enabled:              false,
		HourlyTokenLimit:     100000,
		DailyTokenLimit:      1000000,
		DailyCostLimit:       10.0,
		HourlyCostLimit:      2.0,
		RateLimitRPM:         30,
		Aggressiveness:       0.5,
		PerTaskTokenLimit:    50000,
		PerSessionTokenLimit: 100000,
		PerTaskCostLimit:     5.0,
		PerSessionCostLimit:  10.0,
	}

	// Switch off: limits must NOT cross into the tracker.
	if got := llmBudgetConfigFromConfig(configured); got != (llm.BudgetConfig{}) {
		t.Errorf("disabled mapping = %+v, want zero value (unlimited)", got)
	}

	// Switch on: the configured limits apply verbatim.
	configured.Enabled = true
	want := llm.BudgetConfig{
		HourlyLimit:         100000,
		DailyLimit:          1000000,
		DailyCostLimit:      10.0,
		HourlyCostLimit:     2.0,
		RateLimitRPM:        30,
		Aggressiveness:      0.5,
		PerTaskBudget:       50000,
		PerSessionBudget:    100000,
		PerTaskCostLimit:    5.0,
		PerSessionCostLimit: 10.0,
	}
	if got := llmBudgetConfigFromConfig(configured); got != want {
		t.Errorf("enabled mapping = %+v, want %+v", got, want)
	}
}

// TestLLMBudgetConfigDisabledTrackerUnlimited verifies the end-to-end effect of
// the switch at the tracker boundary: a disabled config produces a tracker that
// reports unlimited limits and never blocks, even after heavy usage.
func TestLLMBudgetConfigDisabledTrackerUnlimited(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Budget.HourlyTokenLimit = 1000 // set, but the switch is false
	cfg.LLM.Budget.RateLimitRPM = 60

	tracker := llm.NewBudget(llmBudgetConfigFromConfig(cfg.LLM.Budget), slog.New(slog.DiscardHandler))
	tracker.RecordUsage(llm.TokenUsage{TotalTokens: 5_000_000})

	if res := tracker.CheckBudget(); res.Exceeded {
		t.Errorf("CheckBudget() exceeded with the switch off: %+v", res)
	}
	if st := tracker.GetStatus(); st.HourlyLimit != 0 || st.RPMLimit != 0 {
		t.Errorf("status limits = (%d, %d), want (0, 0) when the switch is off", st.HourlyLimit, st.RPMLimit)
	}
}

// TestAgentBudgetEnabledSingleSwitch pins the hierarchical-budget gate: it is
// governed by the SAME global switch (llm.budget.enabled) plus a positive
// total, with no enable flag of its own under agent.budget.
func TestAgentBudgetEnabledSingleSwitch(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		total   int
		want    bool
	}{
		{"default: off, no total", false, 0, false},
		{"limits configured but switch off (back-compat)", false, 200000, false},
		{"switch on but no total", true, 0, false},
		{"switch on and positive total", true, 200000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.LLM.Budget.Enabled = tc.enabled
			cfg.Agent.Budget.Total = tc.total
			if got := agentBudgetEnabled(cfg); got != tc.want {
				t.Errorf("agentBudgetEnabled(enabled=%v, total=%d) = %v, want %v", tc.enabled, tc.total, got, tc.want)
			}
		})
	}

	if agentBudgetEnabled(nil) {
		t.Error("agentBudgetEnabled(nil) = true, want false")
	}
}
