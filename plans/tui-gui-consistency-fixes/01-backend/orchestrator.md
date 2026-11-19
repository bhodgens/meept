# Backend roster RPC - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 2 test leaves, 2 implementation leaves
- **Scope:** Backend roster RPC

## Goal

Deliver a single RPC that merges registry agents, bots, and employees into the C1 row shape with active counts and daily cost. Both UIs consume this RPC.

## Architecture

The daemon already has all three sources (AgentRegistry.ListSpecs, bot.Manager.ListBots, employee.Manager.ListEmployees). A new handler aggregates them. Active count comes from live queue registration; daily cost from bot state and per-agent metrics. The RPC is registered on the same socket path the TUI uses.

## Interface Contracts

### C1: RosterEntry

```
type RosterEntry struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Type string `json:"type"`
    Status string `json:"status"`
    ActiveCount int `json:"active_count"`
    DailyCostCents int `json:"daily_cost_cents"`
    Role string `json:"role,omitempty"`
    Description string `json:"description,omitempty"`
    Enabled bool `json:"enabled"`
}
// RPC "agents.roster" -> {"agents": []RosterEntry}
// Owner: 01-roster-rpc.md
// Consumers: 02-tui/02-agents-columns.md, 03-gui/02-popup-tabs.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-roster-tests.md | leaf | none | 26K | A0 |
| 01 | 01-roster-rpc.md | leaf | 00T | 55K | A1 |
| 02 | 02-roster-active-cost.md | leaf | 00T, 01 | 45K | A2 |

Same-letter children have no inter-dependencies and dispatch together
(max 3 per batch).

## Dispatch Protocol

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T- test leaves first: tests only, fail RED for
missing-implementation reasons. Validate in-session (never delegate
review) against the Test Gate matrix; apply the delta protocol; commit
gate tests with --no-verify (RED-by-design; record scoped gates run).
Set children to TESTS_VALIDATED. Then implementation may dispatch.

### Phase 1: Dispatch Concurrency Groups

Dispatch same-letter children simultaneously via `delegate_task`: goal =
"Implement all tasks from [leaf]"; context = full leaf text + contracts +
conventions + relevant source INLINED; include the no-commit, no-git-add,
no-read_file, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts, Review Checklist; run tests; gate-test diff must be
empty. Pass: commit with `git commit -- <exact paths>` + status REVIEWED.
Gaps: re-dispatch with findings, max 3 cycles, then escalate to parent.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts between children, run the
full suite, check no import cycles. Pass: normalize formatting, run the
line-number corruption check, commit integration changes, set children
COMPLETE, report COMPLETE to parent.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go
- **Naming:** exported types in internal/agent or internal/rpc
- **Imports:** standard group then project group; no unused
- **Errors:** wrap with `%w`; typed-nil guards on setters
- **Testing:** table-driven unit tests + e2e suite
- **Formatting:** gofmt before reporting
- **Commits:** orchestrator only

## Requirement Traceability

See root `../master.md` Requirement Traceability. This branch covers R1-R14
as listed in each leaf's Goal.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T | TESTS_VALIDATED | 1 | RED undefined C1 symbols |
| 01 | PENDING | 0 | |
| 02 | PENDING | 0 | |


Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until this branch's
test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

`go build ./...` then `go test -p 2 ./internal/rpc/... ./internal/agent/...` then `make e2e-fast`. Verify `agents.roster` over the Unix socket returns three distinct type values.

## Test Gate

Test leaves dispatched in Phase 0, validated by the control agent
(orchestrator, in-session).

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | roster merges 3 sources | 01-roster-rpc.md T1 | TestRoster_MergesSources | e2e roster RPC | PENDING |
| F2 | active count + cost | 02-roster-active-cost.md T1 | TestRoster_ActiveAndCost | e2e roster fields | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. Q1: Which registration path owns `GET /api/v1/agents` - the employee HTTP
   handler (`internal/comm/http/server.go:1912`) or the web handler
   (`internal/comm/web/agents.go:13-29`)? The roster leaf must verify before
   extending either. Impact: wrong seam means the TUI still sees employees only.
## Notes

Confirm route ownership before extending: `internal/comm/http/server.go:1912` vs `internal/comm/web/agents.go:13-29`.
