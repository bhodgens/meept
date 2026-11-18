# Flutter GUI fixes - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test leaf, 4 implementation leaves
- **Scope:** Flutter GUI fixes

## Goal

Make the session popup full-window and reuse canonical tabs so plans and tasks populate; add agent active/cost/type display; render sessions as a project-grouped expandable tree with a search button that opens the existing SearchPanel scoped to sessions.

## Architecture

The popup currently duplicates four tab bodies with broken filters. The fix deletes the duplicates and embeds `SessionsOverviewTab`, `PlansTab`, `TasksTab`, and `AgentsTab`. The tree uses a new shared Dart grouping helper (C2). Search reuses `SearchPanel` (C4).

## Interface Contracts

### C1 (consumer)

`agents.roster -> {"agents": []RosterEntry}` - owner: 01-backend/01-roster-rpc.md

### C2: Dart bucket helper

`bucketFor(Session s) = s.projectId ?? s.projectPath ?? "unbound"` - owner: 03-gui/03-sessions-tree.md

### C4: SearchPanel

`SearchPanel(scope: SearchScope.sessions)` - owner: existing code

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gui-tests.md | leaf | none | 28K | C0 |
| 01 | 01-popup-shell.md | leaf | 00T | 45K | C1 |
| 02 | 02-popup-tabs.md | leaf | 00T, 01, C1 | 50K | C2 |
| 03 | 03-sessions-tree.md | leaf | 00T, C2 | 60K | C3 |
| 04 | 04-search-entry.md | leaf | 00T, 03, C4 | 30K | C4 |

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

- **Language:** Dart / Flutter / Riverpod
- **UI text:** all lowercase
- **Web:** no top-level dart:io in shared code
- **Formatting:** `dart format` before reporting
- **Tests:** widget tests with ProviderScope + SdkApiClient stub subclass
- **Commits:** orchestrator only

## Requirement Traceability

See root `../master.md` Requirement Traceability. This branch covers R1-R14
as listed in each leaf's Goal.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T | PENDING | 0 | |
| 01 | PENDING | 0 | |
| 02 | PENDING | 0 | |
| 03 | PENDING | 0 | |
| 04 | PENDING | 0 | |


Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until this branch's
test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

`flutter analyze lib/ test/` zero errors under lib; `flutter test` green; live GUI open popup full window, verify tabs, tree, search button.

## Test Gate

Test leaves dispatched in Phase 0, validated by the control agent
(orchestrator, in-session).

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F7 | popup fullscreen | 01-popup-shell.md T1 | widget test | gui-flows | PENDING |
| F8 | tabs reuse canonical widgets | 02-popup-tabs.md T1 | widget test | gui-flows | PENDING |
| F9 | agent active/cost/type | 02-popup-tabs.md T2 | widget test | gui-flows | PENDING |
| F10 | project tree + search | 03-sessions-tree.md, 04-search-entry.md | widget test | gui-flows | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. Q1: Should the popup use `Dialog.fullscreen` or a zero-inset sized dialog?
   Recommendation: `Dialog.fullscreen`. Impact: consistent route semantics and
   no manual width/height math.
## Notes

session_info_overlay.dart, sessions_list.dart, api_models.dart are hot files. Serialize popup leaves 01+02 and tree leaves 03+04.
