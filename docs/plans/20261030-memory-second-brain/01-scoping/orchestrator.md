# Memory Scoping - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 (00T-tests gate, 01-scope-schema, 03-agent-recall-wiring)
- **Scope:** Thread a Scope field through memory storage and recall; bind each agent's widest recallable scope; wire the loop and tools to filter by the session's project/user identity.

## Goal

Memory recall is GLOBAL today: any session recalls every stored memory, so
bench fixtures and other projects' facts leak into chat (proven defect class).
This branch makes memory scoped - per-project by default, per-user when
multiuser is on, global only when the writer widened it or the agent is
authorized to span projects (chief-of-staff pattern, user decision D2).

Legacy rows backfill as ScopeGlobal: nothing existing disappears from recall;
isolation grows as new scoped writes accumulate.

## Architecture

C1 (master.md) adds `memory.Scope` + a `scope` column/metadata key with
default 'global'. C2 adds `WithScopeFilter(maxScope, projectID, userID)` as an
additive variadic option on Manager.Search/GetContext. C3 adds
`MemoryMaxScope` to the agent spec and resolves the filter inputs from the
session (project binding) at recall time; the memory tools stamp writes with
the session's scope unless the caller explicitly widens within maxScope.

internal/memory is ONE package: leaves 01 and 03 both touch store files -
NEVER dispatch them concurrently. The 00T gate leaf provides RED unit pins
(unit tests are the bug-fix/bootstrap exception here: this is new-package
bootstrap plumbing asserted at store level; the live e2e surface already
exists in the parent gate's memory-scoping suite).

## Interface Contracts

(All defined once in ../master.md: C1 Scope, C2 WithScopeFilter,
C3 MemoryMaxScope. This branch's children consume them verbatim.)

Additional intra-branch contract:

### B1: Scope filter inputs resolution

```
// Owner: 03-agent-recall-wiring.md
// resolveScopeInputs(session) -> (maxScope Scope, projectID, userID string)
// Precedence: agent MemoryMaxScope (empty=project) ; projectID = session's
// bound project (DetectionContext.CWD-resolved project id; "" if unbound);
// userID = multiuser identity ("" in single-user). Unbound session +
// maxScope=project => filter matches NOTHING project-scoped (global + own-
// user still recallable). Never fall back to the daemon's CWD.
// Consumers: 00T-tests (pins), master 00T e2e (live surface)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-tests.md | leaf (test gate) | none | 45K | T |
| 01 | 01-scope-schema.md | leaf | 00T | 70K | A |
| 03 | 03-agent-recall-wiring.md | leaf | 01 | 65K | B |

Sequential by package contention: 01 then 03 (both edit internal/memory).

## Dispatch Protocol

For each child, in order. Full recipe: skill `hierarchical-plan-execution`.

### Phase 0: Test Gate

Dispatch 00T-tests.md: RED store-level pin tests for C1/C2/B1 shapes.
Validate in-session against the matrix, commit gate tests, set
TESTS_VALIDATED. (Unit pins allowed: store-level plumbing, not a user-facing
feature - the feature e2e lives in the parent gate.)

### Phase 1: Dispatch 01-scope-schema.md (concurrency group A, alone)

delegate_task; context = full leaf text + C1/C2 verbatim + conventions from
../master.md + the internal/memory file map (below) INLINED. No-commit rules.

### Phase 2: Review and Commit 01

In-session review (main model). Verify: Scope enum + column + backfill + FTS
schema handling; WithScopeFilter additive; existing tests green
(`go test -p 2 ./internal/memory/...`). Commit with explicit pathspecs.

### Phase 3: Dispatch and Review 03-agent-recall-wiring.md (group B)

Same rules; verify against B1 + C3; run the parent gate's memory-scoping e2e
suite in isolation once this lands (it should flip GREEN here).

### Phase 4: Integration Review

`go build ./...`; `go test -p 2 ./internal/memory/... ./internal/agent/...
./internal/bot/...`; `make graphs` (RPC surface unchanged, but confirm);
set children COMPLETE; report COMPLETE to ../master.md.

## Review Checklist

(Same in-session checklist as ../master.md - all bullets apply to each child.)

## Coding Conventions

(Copy of ../master.md conventions - pass verbatim to every dispatch.)

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R3a | "scoped per project" | 01-scope-schema.md (C1/C2) | 00T pins + memory-scoping e2e |
| R3b | "chief of staff over numerous projects" | 03-agent-recall-wiring.md (C3/B1) | memory-scoping e2e F3 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests.md | PENDING | 0 | |
| 01-scope-schema.md | PENDING | 0 | |
| 03-agent-recall-wiring.md | PENDING | 0 | |

## Integration Test Plan

1. `go build ./...` clean.
2. `go test -p 2 ./internal/memory/... ./internal/agent/... ./internal/bot/...` green.
3. Parent gate suite `memory-scoping` GREEN in isolation.
4. Migration check: boot against a COPY of a legacy memory db (pre-scope);
   all rows recallable (backfill=global); `PRAGMA integrity_check` passes.

## Test Gate

Test leaf: 00T-tests.md.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| S1 | Scope enum + column + global backfill | C1; 01-scope-schema.md T1-T2 | TestScopeBackfill etc. | parent suite memory-scoping F2 | PENDING |
| S2 | WithScopeFilter predicate | C2; 01-scope-schema.md T3 | TestScopeFilterMatrix | parent suite F2/F3 | PENDING |
| S3 | Agent MemoryMaxScope + B1 resolution | C3/B1; 03-agent-recall-wiring.md | TestResolveScopeInputs | parent suite F3 | PENDING |

Delta log: (empty)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Do NOT repurpose ScopedMemoryManager (bot_id tagging) - different mechanism,
  leave intact (../master.md Notes).
- typed-nil rule (internal/daemon/AGENTS.md): any new wiring seam that takes a
  concrete pointer into an interface field needs the explicit nil check +
  nil-dependency test through the REAL wiring function.
- The bench-harness belt-and-braces note (plant bench markers project-scoped)
  is recorded in ../master.md Notes - surface in 03's report, do not block.
