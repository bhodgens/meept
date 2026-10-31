# 01-permissions: Engine Absorption Branch - Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Dispatch implementation agents, review in-session (main model, never a
> delegated reviewer), re-dispatch on gaps (max 3 cycles), commit after review.
> Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 4 (1 test-gate leaf, 3 implementation leaves)
- **Scope:** Absorb the two surviving ideas of the never-wired
  internal/security.Engine into the live pkg/security.PermissionChecker path
  (fence-as-stage, decision audit lines), add time-boxed override support to
  the resolver layer, then DELETE the dead engine package. Resolves issue #77
  (decision D3).

## Goal

One permission engine, strictly more capable than today's: fence enforced
centrally (no per-tool opt-in to forget), every tool-call decision logged with
its rule source, overrides that can time-box a grant, and no frozen second
engine in the tree to mis-wire against.

Sequencing is STRICT: all three leaves touch pkg/security/ and/or
internal/daemon/components.go - dispatch 01, review, commit; then 02; then 03.

## Interface Contracts (consumed from master.md)

- C1: FenceStage interface + SetFence on PermissionChecker (owner: leaf 01)
- C2: Decision audit slog line shape (owner: leaf 02)
- C8 overrides are consumed by leaf 04-tool-gates; this branch only delivers
  the override READER + types (writer/CLI is 07-overrides.md, outside branch).

## Child Document Index

| # | Document | Type | Dependencies | Est. Context |
|---|----------|------|--------------|--------------|
| 00T | 00T-tests.md | leaf (gate) | none | 45K |
| 01 | 01-fence-stage.md | leaf | 00T | 60K |
| 02 | 02-decision-audit.md | leaf | 01 | 45K |
| 03 | 03-delete-engine.md | leaf | 01, 02 | 40K |

Dispatch: 00T -> 01 -> 02 -> 03 (strict serialization).

## Dispatch Protocol

Batch shape: `delegate_task` with tasks=[{goal: "Implement all tasks from
<leaf path>", context: <full leaf text + contracts C1/C2 + conventions +
inlined source + no-commit/no-git-add/no-read_file/write-once-and-stop rules>}].
Dispatch ONE leaf at a time (strict serialization - shared files). After each
return: review in-session per parent Review Checklist, run the leaf's verify
commands, commit with explicit pathspecs + single-line message, update the
tracking table. Re-dispatch on gaps, max 3 cycles, then escalate to the root.

## Test Gate

| # | Feature | Unit/E2E | Status |
|---|---------|----------|--------|
| F1 | Fence denies via central stage for a tool with NO per-tool opt-in | branch e2e member (parent 00T suite) | PENDING |
| F2 | Nil fence = legacy behavior unchanged | unit pin (bug-fix class) | PENDING |
| F3 | Decision line carries rule_source for each short-circuit stage | branch e2e member (log capture) | PENDING |
| F4 | Engine deleted; `go build ./...` clean; no dangling references | integration (leaf 03 verify) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

## Integration Test Plan

1. After leaf 02: `go test -p 2 ./pkg/security/... ./internal/agent/...` green.
2. After leaf 03: `go build ./...` clean; grep -r "internal/security" over
   buildable packages returns no imports; parent suites green.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests.md | PENDING | 0 | |
| 01-fence-stage.md | PENDING | 0 | |
| 02-decision-audit.md | PENDING | 0 | |
| 03-delete-engine.md | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Review Checklist (per child, in-session)

- [ ] Leaf tasks implemented at exact paths; contracts C1/C2 satisfied
- [ ] Gate tests GREEN and unmodified
- [ ] gofmt clean; no ignored errors; typed-nil guards on Set* seams
- [ ] No debug artifacts; no line-number corruption
- [ ] Leaf 03: engine package fully removed, docs updated in same commit

## Coding Conventions

See parent master.md Coding Conventions. Additional for this branch: the
FenceStage interface MUST NOT import internal/ (import-cycle direction: daemon
adapts its FenceChecker to the interface; pkg/security stays leaf-clean).
