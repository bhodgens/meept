# comm/http Budget Parity — Implementation Leaf

> **For the implementing agent:** Gate tests are written and validated BEFORE
> you are dispatched (Phase 0 of this leaf). Make them pass. Do NOT delete,
> skip, weaken, or rewrite any gate test. If one appears wrong, STOP and
> report. Do NOT commit. Do NOT git add. No read_file→write_file; inspect
> with terminal cat/sed; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Kill the THIRD recompute instance of the budget-status bug class: `internal/comm/http/server.go:2352` (`within_budget = hourlyUsed < hourlyRemaining && dailyUsed < dailyRemaining` — wrong even for enabled limits: compares used vs REMAINING) and migrate the daemon.go:1634 wiring to the snapshot getter.
- **Dependencies:** leaf 01 REVIEWED (577228e8: `llm.BudgetStatusSnapshot`, `SnapshotFromStatus`, `LegacyTupleBudgetSnapshot`, rpc pass-through pattern)
- **Estimated Context:** 55K
- **Concurrency Group:** C

## Goal

After leaf 01, two of three wire surfaces pass through the authoritative
`Budget.Status` booleans. The comm/http status handler still recomputes
`within_budget` from raw scalars — and its formula (`used < remaining`) is
doubly wrong: for disabled limits remaining=0 so it reads false (same class
as bugs A/B), and even with limits enabled `used < limit-used` is not the
authoritative predicate. It also lacks `within_cost_budget`. Per the user
directive (full-codebase consistency) this leaf closes the class.

## Interface Contracts (From Parent)

Contract 2/3 semantics apply to this surface:
- `status["budget"]["within_budget"]` = `snapshot.WithinBudget` (pass-through)
- `status["budget"]["within_cost_budget"]` = `snapshot.WithinCostBudget` (NEW key, additive)
- All existing keys in the budget block byte-identical.
- Getter: add `BudgetStatusSnapshotGetter func() llm.BudgetStatusSnapshot` to
  the comm/http Server (mirror rpc.Server's leaf-01 shape, including the
  legacy-tuple bridge via `llm.LegacyTupleBudgetSnapshot` so the existing
  BudgetStatusGetter field keeps working for any current wiring/tests).
- `internal/daemon/daemon.go:1634` wiring migrates to the snapshot getter
  (same closure style as the :461 migration in leaf 01).

## Gate tests (Phase 0, written before this leaf dispatches)

File: `internal/comm/http/server_budget_status_test.go` (or the package's
existing status-handler test file conventions — check first):
- `TestHTTPStatusBudgetDisabledLimits`: all limits 0, usage recorded →
  budget block `within_budget == true`, `within_cost_budget == true`,
  remaining fields 0 (RED today: recompute yields false; cost key absent).
- `TestHTTPStatusBudgetThreeStates`: disabled / under / at-limit table
  driving a real llm.Budget (mirror the rpc gate test's approach, including
  the cost-breach construction for the at-limit row).
- `TestHTTPStatusBudgetKeySetStable`: existing budget-block keys all present
  post-change (compat pin; GREEN today except it must still pass after).

## Tasks

### Task 1: Snapshot getter on comm/http Server

**Files:** Modify `internal/comm/http/server.go`. Add the
`BudgetStatusSnapshotGetter` field beside the existing `BudgetStatusGetter`
(~:141); in the status handler (~:2334-2354) prefer the snapshot getter when
set, else bridge the legacy tuple through `llm.LegacyTupleBudgetSnapshot`;
replace the :2352 recompute with pass-through; add `within_cost_budget`.

### Task 2: Daemon wiring migration

**Files:** Modify `internal/daemon/daemon.go` (~:1634) — wire
`BudgetStatusSnapshotGetter` returning `llm.SnapshotFromStatus(bs)` from the
same budget tracker the legacy closure reads. Keep or drop the legacy wiring
per what comm/http tests need (prefer keeping both fields set during
transition; note in Deviations if you drop).

### Task 3: Docs touch

`docs/configuration/token-budgets.md` "Disabled Limits on the Wire" section:
add one line that all THREE surfaces (RPC status, HTTP daemon status,
components budget block) now pass through the authoritative booleans.

## Self-Verification Checklist

- [ ] Gate tests GREEN: `go test -count=1 ./internal/comm/http/ -run 'TestHTTPStatusBudget'`
- [ ] `go test -count=1 -p 2 ./internal/comm/... ./internal/daemon/ ./internal/rpc/ ./internal/llm/` GREEN
- [ ] `go build ./...` OK; vet clean; gofmt clean
- [ ] `grep -n 'hourlyUsed < hourlyRemaining' internal/comm/http/server.go` → no hits
- [ ] No gate test file modified; existing budget-block keys unchanged

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] All three wire surfaces now pass through authoritative booleans (grep for `within_budget.*<` repo-wide → zero recompute instances outside budget.go)
- [ ] Legacy bridge keeps old field working (no wiring broken elsewhere: grep BudgetStatusGetter usages)
- [ ] Key set additive only; no renames
- [ ] daemon.go:461 and :1634 wiring consistent in style
- [ ] mutexio/predid/selflock clean

Output: APPROVED or gaps with file:line.
