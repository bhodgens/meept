package rpc

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// Gate tests for budget-status wire consistency
// (docs/plans/budget-status-consistency/, master.md Contracts 1-4, matrix
// F1-F6). BUG A: internal/rpc/server.go ~:630 recomputes
// `within_cost_budget = dcu < dcl && hcu < hcl`, which is false when cost
// limits are disabled (0) — an unlimited daemon reports "over budget".
// Contract 2: `within_cost_budget` must be a pass-through of
// Status.WithinCostBudget and a NEW `within_budget` key must carry
// Status.WithinBudget.

// newBudgetStatusServer builds the status handler exactly like the
// server_test.go fixtures, but wires BudgetStatusGetter/TokenLimitGetter as
// daemon-style closures over a REAL llm.Budget (the daemon.go:461 shape) so
// the pass-through — not hand-fed scalars — is what's tested.
func newBudgetStatusServer(t *testing.T, budget *llm.Budget) map[string]any {
	t.Helper()
	srv := New(&Config{SocketPath: ""}, bus.New(nil, nil), slog.Default())
	srv.BudgetStatusGetter = func() (int, int, int, int, int, int, float64, float64, float64, float64, float64, float64, int, int) {
		bs := budget.GetStatus()
		return bs.HourlyUsed, bs.HourlyRemaining, bs.DailyUsed, bs.DailyRemaining, bs.RPMCurrent, bs.RPMLimit, bs.DailyCostUsed, bs.DailyCostLimit, bs.HourlyCostUsed, bs.HourlyCostLimit, bs.PerTaskCost, bs.PerSessionCost, bs.PerTaskBudget, bs.PerSessionBudget
	}
	srv.TokenLimitGetter = func() (int, int) {
		bs := budget.GetStatus()
		return bs.HourlyLimit, bs.DailyLimit
	}
	srv.registerBuiltinHandlers()

	srv.mu.RLock()
	handler := srv.handlers["status"]
	srv.mu.RUnlock()
	if handler == nil {
		t.Fatal("status handler not registered")
	}
	raw, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("status handler: %v", err)
	}
	result, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("status handler returned %T, want map[string]any", raw)
	}
	return result
}

// TestStatusBudgetWireShape pins the Contract-4 three-state table on the RPC
// status surface (matrix rows F1/F2/F3).
//
// RED today:
//   - disabled state: within_cost_budget == false (BUG A, server.go:630
//     recompute `0 < 0`), and within_budget is absent (F2, Contract 2).
//   - under-limit state: same two failures (cost limits disabled there too).
func TestStatusBudgetWireShape(t *testing.T) {
	tests := []struct {
		name string
		cfg  llm.BudgetConfig
		// usage recorded via RecordUsage before the status call.
		usage int
		// costUSD, when > 0, is recorded via RecordCost so the at/over
		// state exercises a REAL cost-limit breach (authoritative
		// semantics: With cost limits disabled, WithinCostBudget stays
		// true even when token usage is over).
		cost               float64
		wantWithinBudget   bool
		wantWithinCost     bool
		wantTokensUsed     int
		wantTokensRemain   int
		wantHourlyTokLimit int
		wantDailyTokLimit  int
	}{
		{
			// F1/F2/F3: all limits 0 (repo-shipped default). The wire must
			// report the disabled shape, not exhaustion.
			name:               "disabled: all limits 0 -> booleans true, limits 0, remaining 0",
			cfg:                llm.BudgetConfig{},
			usage:              500,
			wantWithinBudget:   true,
			wantWithinCost:     true,
			wantTokensUsed:     500,
			wantTokensRemain:   0,
			wantHourlyTokLimit: 0,
			wantDailyTokLimit:  0,
		},
		{
			// F3: limits set, usage under. Aggressiveness 1.0 so
			// effectiveLimit(base) == base (factor 0.5+0.5*a).
			name:               "under limit: hourly 1000, usage 100 -> booleans true, remaining 900",
			cfg:                llm.BudgetConfig{HourlyLimit: 1000, DailyLimit: 5000, Aggressiveness: 1.0},
			usage:              100,
			wantWithinBudget:   true,
			wantWithinCost:     true, // cost limits disabled -> unconstrained
			wantTokensUsed:     100,
			wantTokensRemain:   900,
			wantHourlyTokLimit: 1000,
			wantDailyTokLimit:  5000,
		},
		{
			// F3: usage at/over the effective token limit AND a real
			// cost-limit breach, so both authoritative booleans are false.
			name:               "at/over limit: hourly 1000, usage 1100, cost 2.00 over 1.00 limit -> booleans false, remaining 0",
			cfg:                llm.BudgetConfig{HourlyLimit: 1000, DailyLimit: 5000, HourlyCostLimit: 1.0, Aggressiveness: 1.0},
			usage:              1100,
			cost:               2.00,
			wantWithinBudget:   false,
			wantWithinCost:     false,
			wantTokensUsed:     1100,
			wantTokensRemain:   0,
			wantHourlyTokLimit: 1000,
			wantDailyTokLimit:  5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := llm.NewBudget(tt.cfg, nil)
			if tt.usage != 0 {
				budget.RecordUsage(llm.TokenUsage{TotalTokens: tt.usage})
			}
			if tt.cost > 0 {
				budget.RecordCost(llm.CostRecord{Timestamp: time.Now(), CostUSD: tt.cost})
			}
			result := newBudgetStatusServer(t, budget)

			// F1: within_cost_budget must be the authoritative boolean.
			// RED today in the disabled/under states (server.go:630
			// recompute: dcu < dcl && hcu < hcl is `0 < 0` = false).
			if got, ok := result["within_cost_budget"].(bool); !ok {
				t.Errorf("within_cost_budget missing or not a bool: %#v", result["within_cost_budget"])
			} else if got != tt.wantWithinCost {
				t.Errorf("within_cost_budget = %v, want %v", got, tt.wantWithinCost)
			}

			// F2 (Contract 2): NEW within_budget key, present in every
			// state, equal to Status.WithinBudget. RED today: key absent.
			raw, present := result["within_budget"]
			if !present {
				t.Errorf("within_budget key absent from status result (Contract 2 requires it; RPC/HTTP parity)")
			} else if got, ok := raw.(bool); !ok {
				t.Errorf("within_budget present but not a bool: %#v", raw)
			} else if got != tt.wantWithinBudget {
				t.Errorf("within_budget = %v, want %v", got, tt.wantWithinBudget)
			}

			if got, ok := result["tokens_used"].(int); !ok || got != tt.wantTokensUsed {
				t.Errorf("tokens_used = %#v, want %d", result["tokens_used"], tt.wantTokensUsed)
			}
			if got, ok := result["tokens_remaining"].(int); !ok || got != tt.wantTokensRemain {
				t.Errorf("tokens_remaining = %#v, want %d", result["tokens_remaining"], tt.wantTokensRemain)
			}
			if got, ok := result["hourly_token_limit"].(int); !ok || got != tt.wantHourlyTokLimit {
				t.Errorf("hourly_token_limit = %#v, want %d", result["hourly_token_limit"], tt.wantHourlyTokLimit)
			}
			if got, ok := result["daily_token_limit"].(int); !ok || got != tt.wantDailyTokLimit {
				t.Errorf("daily_token_limit = %#v, want %d", result["daily_token_limit"], tt.wantDailyTokLimit)
			}
		})
	}
}

// TestStatusBudgetKeySetStable is the wire-compat golden pin (matrix row F6,
// Contract 2 "additive only"). Every key the budget block publishes today
// must remain present after the fix, with unchanged names/types, and NO
// value may be a -1 sentinel (disabled-vs-exhausted disambiguation rides the
// limit fields: *_limit == 0 means disabled).
//
// This test MUST PASS today and keep passing after leaf 01.
func TestStatusBudgetKeySetStable(t *testing.T) {
	budget := llm.NewBudget(llm.BudgetConfig{}, nil) // disabled posture
	budget.RecordUsage(llm.TokenUsage{TotalTokens: 500})
	result := newBudgetStatusServer(t, budget)

	goldenKeys := []string{
		"tokens_used", "tokens_remaining",
		"daily_used", "daily_remaining",
		"rpm_current", "rpm_limit",
		"daily_cost_used", "daily_cost_limit", "daily_cost_remaining",
		"hourly_cost_used", "hourly_cost_limit", "hourly_cost_remaining",
		"budget_used", "budget_remaining",
		"within_cost_budget",
		"per_task_cost", "per_session_cost",
		"per_task_budget", "per_session_budget",
		"hourly_token_limit", "daily_token_limit",
	}
	for _, key := range goldenKeys {
		if _, ok := result[key]; !ok {
			t.Errorf("golden budget key %q missing from status result", key)
		}
	}

	// No -1 sentinels anywhere in the budget block.
	for _, key := range goldenKeys {
		switch v := result[key].(type) {
		case int:
			if v == -1 {
				t.Errorf("budget key %q = -1 sentinel; disabled must serialize as 0 with the limit field disambiguating", key)
			}
		case float64:
			if v == -1 {
				t.Errorf("budget key %q = -1 sentinel; disabled must serialize as 0 with the limit field disambiguating", key)
			}
		}
	}
}

// TestBudgetStatusGetterCarriesAuthoritativeBooleans pins matrix row F4
// (Contract 1): the booleans reaching the wire must come from
// Budget.GetStatus() UNTOUCHED — there is no recompute path.
//
// Written against the CURRENT 14-value BudgetStatusGetter closure shape (the
// daemon.go:461/:1634 wiring) so this file compiles and the package runs
// today; no reference to a future llm.BudgetStatusSnapshot symbol is needed.
// The assertion — wire within_cost_budget equals GetStatus().WithinCostBudget
// in the disabled state — FAILS today because the getter cannot carry the
// boolean and server.go:630 recomputes it as false while the authoritative
// Status says true. After leaf 01 changes the getter (struct or +2 tuple)
// and deletes the recompute, the SAME assertion passes: this is the
// pass-through pin, not a shape pin.
func TestBudgetStatusGetterCarriesAuthoritativeBooleans(t *testing.T) {
	budget := llm.NewBudget(llm.BudgetConfig{}, nil) // all limits disabled
	budget.RecordUsage(llm.TokenUsage{TotalTokens: 500})

	// The authoritative source of truth (Contract 1).
	bs := budget.GetStatus()
	if !bs.WithinCostBudget {
		// Guard: if the budget-level boolean is wrong the wire assertion
		// below would be vacuous. (WithinCostBudget is already correct
		// today; WithinBudget's disabled-shape bug is pinned in
		// internal/llm/budget_status_shape_test.go.)
		t.Fatalf("precondition: Budget.GetStatus().WithinCostBudget = false on a disabled config; want true")
	}

	result := newBudgetStatusServer(t, budget)

	got, ok := result["within_cost_budget"].(bool)
	if !ok {
		t.Fatalf("within_cost_budget missing or not a bool: %#v", result["within_cost_budget"])
	}
	if got != bs.WithinCostBudget {
		t.Errorf("wire within_cost_budget = %v but authoritative Budget.GetStatus().WithinCostBudget = %v — the RPC handler must pass the Status boolean through untouched, not recompute it from raw counters (server.go:630)", got, bs.WithinCostBudget)
	}
}
