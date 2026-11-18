# 02 Roster Active Cost - Implementation Leaf

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
- **Scope:** Populate active_count and daily_cost_cents
- **Dependencies:** 01-backend/00T-roster-tests.md; 01-backend/01-roster-rpc.md
- **Estimated Context:** 45K
- **Concurrency Group:** A2
- **Audit references:** none

## Goal

Fill C1 active_count from live registry queue registration and daily_cost_cents from bot state and available per-agent metrics. Never invent cost data; leave zero when a source has none.

## Context

Go. Active loops are keyed per conversation in AgentRegistry. Bot state carries TodayCostCents. Reuse existing accessors; add a small seam if none exists.

## Verified Anchors

- internal/agent/registry.go:2011-2064 - RegisterActiveQueue / GetActiveQueue
- internal/bot/types.go:155-169 - BotState.TodayCostCents
- internal/tui/agents_panel.go:112 - DailyCostCents precedent

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
populated C1 fields on the roster handler
```

### What This Leaf Consumes

- C1 RosterEntry - owner: 01-roster-rpc.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Compute active counts | Modify: internal/rpc/roster.go | `go test -p 2 ./internal/rpc/... -run TestRoster` | active_count always 0 | count matches live loops | Given 3 live loops; When roster; Then active_count=3 | Given active queues; When list; Then count correct |
| T2 | Cost mapping | Modify: internal/rpc/roster.go | same | cost always 0 | bot cost surfaced | Given bot TodayCostCents=123; When roster; Then daily_cost_cents=123 | Given bot state; When list; Then cost present |

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
