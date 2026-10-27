package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Gate tests for the comm/http budget-status wire surface
// (docs/plans/budget-status-consistency/02-comm-http-parity.md, matrix rows
// F7-F9). BUG: handleDaemonStatus (internal/comm/http/server.go ~:2352)
// recomputes `within_budget = hourlyUsed < hourlyRemaining && dailyUsed <
// dailyRemaining` from the legacy 14-value BudgetStatusGetter tuple. Doubly
// wrong: (a) with limits disabled remaining == 0 so `used < 0` reads false —
// an unlimited daemon reports "over budget"; (b) even with limits enabled
// `used < limit-used` is not the authoritative predicate
// (llm.Budget.GetStatus().WithinBudget is). The block also lacks
// `within_cost_budget` entirely.
//
// These tests wire the CURRENT legacy tuple getter (daemon.go:1634 closure
// shape) over a REAL llm.Budget and assert wire OUTPUT only — never which
// getter field the handler consumed — so they keep compiling and passing
// after leaf 02 swaps the wiring to BudgetStatusSnapshotGetter (the legacy
// field remains supported via llm.LegacyTupleBudgetSnapshot).

// statusBudgetBlock drives handleDaemonStatus over a real Budget wired via
// the legacy 14-value tuple getter and returns the decoded status["budget"]
// block. Reuses mockDaemonController from server_test.go (same package).
func statusBudgetBlock(t *testing.T, budget *llm.Budget) map[string]any {
	t.Helper()
	srv := NewServer(ServerConfig{}, nil, &mockDaemonController{running: true, pid: 4242, uptime: time.Minute}, nil, nil, nil)
	srv.BudgetStatusGetter = func() (int, int, int, int, int, int, float64, float64, float64, float64, float64, float64, int, int) {
		bs := budget.GetStatus()
		return bs.HourlyUsed, bs.HourlyRemaining, bs.DailyUsed, bs.DailyRemaining, bs.RPMCurrent, bs.RPMLimit, bs.DailyCostUsed, bs.DailyCostLimit, bs.HourlyCostUsed, bs.HourlyCostLimit, bs.PerTaskCost, bs.PerSessionCost, bs.PerTaskBudget, bs.PerSessionBudget
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", http.NoBody)
	w := httptest.NewRecorder()
	srv.handleDaemonStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("daemon status: HTTP %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode status body: %v", err)
	}
	block, ok := body["budget"].(map[string]any)
	if !ok {
		t.Fatalf("status body has no budget block: %#v", body)
	}
	return block
}

// TestHTTPStatusBudgetDisabledLimits pins matrix rows F7/F8 on the disabled
// posture: all limits 0 (repo-shipped default), 500 tokens recorded. The
// wire must report the authoritative disabled shape — unconstrained — not
// exhaustion.
//
// RED today: server.go:2352 recomputes within_budget as `500 < 0` = false
// (want true), and within_cost_budget is absent from the block entirely.
func TestHTTPStatusBudgetDisabledLimits(t *testing.T) {
	budget := llm.NewBudget(llm.BudgetConfig{}, nil) // all limits disabled
	budget.RecordUsage(llm.TokenUsage{TotalTokens: 500})

	// Guard: if the authoritative source is wrong the wire assertions
	// below would be vacuous.
	bs := budget.GetStatus()
	if !bs.WithinBudget || !bs.WithinCostBudget {
		t.Fatalf("precondition: disabled config must be unconstrained (WithinBudget=%v WithinCostBudget=%v, want true/true)", bs.WithinBudget, bs.WithinCostBudget)
	}

	block := statusBudgetBlock(t, budget)

	if got, ok := block["within_budget"].(bool); !ok {
		t.Errorf("within_budget missing or not a bool: %#v", block["within_budget"])
	} else if got != true {
		t.Errorf("within_budget = false on a disabled (unlimited) budget; the handler must pass through Budget.GetStatus().WithinBudget (true), not recompute used < remaining (server.go:2352)")
	}

	raw, present := block["within_cost_budget"]
	if !present {
		t.Errorf("within_cost_budget key absent from the budget block (Contract 2 requires it on every surface; RPC/HTTP parity)")
	} else if got, ok := raw.(bool); !ok {
		t.Errorf("within_cost_budget present but not a bool: %#v", raw)
	} else if got != true {
		t.Errorf("within_cost_budget = %v, want true (cost limits disabled = unconstrained)", got)
	}

	if got, ok := block["hourly_used"].(float64); !ok || int(got) != 500 {
		t.Errorf("hourly_used = %#v, want 500", block["hourly_used"])
	}
	if got, ok := block["hourly_remaining"].(float64); !ok || int(got) != 0 {
		t.Errorf("hourly_remaining = %#v, want 0 (disabled serializes as 0, no -1 sentinel)", block["hourly_remaining"])
	}
	if got, ok := block["daily_remaining"].(float64); !ok || int(got) != 0 {
		t.Errorf("daily_remaining = %#v, want 0", block["daily_remaining"])
	}
}

// TestHTTPStatusBudgetThreeStates pins the Contract-4 three-state table on
// the HTTP daemon-status surface (matrix row F7): disabled / under-limit /
// at-limit-with-cost-breach, each driven by a REAL llm.Budget (mirrors the
// internal/rpc/server_budget_status_test.go constructions, aggressiveness
// 1.0 so effectiveLimit == base).
//
// RED today on the disabled row only: within_budget recomputes to false.
// The under/at-limit rows' within_budget happens to match the recompute
// today; within_cost_budget is checked when present so those rows stay
// GREEN pre-fix and are fully pinned post-fix (the hard presence pin lives
// in TestHTTPStatusBudgetDisabledLimits).
func TestHTTPStatusBudgetThreeStates(t *testing.T) {
	tests := []struct {
		name string
		cfg  llm.BudgetConfig
		// usage recorded via RecordUsage before the status call.
		usage int
		// costUSD, when > 0, is recorded via RecordCost so the at-limit
		// row exercises a REAL cost-limit breach (authoritative
		// semantics: with cost limits disabled, WithinCostBudget stays
		// true even when token usage is over).
		cost             float64
		wantWithinBudget bool
		wantWithinCost   bool
		wantHourlyUsed   int
		wantHourlyRemain int
	}{
		{
			// F7/F8: all limits 0. The wire must report the
			// disabled shape, not exhaustion.
			name:             "disabled: all limits 0 -> within_budget true, remaining 0",
			cfg:              llm.BudgetConfig{},
			usage:            500,
			wantWithinBudget: true,
			wantWithinCost:   true,
			wantHourlyUsed:   500,
			wantHourlyRemain: 0,
		},
		{
			// F7: limits set, usage under. Aggressiveness 1.0 so
			// effectiveLimit(base) == base (factor 0.5+0.5*a).
			name:             "under limit: hourly 1000, usage 100 -> within_budget true, remaining 900",
			cfg:              llm.BudgetConfig{HourlyLimit: 1000, DailyLimit: 5000, Aggressiveness: 1.0},
			usage:            100,
			wantWithinBudget: true,
			wantWithinCost:   true, // cost limits disabled -> unconstrained
			wantHourlyUsed:   100,
			wantHourlyRemain: 900,
		},
		{
			// F7: usage at/over the effective token limit AND a
			// real cost-limit breach ($2.00 vs $1.00
			// HourlyCostLimit), so both authoritative booleans
			// are false.
			name:             "at/over limit: hourly 1000, usage 1100, cost 2.00 over 1.00 limit -> booleans false, remaining 0",
			cfg:              llm.BudgetConfig{HourlyLimit: 1000, DailyLimit: 5000, HourlyCostLimit: 1.0, Aggressiveness: 1.0},
			usage:            1100,
			cost:             2.00,
			wantWithinBudget: false,
			wantWithinCost:   false,
			wantHourlyUsed:   1100,
			wantHourlyRemain: 0,
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
			block := statusBudgetBlock(t, budget)

			if got, ok := block["within_budget"].(bool); !ok {
				t.Errorf("within_budget missing or not a bool: %#v", block["within_budget"])
			} else if got != tt.wantWithinBudget {
				t.Errorf("within_budget = %v, want %v (must be the pass-through of Budget.GetStatus().WithinBudget, not the server.go:2352 recompute)", got, tt.wantWithinBudget)
			}

			// Checked when present: absent pre-fix (hard presence pin
			// in TestHTTPStatusBudgetDisabledLimits), fully pinned
			// post-fix.
			if raw, present := block["within_cost_budget"]; present {
				if got, ok := raw.(bool); !ok {
					t.Errorf("within_cost_budget present but not a bool: %#v", raw)
				} else if got != tt.wantWithinCost {
					t.Errorf("within_cost_budget = %v, want %v", got, tt.wantWithinCost)
				}
			}

			if got, ok := block["hourly_used"].(float64); !ok || int(got) != tt.wantHourlyUsed {
				t.Errorf("hourly_used = %#v, want %d", block["hourly_used"], tt.wantHourlyUsed)
			}
			if got, ok := block["hourly_remaining"].(float64); !ok || int(got) != tt.wantHourlyRemain {
				t.Errorf("hourly_remaining = %#v, want %d", block["hourly_remaining"], tt.wantHourlyRemain)
			}
		})
	}
}

// TestHTTPStatusBudgetKeySetStable is the wire-compat golden pin (matrix row
// F9, Contract 2 "additive only"): every key the budget block publishes
// TODAY must remain present after the fix, and NO value may be a -1
// sentinel (disabled-vs-exhausted disambiguation rides the limit fields:
// *_limit == 0 means disabled). Additive keys (within_cost_budget) are
// allowed.
//
// MUST PASS today AND keep passing after leaf 02.
func TestHTTPStatusBudgetKeySetStable(t *testing.T) {
	budget := llm.NewBudget(llm.BudgetConfig{}, nil) // disabled posture
	budget.RecordUsage(llm.TokenUsage{TotalTokens: 500})
	block := statusBudgetBlock(t, budget)

	goldenKeys := []string{
		"hourly_used", "hourly_remaining",
		"daily_used", "daily_remaining",
		"rpm_current", "rpm_limit",
		"daily_cost_used", "daily_cost_limit",
		"hourly_cost_used", "hourly_cost_limit",
		"per_task_cost", "per_session_cost",
		"per_task_budget", "per_session_budget",
		"within_budget",
	}
	for _, key := range goldenKeys {
		if _, ok := block[key]; !ok {
			t.Errorf("golden budget key %q missing from the status budget block", key)
		}
	}

	// No -1 sentinels anywhere in the budget block (including additive
	// keys).
	for key, v := range block {
		switch n := v.(type) {
		case float64:
			if n == -1 {
				t.Errorf("budget key %q = -1 sentinel; disabled must serialize as 0 with the limit field disambiguating", key)
			}
		}
	}
}
