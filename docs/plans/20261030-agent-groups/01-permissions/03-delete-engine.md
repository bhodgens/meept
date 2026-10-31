# 03: Delete the Dead Engine - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Remove internal/security (the never-wired Engine package) after
  its two ideas are absorbed (leaves 01+02). Resolves issue #77.
- **Dependencies:** 01 (REVIEWED), 02 (REVIEWED)
- **Estimated Context:** 40K
- **Concurrency Group:** A (serialized, last)

## Goal

One permission system remains. The tree must build and test green with the
package gone, and no doc/reference still points at it as live machinery.

## Verified Anchors

- Package to delete: internal/security/ - BUT grep FIRST: FenceChecker and
  any other types may live there that pkg/security or tools still import.
  MOVE survivors (e.g. FenceChecker -> pkg/security or a neutral home per
  its real importers) BEFORE deleting; the absorbed FenceStage interface
  (leaf 01) does NOT depend on the old type.
- Non-test callers expected: ZERO for Engine/NewEngine/CheckForAgent (that is
  the premise). If you FIND one, STOP and report - the premise is wrong.
- Docs referencing the engine: docs/workflows/security*, docs/reference/
  generated/* (regenerate, do not hand-edit).

## Tasks

| Task ID | Objective | Files | Verify command | PASS state |
|---------|-----------|-------|----------------|------------|
| T1 | Inventory + relocate surviving exported types used elsewhere (expected: FenceChecker; move to pkg/security and adapt the leaf-01 daemon adapter to construct it) | internal/security/* -> pkg/security/, importers updated | `go build ./...` | builds; import cycle check clean |
| T2 | Delete the Engine package (engine.go, schema, seeds, override store, tests) once survivors relocated | internal/security/ (remaining files) | `go build ./...` | package gone; zero references (grep) |
| T3 | Sweep references: docs + comments pointing at internal/security.Engine as live; mark issue #77 resolution in commit body | docs/**, code comments | grep -rn "internal/security" --include="*.go" \| grep -v _test | zero non-test hits (or test-only for relocated FenceChecker) |
| T4 | Regenerate affected generated docs | `make docs-generate` (if reference docs changed) | docs-check clean | fresh |

## Self-Verification Checklist

- [ ] Premise verified: you found NO production Engine caller (if found: STOP)
- [ ] FenceChecker relocation keeps the leaf-01 central stage working
- [ ] `go build ./...` + `go test -p 2 ./pkg/security/... ./internal/agent/... ./internal/daemon/...` green
- [ ] No dangling references; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Survivors found + relocated: [types -> new homes]
2. Deletions: [files]
3. Verify output: [last 20 lines]

## Review Checklist (For Review Agent)

- [ ] Deletion diff contains ONLY dead engine machinery + relocations
- [ ] Central fence stage still enforced after the move (gate F6 GREEN)
- [ ] Issue #77 named in the commit message the orchestrator writes
