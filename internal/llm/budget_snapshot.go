package llm

// BudgetStatusSnapshot is the flat, wire-facing copy of the authoritative
// budget Status: the 14 scalars the status surfaces publish plus the two
// authoritative booleans (WithinBudget, WithinCostBudget). Wire handlers
// (RPC status, HTTP status) consume this snapshot as a pure pass-through of
// Budget.GetStatus() and must NEVER re-derive the booleans from raw
// counters — a recompute like `used < limit` reads `0 < 0` = false when a
// limit is disabled (0) and reports an unlimited daemon as over budget.
type BudgetStatusSnapshot struct {
	HourlyUsed       int
	HourlyRemaining  int
	DailyUsed        int
	DailyRemaining   int
	RPMCurrent       int
	RPMLimit         int
	DailyCostUsed    float64
	DailyCostLimit   float64
	HourlyCostUsed   float64
	HourlyCostLimit  float64
	PerTaskCost      float64
	PerSessionCost   float64
	PerTaskBudget    int
	PerSessionBudget int
	WithinBudget     bool
	WithinCostBudget bool
}

// SnapshotFromStatus is a plain field copy of an authoritative Status —
// no logic, no recompute.
func SnapshotFromStatus(s Status) BudgetStatusSnapshot {
	return BudgetStatusSnapshot{
		HourlyUsed:       s.HourlyUsed,
		HourlyRemaining:  s.HourlyRemaining,
		DailyUsed:        s.DailyUsed,
		DailyRemaining:   s.DailyRemaining,
		RPMCurrent:       s.RPMCurrent,
		RPMLimit:         s.RPMLimit,
		DailyCostUsed:    s.DailyCostUsed,
		DailyCostLimit:   s.DailyCostLimit,
		HourlyCostUsed:   s.HourlyCostUsed,
		HourlyCostLimit:  s.HourlyCostLimit,
		PerTaskCost:      s.PerTaskCost,
		PerSessionCost:   s.PerSessionCost,
		PerTaskBudget:    s.PerTaskBudget,
		PerSessionBudget: s.PerSessionBudget,
		WithinBudget:     s.WithinBudget,
		WithinCostBudget: s.WithinCostBudget,
	}
}

// LegacyTupleBudgetSnapshot bridges the pre-snapshot 14-value
// BudgetStatusGetter tuple (plus the configured effective token limits from
// TokenLimitGetter) into a BudgetStatusSnapshot. The boolean guards mirror
// GetStatus exactly: a limit only constrains when > 0 (0 = disabled =
// unconstrained). Production wiring must use SnapshotFromStatus (the pure
// pass-through); this bridge exists only for legacy tuple consumers so the
// wire booleans keep the authoritative disabled-limits semantics.
func LegacyTupleBudgetSnapshot(hourlyUsed, hourlyRemaining, dailyUsed, dailyRemaining, rpmCurrent, rpmLimit int, dailyCostUsed, dailyCostLimit, hourlyCostUsed, hourlyCostLimit, perTaskCost, perSessionCost float64, perTaskBudget, perSessionBudget int, hourlyTokenLimit, dailyTokenLimit int) BudgetStatusSnapshot {
	withinCostBudget := true
	if dailyCostLimit > 0 {
		withinCostBudget = dailyCostUsed < dailyCostLimit
	}
	if hourlyCostLimit > 0 {
		withinCostBudget = withinCostBudget && hourlyCostUsed < hourlyCostLimit
	}
	withinTokenBudget := true
	if hourlyTokenLimit > 0 {
		withinTokenBudget = hourlyUsed < hourlyTokenLimit
	}
	if dailyTokenLimit > 0 {
		withinTokenBudget = withinTokenBudget && dailyUsed < dailyTokenLimit
	}
	return BudgetStatusSnapshot{
		HourlyUsed:       hourlyUsed,
		HourlyRemaining:  hourlyRemaining,
		DailyUsed:        dailyUsed,
		DailyRemaining:   dailyRemaining,
		RPMCurrent:       rpmCurrent,
		RPMLimit:         rpmLimit,
		DailyCostUsed:    dailyCostUsed,
		DailyCostLimit:   dailyCostLimit,
		HourlyCostUsed:   hourlyCostUsed,
		HourlyCostLimit:  hourlyCostLimit,
		PerTaskCost:      perTaskCost,
		PerSessionCost:   perSessionCost,
		PerTaskBudget:    perTaskBudget,
		PerSessionBudget: perSessionBudget,
		WithinBudget:     withinTokenBudget && withinCostBudget,
		WithinCostBudget: withinCostBudget,
	}
}
