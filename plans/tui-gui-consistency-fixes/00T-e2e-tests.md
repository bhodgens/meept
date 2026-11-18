# 00T E2E Tests - Implementation Leaf

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

- **Parent:** ../../master.md
- **Scope:** Cross-branch e2e coverage for the entire tree
- **Dependencies:** none
- **Estimated Context:** 32K
- **Concurrency Group:** Z
- **Audit references:** none

## Goal

Write hermetic e2e scenarios that exercise the cross-branch contracts: agents.roster returns all three agent types with active counts and cost; the kanban default boot answers kanban.board.list; TUI and GUI search entry points reach the existing search implementations.

## Context

Go e2e harness with build tag `e2e`. See `e2e/suites/kanban/kanban_test.go` and `e2e/manifest.json` for suite shape. New suite: `e2e/suites/tui-gui-consistency/`.

## Verified Anchors

- e2e/manifest.json - suite registration and path_map
- e2e/suites/kanban/kanban_test.go:552-594 - rpcResult and disabled-boot pin
- internal/tui/models/search.go:1-60 - OpenSearchViewMsg contract

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
hermetic e2e suite for tree contracts
```

### What This Leaf Consumes

- `SearchRPCClient` interface (existing, internal/tui/models/search.go)
- C1 RosterEntry JSON keys (root master.md)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add e2e suite + manifest | Create: e2e/suites/tui-gui-consistency/consistency_test.go; Modify: e2e/manifest.json | `go test -p 2 -tags e2e ./e2e/suites/tui-gui-consistency/...` | suite missing | suite runs RED on unimplemented RPC | Given a scratch daemon; When roster RPC called; Then all 3 types present |
| T2 | Roster fields pin | Modify: e2e/suites/tui-gui-consistency/consistency_test.go | same | fields absent | active_count + daily_cost_cents present | Given seeded agents/bots/employees; When roster; Then counts and cost nonzero |
| T3 | Kanban default pin | Modify: e2e/suites/tui-gui-consistency/consistency_test.go | same | method not found on default boot | default boot answers list | Given default config; When kanban.board.list; Then boards array returned |

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
