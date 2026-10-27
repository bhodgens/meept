# Gate Tests — Budget-Status Wire Consistency (TESTS ONLY)

> **For the implementing agent:** You are the TEST implementer for this gate
> leaf. Write ONLY the test files below. Do NOT modify any implementation
> file. Target end state: RED — failing assertions against the current
> server.go:630 recompute behavior, and/or compile errors naming ONLY the
> new getter shape symbols if you write tests against the Contract-1 struct.
> Do NOT commit. Do NOT git add. No read_file→write_file; inspect with
> terminal cat/sed; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Pin Contracts 1-4 (master.md) with tests before the fix exists.
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Context

- Bug under test: `internal/rpc/server.go:630` — `result["within_cost_budget"] = dcu < dcl && hcu < hcl` reads false when cost limits are disabled (0), on the `status`/`daemon.status` RPC handler wired via `BudgetStatusGetter` (14 positional scalars; daemon.go:461 and :1634 are the two wiring sites).
- Authoritative semantics: `internal/llm/budget.go` `Status()` — `WithinCostBudget` (:707-712, :739) and `WithinBudget` (:731) treat limit 0 as unconstrained.
- Existing test conventions: `internal/rpc/server_test.go` (how the status handler + getters are fixture-wired — read it FIRST and reuse its harness), `internal/llm/budget_test.go` (Status tables).
- HTTP surface: `internal/daemon/components.go` budget blocks (~:8664, ~:9473) embed `budgetStatus.*` and carry `within_budget` but not `within_cost_budget`.

## Files to create

1. `internal/rpc/server_budget_status_test.go` (package per server_test.go)
2. `internal/llm/budget_status_shape_test.go` (package llm) — ONLY if the Status() disabled-shape assertions do not already exist in budget_test.go (grep first; if they exist, skip this file and note it)
3. `internal/daemon/components_budget_keys_test.go` (package per existing components tests) — only if the components budget block is unit-reachable with existing fixtures; else document why F5 pins at the RPC/integration layer instead (check how components_test.go builds handlers)

## Tasks

### Task 1: RPC three-state table (F1, F2, F3) — server_budget_status_test.go

Build the status handler exactly like server_test.go fixtures do, with a BudgetStatusGetter closure over a real `llm.Budget` (construct via the exported config path used in budget_test.go — do NOT mock the 14 scalars by hand for the state table; drive real Budget usage so the pass-through is what's tested).

`TestStatusBudgetWireShape` table, three states:
- **disabled:** all limits 0. Assert: `within_cost_budget == true` (FAILS today — the bug), `within_budget == true` (key may not exist today: assert presence too, FAILS today = F2 RED), `hourly_token_limit == 0`, `daily_token_limit == 0`, `tokens_remaining == 0`.
- **under limit:** hourly limit 1000, usage 100 (drive via the Budget's exported record path used in budget_test.go). Assert booleans true, `tokens_remaining == 900`.
- **at/over limit:** usage >= limit. Assert booleans false, remaining 0.

### Task 2: Golden key-set pin (F6) — same file

`TestStatusBudgetKeySetStable`: capture the full status result map in the disabled state; assert every EXISTING key from today's handler is present (tokens_used, tokens_remaining, daily_used, daily_remaining, rpm_current, rpm_limit, daily_cost_used, daily_cost_limit, daily_cost_remaining, hourly_cost_used, hourly_cost_limit, hourly_cost_remaining, budget_used, budget_remaining, within_cost_budget, per_task_cost, per_session_cost, per_task_budget, per_session_budget, hourly_token_limit, daily_token_limit) and that NO value equals -1 anywhere in the budget block (no sentinels). This test passes today and must keep passing after the fix (compat pin).

### Task 3: Getter-shape pin (F4)

Write `TestBudgetStatusGetterCarriesAuthoritativeBooleans` against the CURRENT 14-value getter: call the daemon wiring closure shape and assert there is NO path for WithinCostBudget/WithinBudget to reach the RPC result except a recompute — i.e. pin the current gap with a comment, and structure the test so that after leaf 01 changes the getter (struct or +2 tuple) the SAME test asserts the booleans now flow from `Budget.GetStatus()` untouched. If a compile-time symbol for the new shape is unavoidable, reference `llm.BudgetStatusSnapshot` and accept the compile-RED (report it as the expected RED symbol).

### Task 4: HTTP surface keys (F5) — components_budget_keys_test.go (if feasible)

Assert the components budget block key set includes `within_budget` today and (post-fix) `within_cost_budget` — write the assertion for the post-fix shape and mark the test with a comment `// RED until leaf 01 (Contract 3)`. If components handlers are not unit-reachable without heavy fixtures, do NOT build new infrastructure: skip the file, and instead extend Task 1's file with a comment-documented note that F5 is pinned at review time by grep (orchestrator runs: `grep -c 'within_cost_budget' internal/daemon/components.go` >= 2 after fix).

## Self-Verification Checklist

- [ ] `go test -count=1 ./internal/rpc/ -run 'TestStatusBudget'` shows the EXPECTED reds: disabled-state within_cost_budget assertion FAILS (current bug), within_budget key-presence FAILS (F2 not yet added); golden key-set test PASSES
- [ ] No implementation file modified (`git status --short | grep -v '^??'` shows only sibling dirt you never touched)
- [ ] Compile errors (if any) name ONLY BudgetStatusSnapshot (or the agreed new symbol) — nothing else
- [ ] Every test doc comment names its matrix row (F1-F6)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Control Agent)

- [ ] The disabled-state failure is provably the server.go:630 recompute (read the assertion + fixture wiring)
- [ ] Real Budget drives the table (not hand-fed scalars) for states; getter test may use scalars
- [ ] Golden key-set passes pre-fix (it is the compat pin, not a RED)
- [ ] No gate-test tautologies; wall-clock under 2s total
- [ ] git status shows only new test files

Output: APPROVED or gaps with file:line.
