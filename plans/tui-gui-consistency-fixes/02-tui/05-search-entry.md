# 05 Search Entry - Implementation Leaf

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
- **Scope:** Search entry on the sessions tree
- **Dependencies:** 02-tui/00T-tui-tests.md; 02-tui/03-sessions-tree.md; C4
- **Estimated Context:** 30K
- **Concurrency Group:** B5
- **Audit references:** none

## Goal

Add an `f` keybinding on the sessions tree that emits the existing OpenSearchViewMsg with sessions scope. Reuse `internal/tui/models/search.go`. Do not write a new search model. Serialize with leaf 03 on sessions.go.

## Context

Go. The sessions model already has an `f: search` hint at line 508 and currently opens global search; narrow it to sessions scope via C4.

## Verified Anchors

- internal/tui/models/sessions.go:256 - OpenSearchViewMsg usage
- internal/tui/models/sessions.go:508 - key hint
- internal/tui/models/search.go:18-21 - OpenSearchViewMsg
- internal/tui/models/search.go:88 - scopes list

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
sessions scope search entry wired
```

### What This Leaf Consumes

- C4 OpenSearchViewMsg - owner: existing code, reuse only

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Wire search key | Modify: internal/tui/models/sessions.go | `go test -p 2 ./internal/tui/models -run TestSessionSearchEntry` | no message | OpenSearchViewMsg emitted | Given tree focused; When f; Then search opens scoped to sessions |

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
