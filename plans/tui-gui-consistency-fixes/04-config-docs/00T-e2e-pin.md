# 00T E2E Pin - Implementation Leaf

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
- **Scope:** RED e2e pin for kanban default flip
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** D0
- **Audit references:** none

## Goal

Write (or rewrite) the e2e scenario that will fail RED until `[kanban].enabled` defaults true: default boot answers `kanban.board.list`; explicit disabled boot answers method-not-found. Tests only, no implementation.

## Context

Go e2e in e2e/suites/kanban/. The current pin at lines 579-594 asserts the opposite; rewrite it to start a daemon with an explicit disabled config.

## Verified Anchors

- e2e/suites/kanban/kanban_test.go:552-594 - current pin
- e2e/suites/kanban/helpers_test.go:126 - rpcResult helper

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
rewritten e2e test for default-on posture
```

### What This Leaf Consumes

- C5 kanban default - owner: this leaf's target

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Default-on pin | Modify: e2e/suites/kanban/kanban_test.go | `go test -p 2 -tags e2e ./e2e/suites/kanban -run TestKanbanDefault` | default boot refuses | default boot answers | Given default config; When list; Then boards returned | Given explicit disabled config; When list; Then method-not-found |

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
