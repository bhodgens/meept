# 03 Sessions Tree - Implementation Leaf

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
- **Scope:** Project-grouped expandable sessions tree
- **Dependencies:** 02-tui/00T-tui-tests.md; C2
- **Estimated Context:** 55K
- **Concurrency Group:** B3
- **Audit references:** none

## Goal

Group sessions into C2 buckets and render a flat table with group header rows. Left/right arrow keys collapse/expand a group. Keep the detail pane working for the selected session. Serialize with leaf 05 on sessions.go.

## Context

Go. `types.Session` carries ProjectID and ProjectPath. Build a derived row slice; header rows are non-selectable. Use tableutil.SetRows.

## Verified Anchors

- internal/tui/types/types.go:251-252 - ProjectID / ProjectPath
- internal/tui/models/sessions.go:82-108 - columns and model
- internal/tui/models/sessions.go:508 - current key hint line

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
expanded/collapsed grouped session rows via C2
```

### What This Leaf Consumes

- C2 bucketFor - owner: this leaf (Go side)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Group rows | Modify: internal/tui/models/sessions.go, create: internal/tui/models/sessions_tree.go | `go test -p 2 ./internal/tui/models -run TestSessionTree` | flat rows | grouped rows | Given sessions across 2 projects; When View; Then group headers present |
| T2 | Expand/collapse | Modify: internal/tui/models/sessions.go | same | keys do nothing | left/right toggles | Given a group; When right; Then children shown; When left; Then hidden |

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
