# 02-memory-origin-column — implementation leaf (branch 02, contract C3)

> **For the implementing agent:** TDD; gate tests exist (origin scenario F4). Do NOT weaken
> gate tests. Do NOT commit / git add.

## Meta

- **Parent:** orchestrator.md
- **Scope:** C3 - memory_facts origin column + MemoryFact.Origin + Upsert stamping
- **Dependencies:** 00T-e2e-tests (TESTS_VALIDATED)
- **Estimated Context:** 30K
- **GitHub Issue:** https://github.com/bhodgens/meept/issues/104
- **Concurrency Group:** B

## Goal

memory_facts gains an additive `origin TEXT NOT NULL DEFAULT 'unknown'` column; the Go
MemoryFact carries it and Upsert stamps it. Legacy rows read 'unknown' - no data loss, no
backfill rewrite (that is the doctor's job, branch 03).

## Context

- Store: internal/memory/fact_store.go (NewFactStore :84, CREATE TABLE :56, Upsert :130,
 INSERT :161). The memory fact tool surface (internal/tools/builtin/memory.go) writes via
 this store.

## Verified Anchors

- internal/memory/fact_store.go:56 - CREATE TABLE IF NOT EXISTS memory_facts (...)
- internal/memory/fact_store.go:130 - func (s *FactStore) Upsert(ctx, f MemoryFact) error
- internal/memory/fact_store.go:161 - INSERT INTO memory_facts
- internal/memory/fact.go (MemoryFact struct)

## Interface Contracts (From Parent)

### What This Leaf Exposes (C3, exact)

```
// memory_facts DDL: add  origin TEXT NOT NULL DEFAULT 'unknown'
type MemoryFact struct { ...; Origin string }
func (s *FactStore) Upsert(ctx context.Context, f MemoryFact) error // stamps f.Origin; empty => 'unknown'
```

### What This Leaf Consumes

- none (C3 is self-contained; branch 03 reads the column).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Additive migration: origin column NOT NULL DEFAULT 'unknown'; existing rows untouched | Modify: internal/memory/fact_store.go (DDL) | gate F4 | RED - column missing | GREEN | a pre-existing row reads 'unknown'; schema is upgraded in place |
| T2 | MemoryFact.Origin + Upsert INSERT includes it (empty -> 'unknown') | Modify: internal/memory/fact.go, internal/memory/fact_store.go | gate | RED | GREEN | new upsert carries origin; empty becomes 'unknown' |
| T3 | Stamp at the tool write path (memory.go fact tools set Origin='agent'; any CLI/manual = user) | Modify: internal/tools/builtin/memory.go (write path) | gate | RED | GREEN | a fact written by the tool has origin 'agent' |

## Self-Verification Checklist

- [ ] C3 exact; migration is additive (no DROP/rewrite, NOT NULL DEFAULT)
- [ ] legacy + new rows both readable; empty origin normalizes to 'unknown'
- [ ] gofmt; DO NOT COMMIT

## Review Checklist (For Review Agent)

- [ ] C3 exact; no data loss in migration (assert on a seeded legacy row)
- [ ] every memory write path stamps an origin
- [ ] e2e F4 GREEN

## Notes

- The ALTER must be idempotent (the store already uses CREATE TABLE IF NOT EXISTS patterns;
 add the column only when absent - check the migration approach used by the store).
