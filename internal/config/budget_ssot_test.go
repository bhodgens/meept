package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestDefaultConfigNoBudget verifies that DefaultConfig() expresses "no
// budget": the ONE global switch is off and every limit is zero, which the
// enforcement layer treats as unlimited. Budget VALUES live only in
// meept.json5, so the Go defaults must never be a non-zero cap.
func TestDefaultConfigNoBudget(t *testing.T) {
	cfg := DefaultConfig()
	b := cfg.LLM.Budget

	if b.Enabled {
		t.Error("DefaultConfig().LLM.Budget.Enabled = true, want false (budgets are off unless enabled)")
	}

	tokenAndRate := map[string]int{
		"hourly_token_limit":      b.HourlyTokenLimit,
		"daily_token_limit":       b.DailyTokenLimit,
		"rate_limit_rpm":          b.RateLimitRPM,
		"per_task_token_limit":    b.PerTaskTokenLimit,
		"per_session_token_limit": b.PerSessionTokenLimit,
	}
	for name, v := range tokenAndRate {
		if v != 0 {
			t.Errorf("DefaultConfig().LLM.Budget.%s = %d, want 0 (unlimited)", name, v)
		}
	}

	cost := map[string]float64{
		"daily_cost_limit":       b.DailyCostLimit,
		"hourly_cost_limit":      b.HourlyCostLimit,
		"per_task_cost_limit":    b.PerTaskCostLimit,
		"per_session_cost_limit": b.PerSessionCostLimit,
	}
	for name, v := range cost {
		if v != 0 {
			t.Errorf("DefaultConfig().LLM.Budget.%s = %v, want 0 (unlimited)", name, v)
		}
	}

	// The hierarchical budget has no enable flag of its own; Total 0 means the
	// hierarchy is never wired.
	if got := cfg.Agent.Budget.Total; got != 0 {
		t.Errorf("DefaultConfig().Agent.Budget.Total = %d, want 0 (no cap)", got)
	}
}

// TestBudgetConfigMissingSwitchDefaultsOff pins the back-compat contract: a
// config that sets limits but omits the global switch must NOT silently enforce
// them. The switch defaults to false, so nothing is enforced until the operator
// opts in.
func TestBudgetConfigMissingSwitchDefaultsOff(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "no budget section at all",
			content: `{
				"daemon": {"log_level": "DEBUG"}
			}`,
		},
		{
			name: "limits set, no enabled key",
			content: `{
				"llm": {
					"budget": {
						"hourly_token_limit": 50000,
						"daily_cost_limit": 10.0,
						"rate_limit_rpm": 30
					}
				}
			}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "meept.json5")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadJSON5Config(path)
			if err != nil {
				t.Fatalf("LoadJSON5Config: %v", err)
			}
			if cfg.LLM.Budget.Enabled {
				t.Error("LLM.Budget.Enabled = true for a config without an explicit llm.budget.enabled; want false")
			}
			// The values still load (meept.json5 remains the SSOT for them);
			// they are simply inert until the switch is turned on.
			if tc.name == "limits set, no enabled key" {
				if got := cfg.LLM.Budget.HourlyTokenLimit; got != 50000 {
					t.Errorf("HourlyTokenLimit = %d, want 50000 (value still loads from config)", got)
				}
			}
		})
	}
}

// TestShippedTemplateBudgetDisabled verifies the template copied into
// $MEEPT_HOME/meept.json5 ships with the single switch off and every limit at
// zero, so a fresh install enforces no budget.
func TestShippedTemplateBudgetDisabled(t *testing.T) {
	path := filepath.Join("..", "..", "config", "meept.json5")
	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config(%s): %v", path, err)
	}
	b := cfg.LLM.Budget
	if b.Enabled {
		t.Error("shipped template llm.budget.enabled = true; want false (budgets off by default)")
	}
	if b.HourlyTokenLimit != 0 || b.DailyTokenLimit != 0 || b.RateLimitRPM != 0 ||
		b.PerTaskTokenLimit != 0 || b.PerSessionTokenLimit != 0 {
		t.Errorf("shipped template has a non-zero token/rate limit: %+v", b)
	}
	if b.DailyCostLimit != 0 || b.HourlyCostLimit != 0 || b.PerTaskCostLimit != 0 || b.PerSessionCostLimit != 0 {
		t.Errorf("shipped template has a non-zero cost limit: %+v", b)
	}
	if cfg.Agent.Budget.Total != 0 {
		t.Errorf("shipped template agent.budget.total = %d, want 0", cfg.Agent.Budget.Total)
	}
}

// budgetLimitFieldRE matches a Go struct-literal assignment of a numeric
// literal to a budget LIMIT field. It intentionally matches only numeric
// literals, so threading a configured value through
// (e.g. `HourlyLimit: bc.HourlyTokenLimit`) is allowed - the invariant is that
// no Go file INVENTS a budget, and passing config through invents nothing.
//
// Scope: the LLM budget limit surface (internal/llm.BudgetConfig and
// internal/config.BudgetConfig). Aggressiveness is excluded on purpose: it is a
// usage modifier, not a limit. Non-consumption "budgets" are likewise out of
// scope - retry budgets (agent.retry), orchestrator alert thresholds
// (orchestrator.token_budget_alert), per-IP transport rate limits
// (transport.http.rate_limit_rpm), and the reasoning/thinking tier table
// (internal/llm/reasoning.go defaultBudgetTable, a per-request model parameter
// with its own documented fallback) are not token/cost caps on LLM spend.
var budgetLimitFieldRE = regexp.MustCompile(
	`\b(HourlyLimit|DailyLimit|RateLimitRPM|PerTaskBudget|PerSessionBudget|PerTaskCostLimit|PerSessionCostLimit|HourlyTokenLimit|DailyTokenLimit|DailyCostLimit|HourlyCostLimit|PerTaskTokenLimit|PerSessionTokenLimit)\s*:\s*(-?[0-9][0-9_.]*)`)

// TestNoNonZeroBudgetDefaultsOutsideConfig is the grep-based assertion required
// by the SSOT rule: meept.json5 (surfaced by internal/config) is the only place
// budget VALUES may be defined, so no other Go file may assign a non-zero
// numeric literal to a budget limit field. A focused DefaultConfig() test
// (TestDefaultConfigNoBudget) covers the config package itself; this scan covers
// every other Go file.
func TestNoNonZeroBudgetDefaultsOutsideConfig(t *testing.T) {
	root := repoRoot(t)

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			switch {
			case rel == ".git", rel == "vendor", rel == "node_modules", rel == ".claude",
				rel == "docs", rel == "ui", rel == "testdata":
				return filepath.SkipDir
			// internal/config legitimately owns the (all-zero) defaults.
			case rel == "internal/config" || strings.HasPrefix(rel, "internal/config/"):
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := budgetLimitFieldRE.FindStringSubmatch(line)
			if m == nil || budgetLiteralIsZero(m[2]) {
				continue
			}
			offenders = append(offenders, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenders) > 0 {
		t.Errorf("non-zero budget default(s) defined outside internal/config:\n  %s\n"+
			"meept.json5 is the single source of truth for budget values; a Go file must not invent one.",
			strings.Join(offenders, "\n  "))
	}
}

// budgetLiteralIsZero reports whether a numeric literal is zero. Unparsable
// literals are treated as non-zero so the scan fails closed.
func budgetLiteralIsZero(lit string) bool {
	v, err := strconv.ParseFloat(strings.ReplaceAll(lit, "_", ""), 64)
	if err != nil {
		return false
	}
	return v == 0
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
