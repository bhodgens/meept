# 01 Roster Rpc - Implementation Leaf

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
- **Scope:** Implement agents.roster handler and registration
- **Dependencies:** 01-backend/00T-roster-tests.md; C1
- **Estimated Context:** 55K
- **Concurrency Group:** A1
- **Audit references:** none

## Goal

Add the RosterEntry type and an `agents.roster` RPC handler that merges AgentRegistry.ListSpecs, bot.Manager.ListBots, and employee.Manager.ListEmployees into C1 rows. Register it on the socket path. Confirm route ownership first (see branch notes).

## Context

Go RPC package. The daemon wires dependencies into internal/rpc. Follow the existing handler pattern in internal/employee/handler.go.

## Verified Anchors

- internal/employee/handler.go:308-321 - handleList pattern
- internal/agent/registry.go:533-542 - ListSpecs
- internal/bot/lifecycle.go:64 - ListBots
- internal/daemon/components.go:5289 - lister wiring seam
- internal/rpc/kanban.go:140 - RPC registration pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
```
// Pinned by the committed gate test internal/rpc/roster_test.go.
// There is no generic Handler type in internal/rpc; use RosterHandler.
type RosterDeps struct {
    ListAgents    func() []RosterEntry
    ListBots      func() []RosterEntry
    ListEmployees func() []RosterEntry
}
func NewRosterHandler(deps RosterDeps) *RosterHandler
func (h *RosterHandler) handleRoster(ctx context.Context, raw json.RawMessage) (any, error)
// returns map[string]any{"agents": []RosterEntry}
```
```

### What This Leaf Consumes

- C1 RosterEntry - defined in this leaf, owner of the type

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add RosterEntry type | Modify: internal/rpc/roster.go (create; gate: internal/rpc/roster_test.go) | `go test -p 2 ./internal/rpc/... -run TestRoster` | undefined type | type compiles | Given C1 keys; When marshaled; Then exact JSON | Given a row; When encoded; Then keys match C1 |
| T2 | Add handler + registration | Modify: internal/rpc/roster.go, internal/rpc/registry.go | same | method not found | RPC answers rows | Given seeded sources; When call; Then 3 rows | Given a client; When agents.roster; Then agents array |

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
