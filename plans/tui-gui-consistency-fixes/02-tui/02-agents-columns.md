# 02 Agents Columns - Implementation Leaf

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
- **Scope:** Add type and active columns to agents table
- **Dependencies:** 02-tui/00T-tui-tests.md; 02-tui/01-agents-data.md; C1
- **Estimated Context:** 45K
- **Concurrency Group:** B2
- **Audit references:** none

## Goal

Add `type` and `active` columns to the agents table. Render agent type from C1 Type and active count from C1 ActiveCount. Keep the existing cost column. Fit columns via tableutil so narrow viewports stay readable.

## Context

Go. Follow the exec-column pattern for conditional columns and the tableutil sizing invariant. Serialize with leaf 01 on agents_panel.go.

## Verified Anchors

- internal/tui/agents_panel.go:189-204 - column definitions
- internal/tui/agents_panel.go:184-188 - exec column conditional pattern
- internal/tui/agents_panel.go:923 - cost formatting

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
agents table with id/type/status/active/tier/drift/cost/findings/last-run
```

### What This Leaf Consumes

- C1 RosterEntry - owner: 01-backend/01-roster-rpc.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add columns | Modify: internal/tui/agents_panel.go | `go test -p 2 ./internal/tui -run TestAgentsRoster` | headers absent | headers present | Given panel; When View; Then type and active headers | Given a narrow viewport; When View; Then all columns fit |
| T2 | Render cells | Modify: internal/tui/agents_panel.go | same | cells blank | type and count cells present | Given 3 types; When View; Then each row shows type and count |

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
