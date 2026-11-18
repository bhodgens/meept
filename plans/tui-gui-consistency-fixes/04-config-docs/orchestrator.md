# Config, docs, and kanban menu - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test leaf, 2 implementation leaves
- **Scope:** Config, docs, and kanban menu

## Goal

Flip `[kanban].enabled` to true in DefaultConfig and the config template; rewrite the disabled-boot e2e pin to use an explicit disabled config; add kanban to the GUI tools menu; update all affected docs and AGENTS.md summaries.

## Architecture

Kanban is deliberately opt-in under `docs/agents/opt-in-defaults.md`. The user explicitly requested default-on. `sidecar_enabled` stays false so boot stays light. The e2e pin must verify disabled behavior with an explicit config, not a default boot.

## Interface Contracts

### C5: kanban default

`DefaultConfig().Kanban.Enabled == true`; `SidecarEnabled == false`; config template uncommented with `"enabled": true`.

### C6: tools menu

`toolRoutePaths['kanban'] == '/kanban'`; `HamburgerMenu._knownTools` contains `kanban`.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-pin.md | leaf | none | 25K | D0 |
| 01 | 01-kanban-default.md | leaf | 00T | 35K | D1 |
| 02 | 02-docs-ag.md | leaf | 00T, 01 | 30K | D2 |

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

- **Languages:** Go, JSON5, Markdown, Dart (menu only)
- **Config:** respect MEEPT_HOME; opt-in docs must change with default
- **Tests:** e2e pin rewrite must use explicit disabled config
- **Commits:** orchestrator only; do not sweep pre-modified generated files

## Requirement Traceability

See root `../master.md` Requirement Traceability. This branch covers R1-R14
as listed in each leaf's Goal.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T | PENDING | 0 | |
| 01 | PENDING | 0 | |
| 02 | PENDING | 0 | |


Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until this branch's
test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

`go test -p 2 ./internal/config/...` then `make e2e-fast-area AREA=kanban` then `flutter analyze lib/` for the menu wiring. Verify the default boot answers kanban.board.list and an explicit disabled boot still refuses it.

## Test Gate

Test leaves dispatched in Phase 0, validated by the control agent
(orchestrator, in-session).

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F11 | kanban default on + pin | 01-kanban-default.md | config default test | e2e kanban | PENDING |
| F12 | kanban in tools menu | 01-kanban-default.md T3 | widget test | gui-flows | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. Q1: Should the e2e disabled-boot pin be deleted after the default flip?
   Recommendation: no. Rewrite it with an explicit `enabled=false` config so
   the opt-out path stays covered. Impact: preserves the C9 contract as an
   opt-out rather than losing it.
## Notes

Two generated files are pre-modified in the working tree; leave them out of all commits. Run `make graphs` only if bus/RPC surfaces change.
