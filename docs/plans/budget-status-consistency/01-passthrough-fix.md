# Pass-Through Fix — Budget-Status Wire Consistency

> **For the implementing agent:** The gate tests ALREADY EXIST and are
> committed (internal/rpc/server_budget_status_test.go and any sibling gate
> files). Your job: make them pass. Do NOT delete, skip, weaken, or rewrite
> any gate test. If one appears wrong, STOP and report — the orchestrator
> runs the delta protocol. Do NOT commit. Do NOT git add. No
> read_file→write_file; inspect with terminal cat/sed; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Delete the server.go recompute; carry the authoritative booleans through the getter; HTTP parity; docs line.
- **Dependencies:** 00T gate TESTS_VALIDATED
- **Estimated Context:** 75K
- **Concurrency Group:** B

## Context

Read first (terminal cat/sed): master.md Contracts 1-4; the gate test files;
`internal/rpc/server.go` status handler (~:560-650);
`internal/daemon/daemon.go` BudgetStatusGetter wiring (:461, :1634);
`internal/daemon/components.go` budget blocks (~:8664, ~:9473);
`internal/llm/budget.go` Status struct (:620-650, :700-745).

## Tasks

### Task 1: Getter shape (Contract 1) — decision recorded in Deviations

Preferred: replace the 14-value `BudgetStatusGetter func() (int, ...)` with
`func() llm.BudgetStatusSnapshot` where

```go
// internal/llm/budget.go (or a small new file in package llm)
type BudgetStatusSnapshot struct {
    HourlyUsed, HourlyRemaining, DailyUsed, DailyRemaining int
    RPMCurrent, RPMLimit                                   int
    DailyCostUsed, DailyCostLimit                          float64
    HourlyCostUsed, HourlyCostLimit                        float64
    PerTaskCost, PerSessionCost                            float64
    PerTaskBudget, PerSessionBudget                        int
    WithinBudget, WithinCostBudget                         bool
}
// SnapshotFromStatus(s Status) BudgetStatusSnapshot — plain field copy.
```

Update BOTH daemon.go wiring sites identically (they return `bs.*` from
`GetStatus()` — mechanical). If rpc importing llm creates a cycle (check
first: rpc already imports llm for other getters — verify with
`go list -deps`), fall back to tuple+2 and say why in Deviations.

### Task 1b: budget.go WithinBudget disabled-limits fix (Contract 4, delta 2026-10-27)

`internal/llm/budget.go:731` — `WithinBudget: hourlyUsed < effHourly &&
b.dailyUsed < effDaily && withinCostBudget` reads false when token limits are
disabled (0). Apply the same limit>0 guard shape the cost half already uses
(:707-712):

```go
withinTokenBudget := true
if b.hourlyLimit > 0 { withinTokenBudget = hourlyUsed < effHourly }
if b.dailyLimit > 0 { withinTokenBudget = withinTokenBudget && b.dailyUsed < effDaily }
// ...
WithinBudget: withinTokenBudget && withinCostBudget,
```

(Check existing budget_test.go expectations for WithinBudget with zero limits
first; if any test pins the CURRENT false behavior, that test is wrong per
Contract 4 — report it, do not silently rewrite.)

### Task 2: RPC handler pass-through (Contracts 2, 4)

`internal/rpc/server.go`:
- DELETE line ~630's recompute; `result["within_cost_budget"] = <snapshot>.WithinCostBudget`.
- ADD `result["within_budget"] = <snapshot>.WithinBudget`.
- Keep every existing key name/type byte-identical (golden key-set gate).
- The `TokenLimitGetter` block stays as-is.

### Task 3: HTTP parity (Contract 3)

`internal/daemon/components.go` both budget blocks: add
`"within_cost_budget": budgetStatus.WithinCostBudget` beside the existing
`"within_budget"` entry. Pass-through only.

### Task 4: Docs

- `docs/configuration/` budget page (grep for the file documenting budget
  keys): one paragraph — limit 0 = disabled; the disabled wire shape
  (`within_*_budget: true`, `*_limit: 0`, `*_remaining: 0`); remaining=0 with
  a nonzero limit = exhausted. Never recompute booleans from raw counters in
  wire handlers (root-cause class).
- Root AGENTS.md: ONLY if the review decides the pass-through rule rises to
  an invariant; otherwise skip (note choice in Deviations).

## Self-Verification Checklist

- [ ] `go test -count=1 ./internal/rpc/ -run 'TestStatusBudget'` ALL GREEN (including previously-red disabled-state + within_budget assertions; golden key-set still green)
- [ ] `go test -count=1 -p 2 ./internal/rpc/ ./internal/llm/ ./internal/daemon/` GREEN
- [ ] `go build ./...` OK; `go vet ./internal/rpc/ ./internal/llm/ ./internal/daemon/` clean; gofmt clean on touched files
- [ ] `grep -n 'dcu < dcl' internal/rpc/server.go` → no hits (recompute gone)
- [ ] No gate test file modified; no JSON key renamed/removed; no -1 anywhere in the budget block
- [ ] git status: only the files listed in Tasks 1-4 modified

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] within_cost_budget is a pure pass-through at every surface (grep for `<` comparisons against cost limits in wire handlers → zero outside budget.go)
- [ ] Both daemon.go wiring sites identical; no third getter copy left behind
- [ ] Golden key-set test green pre AND post (compat)
- [ ] Snapshot struct is a dumb copy — no logic, no recompute
- [ ] Docs paragraph lands in the right configuration file (grep-verified)
- [ ] mutexio/predid/selflock clean (`make analyzers`)

Output: APPROVED or gaps with file:line.
