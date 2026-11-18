# 00T Tui Tests - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. If this branch runs a tests-first
> gate, the failing tests ALREADY EXIST and are committed - your job is to
> make them pass; do NOT delete, skip, weaken, or rewrite any gate test
> (report suspected-wrong gate tests to the orchestrator instead). Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** RED TUI model tests for roster, tree, and kanban error
- **Dependencies:** none
- **Estimated Context:** 32K
- **Concurrency Group:** B0
- **Audit references:** none

## Goal

Write model-level render tests that fail RED for: agents panel showing three types with type/active/cost columns; sessions model grouping by project with expand/collapse; kanban view showing the C3 guidance string on method-not-found. Tests only, no implementation.

## Context

Go tests in internal/tui with injected fake RPC clients. Use the existing render-test pattern: assert View() contains cell text, not just row counts.

## Verified Anchors

- internal/tui/agents_panel_render_test.go:205-231 - column assertion pattern
- internal/tui/models/sessions_test.go - sessions model tests
- internal/tui/kanban_view_test.go - kanban view tests
- internal/tui/tableutil - sizing helpers

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
test files for TUI changes
```

### What This Leaf Consumes

- C1, C2, C3 (root master.md and this branch)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | roster render tests | Create: internal/tui/agents_roster_test.go | `go test -p 2 ./internal/tui -run TestAgentsRoster` | cells absent | three types + columns visible | Given C1 rows; When View; Then cells present |
| T2 | sessions tree tests | Create: internal/tui/models/sessions_tree_test.go | `go test -p 2 ./internal/tui/models -run TestSessionTree` | tree absent | grouped rows and expansion | Given 2 projects; When View; Then groups visible |
| T3 | kanban error test | Create: internal/tui/kanban_disabled_test.go | `go test -p 2 ./internal/tui -run TestKanbanDisabled` | raw error shown | C3 string shown | Given method-not-found error; When View; Then C3 present |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

None.
