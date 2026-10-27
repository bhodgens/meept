# Budget-Status Wire Consistency — Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree.
> Run Phase 0 (test gate) BEFORE any implementation dispatch; dispatch leaf
> agents; review in-session with the main model; commit after review with
> explicit paths only (shared repo — sibling sessions live). Do NOT implement
> code yourself.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 2 leaves (1 gate + 1 implementation)
- **Scope:** Make every budget-status wire surface report disabled limits consistently, with ONE source of truth (`internal/llm.Budget.Status`).
- **Repo:** /Users/caimlas/git/meept (branch main)

## Goal

`budget.*` config limits use `0 = unlimited/disabled`. The authoritative
computation in `internal/llm/budget.go` handles this correctly
(`Status.WithinCostBudget`, budget.go:707-712, 739: a limit only constrains
when > 0), but wire surfaces diverge:

1. **BUG — `internal/rpc/server.go:630`:** the `status`/`daemon.status` RPC
   handler recomputes `within_cost_budget` as `dcu < dcl && hcu < hcl`. With
   cost limits disabled (0), that is `0 < 0` = **false** — the daemon reports
   "over budget" on an unlimited config. Real-world impact 2026-10-25:
   `meept-bench doctor` showed `within_cost_budget: false`,
   `tokens_remaining: 0` on a healthy unlimited daemon and an orchestrator
   misdiagnosed budget exhaustion for hours.
2. **Ambiguity — `tokens_remaining: 0`:** `budget.go:664` computes
   `hourlyRemaining = max(effHourly-hourlyUsed, 0)`, so disabled (limit 0)
   and exhausted both serialize as 0. The partial fix already in place is
   `TokenLimitGetter` (server.go:638-645) publishing
   `hourly_token_limit`/`daily_token_limit` so consumers can disambiguate
   (limit=0 ⇒ disabled). That stays the mechanism — NO -1 sentinel (it would
   change the field's meaning for every consumer: TUI types.go:21,
   daemon_service.go:31, sdk/go model_daemon_status.go:29, meept-lite
   handlers.go:244). The gap: not every surface publishes the limit pair,
   and no test pins the disabled-limits shape.
3. **Duplication:** `BudgetStatusGetter` (daemon.go:461, :1634) passes 14
   scalars but NOT `WithinCostBudget`, forcing the RPC handler to recompute
   (bug 1). `components.go:8664` and `:9473` embed `budgetStatus.*` fields
   directly (correct pattern) but neither includes `within_cost_budget`.

Decision (from the user, 2026-10-25): correct the FULL codebase to one
consistent shape — disabled limits must never render as exhaustion or
over-budget anywhere on the wire.

## Architecture

Single source of truth: `Budget.Status` (internal/llm/budget.go) already
computes `WithinBudget` and `WithinCostBudget` with the limit>0 guards.
Every wire surface must PASS THROUGH Status fields, never recompute from
raw counters. Disabled-vs-exhausted disambiguation rides the limit fields
(`*_limit` = 0 means disabled; `*_remaining` = 0 alongside a nonzero limit
means exhausted). No sentinel values; no JSON key renames (SDK/TUI/bench
compat).

## Interface Contracts

### Contract 1: BudgetStatusGetter carries the authoritative booleans

```go
// internal/rpc/server.go — BudgetStatusGetter gains WithinCostBudget and
// WithinBudget as trailing return values (or, preferred if the diff stays
// small: replace the 14-value getter with a func() llm.BudgetStatusSnapshot
// struct — pick ONE; struct preferred, 14+ scalars is the mistake that
// caused the recompute).
// internal/daemon/daemon.go:461 and :1634 — both wiring sites return
// bs.WithinCostBudget / bs.WithinBudget from Budget.GetStatus() unchanged.
```

### Contract 2: status RPC wire shape (additive only)

`status`/`daemon.status` result keys — existing keys keep their names/types:
- `within_cost_budget` = `Status.WithinCostBudget` (pass-through; the
  `dcu < dcl && hcu < hcl` recompute at server.go:630 is DELETED)
- NEW `within_budget` = `Status.WithinBudget` (parity with components.go's
  `within_budget` key so RPC and HTTP agree)
- `hourly_token_limit` / `daily_token_limit` remain published via
  TokenLimitGetter (existing); with disabled limits they read 0 and
  `tokens_remaining` 0 — the documented disabled shape.

### Contract 3: HTTP status surface parity

`internal/daemon/components.go` budget blocks (~:8664, ~:9473) gain
`within_cost_budget: budgetStatus.WithinCostBudget` next to the existing
`within_budget` key. Same pass-through rule; no recompute.

### Contract 4: pinned disabled-limits shape (the regression contract)

With all budget limits 0 (repo-shipped default), a status response asserts:
`within_budget == true`, `within_cost_budget == true`,
`hourly_token_limit == 0`, `daily_token_limit == 0`,
`tokens_remaining == 0`, `tokens_used == <whatever was consumed>`.
With limits set and usage under them: booleans true, remaining = limit-used.
With limits set and usage at/over: booleans false, remaining 0. Three states,
one table — this is the consistency definition for every surface.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate-tests.md | leaf (tests) | none | 55K | A |
| 01 | 01-passthrough-fix.md | leaf | 00T VALIDATED | 75K | B |

## Dispatch Protocol

### Phase 0: Test Gate (MANDATORY — before implementation dispatch)

1. Dispatch `00T-gate-tests.md` via `delegate_task`:
   - Goal: "Write ALL gate tests from 00T-gate-tests.md — tests only, no implementation"
   - Context: full leaf text + Contracts 1-4 INLINED + "Tests ONLY. RED state = compile errors naming ONLY the new/changed symbols (e.g. the new getter shape) plus FAILING assertions on the current server.go:630 recompute behavior. Do NOT commit. Do NOT git add. No read_file→write_file."
2. Control-agent validation (in-session): run the tests; confirm RED for the right reasons; map to the matrix; delta protocol (lesser updates to greater).
3. Commit gate tests (explicit paths): `test(rpc,llm): gate tests for budget-status wire consistency (F1-F6)`.
4. Mark TESTS_VALIDATED; then Phase 1.

### Phase 1: Dispatch 01-passthrough-fix.md

- Context: full leaf + contracts + "Gate tests exist and are committed. Make them pass. Do NOT delete, skip, weaken, or rewrite any gate test. If one appears wrong, STOP and report. Do NOT commit."
- Review in-session against the Review Checklist; run full `go test -count=1 -p 2 ./internal/rpc/ ./internal/llm/ ./internal/daemon/`; commit on pass.

## Review Checklist

- [ ] server.go:630 recompute DELETED; `within_cost_budget` is a Status pass-through
- [ ] Getter shape: struct-or-extended-tuple decision recorded; BOTH daemon.go wiring sites updated identically
- [ ] No JSON key renamed or removed (SDK/TUI/bench wire compat); only additions
- [ ] No -1 sentinels anywhere
- [ ] components.go blocks include within_cost_budget pass-through
- [ ] Contract-4 three-state table pinned by tests on the RPC surface (and HTTP surface if harness allows)
- [ ] `go test -count=1 -p 2 ./internal/rpc/ ./internal/llm/ ./internal/daemon/` GREEN; vet + gofmt clean
- [ ] Gate test files unmodified by the implementer (any diff = NEEDS_FIXES unless audited cleanup-only)
- [ ] docs: status-command docs / configuration budget docs mention the disabled shape (limit=0 ⇒ disabled, remaining=0 not exhaustion); AGENTS.md untouched unless an invariant changed

## Coding Conventions

- **Language:** Go (repo toolchain; no GOTOOLCHAIN bump)
- **Errors:** wrap %w; no ignored errors; no panic
- **Mutex:** no I/O under mutex; getter closures capture the tracker, call GetStatus() at invocation
- **Testing:** existing-file test surfaces only (bugfix class per repo e2e policy — these are wire-shape bugfixes on existing handlers; no new e2e suite required, but if the getter shape changes a struct consumed by e2e harness, check e2e/manifest.json path_map)
- **Naming:** table tests `TestX_Scenario`; mirror existing server_test.go style
- **Format:** gofmt before reporting

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate-tests | PENDING | 0 | |
| 01-passthrough-fix | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: 01 stays PENDING until 00T reaches TESTS_VALIDATED.

## Integration Test Plan

1. `go test -count=1 -p 2 ./internal/rpc/ ./internal/llm/ ./internal/daemon/` GREEN.
2. `make test` GREEN (no regression repo-wide).
3. Live smoke: start/attach a daemon with shipped config (all limits 0),
   `./bin/meept status` (or the RPC status call) shows
   `within_cost_budget: true` and the limit fields 0; `meept-bench doctor`
   against a scratch rig no longer shows the misleading false.
4. `make analyzers` clean.

## Test Gate

Test leaf: 00T-gate-tests.md — dispatched in Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | within_cost_budget pass-through: disabled limits ⇒ true (kills the server.go:630 recompute) | 01 leaf, Contracts 1-2 | internal/rpc gate test: status handler with zero-limit budget → within_cost_budget true | n/a — RPC wire shape, unit-surface | PENDING |
| F2 | within_budget added to RPC status (parity with HTTP) | 01 leaf, Contract 2 | rpc gate test: key present, equals Status.WithinBudget in all three states | n/a | PENDING |
| F3 | Three-state table pinned (disabled / under-limit / at-limit) on RPC surface | 01 leaf, Contract 4 | rpc gate test table: booleans + remaining + limit fields per state | n/a | PENDING |
| F4 | Getter refactor: struct (or extended tuple) carries authoritative booleans; both daemon wiring sites identical | 01 leaf, Contract 1 | internal/daemon gate test exercising BudgetStatusGetter wiring (or compile-level pin via the struct) | n/a | PENDING |
| F5 | HTTP surface parity: components.go budget blocks include within_cost_budget pass-through | 01 leaf, Contract 3 | daemon/http gate test on the budget block keys | n/a | PENDING |
| F6 | No wire compat break: existing keys/types unchanged; no -1 sentinels | 01 leaf | gate test asserting the full key set of a status response (golden-key list) | sdk/go model unmarshal test if cheap | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)

## Open Questions

1. **Getter shape: struct vs extended tuple.** Rec: struct
   (`func() llm.BudgetStatusSnapshot` or reuse `llm.Status` directly if the
   import direction allows — rpc importing llm types is already the case for
   other getters; verify). Impact: tuple+2 is the smaller diff but perpetuates
   the positional-args footgun (14 values already); struct is the clean fix
   the user asked for. Decide at 01 dispatch; pin in the delta log.
2. **TUI/lite rendering of the disabled shape.** `tokens remaining: 0` still
   prints for unlimited configs (command_handler.go:632, handlers.go:244).
   Rec: OUT of this tree's minimal scope unless trivial — rendering
   "unlimited" when limit==0 is a display nicety; record as follow-up.
   Impact: none on correctness; the wire contract is fixed either way.
3. **meept-bench doctor consumer.** The bench doctor prints the raw JSON —
   once the wire is fixed it shows true; no bench change needed. Rec: verify
   only (smoke step 3). Impact: none.

## Notes

- Shared-repo discipline: explicit-path staging; re-check `git log` mid-run;
  never `git add .`.
- Root cause class (record for AGENTS.md consideration at review): wire
  handlers must pass through computed status, never recompute from raw
  counters — the recompute is where the disabled-limit semantics got lost.
- The llama-hang-watchdog tree (sibling, COMPLETE) touched runtime_manager/
  health_checker only; no file overlap with this tree except potentially
  daemon.go wiring lines — coordinate via git log before dispatch if the
  sibling stack is still moving.
