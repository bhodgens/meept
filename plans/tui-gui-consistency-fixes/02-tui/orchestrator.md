# TUI surface fixes - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test leaf, 5 implementation leaves
- **Scope:** TUI surface fixes

## Goal

Update the Bubbletea TUI so the agents panel shows all configured agents with type, active count, and cost columns; the sessions list is a project-grouped expandable tree with a search entry; and the kanban view renders a clean disabled guidance message.

## Architecture

All TUI panels consume RPC through small injected client interfaces. The agents panel swaps `agents.list` for C1 `agents.roster`. The sessions model groups rows by C2 bucket and uses left/right keys to expand/collapse. Table sizing always goes through `tableutil`.

## Interface Contracts

### C1 (consumer)

`agents.roster -> {"agents": []RosterEntry}` - owner: 01-backend/01-roster-rpc.md

### C2: session bucket

`bucketFor(s) = ProjectID || ProjectPath || "unbound"` - owner: this branch

### C3: kanban hint

`const kanbanDisabledHint = "kanban board is not enabled; set [kanban].enabled=true"` - owner: 02-tui/04-kanban-error.md

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-tui-tests.md | leaf | none | 32K | B0 |
| 01 | 01-agents-data.md | leaf | 00T, C1 | 40K | B1 |
| 02 | 02-agents-columns.md | leaf | 00T, 01, C1 | 45K | B2 |
| 03 | 03-sessions-tree.md | leaf | 00T, C2 | 55K | B3 |
| 04 | 04-kanban-error.md | leaf | 00T, C3 | 25K | B4 |
| 05 | 05-search-entry.md | leaf | 00T, 03, C4 | 30K | B5 |

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

- **Language:** Go, bubbletea v2 / bubbles v2 / lipgloss v2
- **UI text:** all lowercase
- **Tables:** `tableutil.Size` both axes; `tableutil.SetRows` rows
- **Columns:** clear rows before SetColumns; save/restore cursor
- **Errors:** wrap with `%w`; no ignored errors
- **Tests:** model-level render assertions on View() containing cell text
- **Formatting:** gofmt before reporting
- **Commits:** orchestrator only

## Requirement Traceability

See root `../master.md` Requirement Traceability. This branch covers R1-R14
as listed in each leaf's Goal.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T | TESTS_VALIDATED | 1 | RED 6 assertions |
| 01 | PENDING | 0 | |
| 02 | PENDING | 0 | |
| 03 | PENDING | 0 | |
| 04 | PENDING | 0 | |
| 05 | PENDING | 0 | |


Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until this branch's
test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

`go build ./...` then `go test -p 2 ./internal/tui/... -count=1` then `make e2e-fast-area AREA=tui-flows`. Live: build `./bin/meept`, drive with `agent-tui run ./bin/meept chat`; screenshot agents, sessions, kanban, search.

## Test Gate

Test leaves dispatched in Phase 0, validated by the control agent
(orchestrator, in-session).

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F3 | all agent types shown | 01-agents-data.md, 02-agents-columns.md | agents_panel render test | tui-flows | PENDING |
| F4 | type + active columns | 02-agents-columns.md T1-T2 | agents_panel column test | tui-flows | PENDING |
| F5 | project tree + search | 03-sessions-tree.md, 05-search-entry.md | sessions tree test | tui-flows | PENDING |
| F6 | clean kanban error | 04-kanban-error.md | kanban view error test | e2e kanban | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. Q1: Does the sessions tree replace the flat table outright or add a toggle?
   Recommendation: replace it with grouped rows; the user asked for tree
   behavior, not a second view mode. Impact: simpler key handling.
## Notes

Two files are hot: agents_panel.go (leaves 01 and 02) and sessions.go (leaves 03 and 05). Serialize within each pair; do not dispatch 01+02 or 03+05 concurrently.
