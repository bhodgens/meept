# 04 Kanban Error - Implementation Leaf

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
- **Scope:** Clean kanban disabled guidance
- **Dependencies:** 02-tui/00T-tui-tests.md; C3
- **Estimated Context:** 25K
- **Concurrency Group:** B4
- **Audit references:** none

## Goal

Detect the method-not-found error for `kanban.board.list` and render C3 instead of the raw RPC error. Do not change any kanban RPC call or config default (that is branch 04).

## Context

Go. Error flows into KanbanPanel.err at kanban_view.go:318 and renders at kanban_view_render.go:36-40.

## Verified Anchors

- internal/tui/kanban_view.go:314-319 - fetch error path
- internal/tui/kanban_view_render.go:36-40 - raw error render

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
C3 guidance string rendered for method-not-found
```

### What This Leaf Consumes

- C3 kanbanDisabledHint - owner: this leaf

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Error detection | Modify: internal/tui/kanban_view.go | `go test -p 2 ./internal/tui -run TestKanbanDisabled` | raw error | hint string | Given method-not-found; When render; Then C3 present |

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
