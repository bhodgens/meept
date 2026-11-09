# jobs.state CHECK + BEGIN IMMEDIATE alignment - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** F7 (jobs.state CHECK constraint, fresh + migrated schema) and F6 (comment-vs-code BEGIN IMMEDIATE drift in FailAttempt/Retry).
- **Dependencies:** 00T-queue-tests.md
- **Estimated Context:** 30K
- **Concurrency Group:** B
- **Audit references:** F6, F7, audit 2026-11-09

## Goal

The DB pins the state enum like effects_ledger does; transactions the
comments claim are IMMEDIATE actually are.

## Context

- internal/queue/store.go:97 (schema), :69 SetMaxOpenConns(1) - current de-facto serializer
- internal/queue/store.go:793-797, :990-994 - comments claiming BEGIN IMMEDIATE over plain Begin()
- Pattern source: internal/effects/ledger_sqlite.go:8-10 (CHECK constraint) and the repo's `_pragma=` DSN rule (audit-sqlite-dsn-pragmas.py)

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Schema migration: jobs.state gains CHECK (state IN ('pending','claimed','processing','completed','failed','dead'))
// Fresh schema + existing-DB migration path (table rebuild or guarded ALTER where supported).
// FailAttempt/Retry use BeginTx with BEGIN IMMEDIATE matching their comments.
```

### What This Leaf Consumes

Repo migration conventions (survey scripts/ and internal/migrations or the store's own migration pattern - re-grep before writing).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | CHECK in fresh schema | Modify: internal/queue/store.go schema const | TestJobsStateCheckRejectsGarbage GREEN | RED | GREEN | Given fresh DB; When INSERT garbage state; Then constraint error |
| T2 | CHECK + partial-index invariants | Modify: store open/migrate path | `go test -p 2 ./internal/queue/ -run TestJobsStateCheckMigrated` (new) | new | GREEN: open a pre-CHECK DB fixture -> CHECK enforced after migrate; PARTIAL UNIQUE INDEX added: at most one non-terminal (pending/claimed/processing) row per natural job key (survey the unique index that Claim uses at dispatch — if jobs are per-id singletons, index targets the dedup/idempotency key instead; if no natural key exists, SKIP with a note, do not invent one) | Given legacy DB without CHECK; When store opens; Then constraint active, existing rows untouched; Given two concurrent claims of same-key jobs; Then only one non-terminal row exists |
| T3 | BEGIN IMMEDIATE | Modify: FailAttempt (:793 region), Retry (:990 region) | `go test -p 2 ./internal/queue/` | - | GREEN; code uses the repo's immediate-tx idiom; comments now true | Given the two call sites; When tx begins; Then BEGIN IMMEDIATE issued |

## Self-Verification Checklist

- [ ] Gate test GREEN, untouched
- [ ] Legacy-DB open path tested, not assumed
- [ ] audit-sqlite-dsn-pragmas.py passes (`python3 scripts/audit-sqlite-dsn-pragmas.py` or make lint-ci scoped)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] CHECK list matches JobState enum exactly (6 values)
- [ ] Migration additive; no data loss on existing rows
- [ ] IMMEDIATE idiom matches repo standard (grep other uses)

## Notes

If the store has no migration mechanism, add the minimal one (PRAGMA
table-info probe + rebuild) - do NOT import a migration framework.
