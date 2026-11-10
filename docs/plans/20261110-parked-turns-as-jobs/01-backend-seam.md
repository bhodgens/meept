# Backend Seam (Wrap Today's Store) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Do NOT commit. No read_file on existing sources; grep/cat. Write once. NEVER edit park_owner_gate_test.go or the 20261109 frozen gates (byte-identical required).

## Meta

- **Parent:** ../master.md
- **Scope:** internal/agent: extract today's SQLite-park behavior behind the C-A1 `parkBackend` interface; TurnParker unchanged otherwise.
- **Dependencies:** none
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **Audit references:** C1 (tree 20261109), design amendment 3

## Goal

TurnParker stores via `parkBackend`, default impl = sqliteParkBackend
(wraps SQLiteParkStore exactly). Behavior byte-identical with the flag
unset; existing 12 park gates stay green UNTOUCHED — they are this leaf's
regression net.

## Context

- internal/agent/parked_turn.go: SetPersistence/SetPersistenceOwner + LoadOwned seam (C-K1, commit bb24843f); drainDue/delete-before-callback — do NOT touch
- internal/agent/park_store_sqlite.go: SQLiteParkStore (Save/Delete/Load/prune/migrate owner col)

## Verified Anchors

Re-grep at dispatch: parked_turn.go parkBackend insertion points = every
`p.persist` call site + LoadOwned use in reArm.

## Interface Contracts (From Parent)

### What This Leaf Exposes
```
C-A1: type parkBackend interface { Save(ctx, rec ParkedTurnRecord) error; Delete(ctx, kind, key) error; LoadOwned(ctx, kind, owner) ([]ParkedTurnRecord, error); Arm(ctx) }
func (p *TurnParker) SetBackend(b parkBackend)   // nil-safe: keeps sqlite default
type sqliteParkBackend struct{ ... }             // today's exact semantics, no logic change
```

### What This Leaf Consumes
- Existing SQLiteParkStore unchanged.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Interface + default impl | Modify: internal/agent/parked_turn.go; new internal/agent/park_backend.go | `go test -p 2 -count=1 ./internal/agent/ -run 'TestLoadReturnsOnlyOwnedRows|TestRestartRearmSingleOwner'` | - | GREEN (gates unchanged) | Given parker w/o SetBackend; When Park/Load; Then sqlite backend behavior identical |
| T2 | Arm no-op + call-site swap | Modify: parked_turn.go | full internal/agent pkg | - | GREEN | - |
| T3 | internal/agent/AGENTS.md line: parkBackend seam | Modify: AGENTS.md | grep | - | documented | - |

## Self-Verification Checklist

- [ ] Full internal/agent suite green; frozen gates zero-diff
- [ ] go build ./... green
- [ ] No drain/resume semantics changed

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tails; deviations.

## Review Checklist (For Review Agent)

- [ ] Wrapper adds no behavior (diff read)
- [ ] SetBackend nil-safe

## Notes

This leaf is pure refactor insurance: it makes leaf 02 additive instead of
invasive.
