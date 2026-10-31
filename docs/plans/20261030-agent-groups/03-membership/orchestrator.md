# 03-membership: Membership Surfaces + Mutation Gate Branch - Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Dispatch implementation agents, review in-session (main model), re-dispatch
> on gaps (max 3), commit after review. Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 4 (1 test-gate leaf, 3 implementation leaves)
- **Scope:** Group membership on BOTH surfaces (agent frontmatter + user
  store), resolution join, and the agent-update-only mutation gate with the
  file-level backstop (decision D1/D2, contract C5).

## Goal

An agent's effective groups = frontmatter groups ∪ session-owner user groups;
only the agent-update agent or an admin human can change any of it - enforced
at the RPC seam AND at the file-tool layer (backstop), not by convention.

## Interface Contracts

- C4 (master): membership fields + resolution; unknown-group fail-safe.
- C5 (master): RPC methods, write gate, admin config keys, BlockedPaths backstop.
- Issue #76 dependency: user-group resolution under multiuser requires the
  auth-store identity fix; if unfixed, single-user resolution only + docs note.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context |
|---|----------|------|--------------|--------------|
| 00T | 00T-tests.md | leaf (gate) | none | 45K |
| 01 | 01-frontmatter.md | leaf | 02-groups-model (sibling of parent, COMPLETE) | 45K |
| 02 | 02-user-groups.md | leaf | 01 | 45K |
| 03 | 03-mutation-gate.md | leaf | 01, 02 | 60K |

Dispatch: 00T -> 01 -> 02 -> 03 (serialized: shared config/auth files).

## Dispatch Protocol

Batch shape: `delegate_task` with tasks=[{goal: "Implement all tasks from
<leaf path>", context: <full leaf text + contracts C4/C5 + conventions +
inlined source + no-commit/no-git-add/no-read_file/write-once-and-stop rules>}].
Dispatch ONE leaf at a time (serialized - shared config/auth files). After
each return: review in-session per parent Review Checklist, run the leaf's
verify commands, commit with explicit pathspecs + single-line message, update
the tracking table. Re-dispatch on gaps, max 3 cycles, then escalate.

## Test Gate

| # | Feature | Test | Status |
|---|---------|------|--------|
| F1 | Frontmatter groups parse through the 5-step field pipeline (AgentMetadata tag, parse lane, AgentSpec, definitionToSpec, mergeSpec) | branch e2e member + unit pin on mergeSpec | PENDING |
| F2 | Unknown group name: Warn + ignored (fail-safe), reported by groups check | unit pin | PENDING |
| F3 | users.json5 groups field round-trips | unit pin | PENDING |
| F4 | Non-privileged agent mutation: denied at RPC AND file backstop denies file_write/shell on config paths | parent 00T suite member | PENDING |

Statuses: PENDING | DELTA | VALIDATED

## Integration Test Plan

`go test -p 2 ./internal/agents/... ./internal/agent/... ./internal/auth/...
./internal/rpc/...` green after each leaf; parent 00T member suites green.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests.md | PENDING | 0 | |
| 01-frontmatter.md | PENDING | 0 | |
| 02-user-groups.md | PENDING | 0 | |
| 03-mutation-gate.md | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Review Checklist (per child, in-session)

- [ ] Contracts C4/C5 satisfied exactly; 5-step pipeline complete (missing
      step 2 or 5 = silent field loss - check BOTH)
- [ ] Gate tests GREEN and unmodified
- [ ] No identity derived from raw key strings (issue #76 class)
- [ ] gofmt clean; no debug artifacts

## Coding Conventions

See parent master.md. Additional: AGENT.md frontmatter tags use the
underscore form (`groups`) as primary; no camelCase alias lane needed (add one
only if an existing catalog file uses camelCase for a sibling field).
