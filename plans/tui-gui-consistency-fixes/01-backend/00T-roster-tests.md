# 00T Roster Tests - Implementation Leaf

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
- **Scope:** RED tests for the unified roster RPC
- **Dependencies:** none
- **Estimated Context:** 26K
- **Concurrency Group:** A0
- **Audit references:** none

## Goal

Write unit tests that specify the C1 roster row shape and the merge of registry agents, bots, and employees. Tests must fail RED because the handler does not exist yet.

## Context

Go unit tests in `internal/rpc/` with injected fake sources. No implementation.

## Verified Anchors

- internal/employee/handler.go:308-321 - current employees-only list
- internal/agent/registry.go:533-542 - ListSpecs
- internal/bot/lifecycle.go:64 - ListBots
- internal/tui/agents_panel.go:100-133 - current row shape

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
test file for roster handler
```

### What This Leaf Consumes

- C1 RosterEntry (root master.md) - exact JSON keys

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Roster type keys | Create: internal/rpc/roster_test.go | `go test ./internal/rpc -run TestRoster` | handler undefined | all C1 keys decoded | Given JSON with C1 keys; When unmarshaled; Then fields match |
| T2 | Merge assertion | Modify: internal/rpc/roster_test.go | same | merge not implemented | 3 types present | Given one source each; When list; Then 3 rows |

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
