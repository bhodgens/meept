package daemon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// TestComponentsBudgetBlockKeys pins matrix row F5 (Contract 3): the
// components HTTP/bus status budget block must carry `within_budget` today
// AND gain `within_cost_budget` as a pass-through of
// Status.WithinCostBudget after leaf 01. Same pass-through rule as the RPC
// surface; no recompute.
//
// The within_cost_budget assertion is RED until leaf 01 (Contract 3):
// components.go's budget blocks (~:8664, ~:9473) embed budgetStatus.* fields
// but neither includes within_cost_budget.
//
// The block is unit-reachable via webHandlerAdapter.Status() with existing
// fixtures (NewStatusHandler + WithBudgetTracker) — no new infrastructure.
func TestComponentsBudgetBlockKeys(t *testing.T) {
	// Disabled posture (all limits 0): the authoritative Status reports
	// WithinCostBudget == true today (budget.go:707-712 guards limit > 0).
	budget := llm.NewBudget(llm.BudgetConfig{}, nil)
	budget.RecordUsage(llm.TokenUsage{TotalTokens: 500})

	msgBus := bus.New(nil, nil)
	sh := NewStatusHandler(msgBus, slog.Default(), WithBudgetTracker(budget))
	adapter := &webHandlerAdapter{statusHandler: sh}

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("webHandlerAdapter.Status: %v", err)
	}

	block, ok := status["budget"].(map[string]any)
	if !ok {
		t.Fatalf("status budget block missing or not a map: %#v", status["budget"])
	}

	// Existing keys must stay present (wire compat, F6 at this surface).
	for _, key := range []string{
		"hourly_used", "hourly_limit", "hourly_remaining",
		"daily_used", "daily_limit", "daily_remaining",
		"rpm_current", "rpm_limit", "within_budget",
	} {
		if _, ok := block[key]; !ok {
			t.Errorf("budget block key %q missing", key)
		}
	}

	// Contract 3 (F5): within_cost_budget pass-through.
	// RED until leaf 01 (Contract 3).
	want := budget.GetStatus().WithinCostBudget
	raw, present := block["within_cost_budget"]
	if !present {
		t.Errorf("budget block lacks within_cost_budget (Contract 3 requires the pass-through next to within_budget)")
	} else if got, ok := raw.(bool); !ok {
		t.Errorf("within_cost_budget present but not a bool: %#v", raw)
	} else if got != want {
		t.Errorf("within_cost_budget = %v, want %v (pass-through of Budget.GetStatus().WithinCostBudget, no recompute)", got, want)
	}
}
