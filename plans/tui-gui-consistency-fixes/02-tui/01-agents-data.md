# 01 Agents Data - Implementation Leaf

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
- **Scope:** Switch agents panel to agents.roster data
- **Dependencies:** 02-tui/00T-tui-tests.md; C1
- **Estimated Context:** 40K
- **Concurrency Group:** B1
- **Audit references:** none

## Goal

Replace the `agents.list` call in the agents panel with `agents.roster` and decode C1 RosterEntry rows into AgentSummary. Preserve quota state handling and all existing columns. This leaf changes data source only; column additions are leaf 02.

## Context

Go. Keep AgentSummary as the panel's row model; map C1 fields onto it.

## Verified Anchors

- internal/tui/agents_panel.go:401-420 - fetchMsg and agents.list call
- internal/tui/agents_panel.go:100-133 - AgentSummary fields
- internal/tui/agents_panel.go:97-99 - comment about employees-only

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
agents panel fetches C1 roster rows
```

### What This Leaf Consumes

- C1 RosterEntry - owner: 01-backend/01-roster-rpc.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Switch RPC method | Modify: internal/tui/agents_panel.go | `go test -p 2 ./internal/tui -run TestAgentsRoster` | old method called | roster called | Given panel Init; When fetch; Then agents.roster requested | Given a client; When Init; Then roster rows decoded |

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
