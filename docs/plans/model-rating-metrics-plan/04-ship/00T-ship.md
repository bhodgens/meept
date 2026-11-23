# Ship Gate Tests - Test-Gate Leaf (00T)

> **For the implementing agent:** TESTS ONLY, no implementation. RED for
> missing-implementation reasons. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Gate tests SH-* for the hermetic e2e suite skeleton + docs gates
- **Dependencies:** branches 01-03 COMPLETE
- **Estimated Context:** 25K
- **Concurrency Group:** A

## Goal

Executable acceptance that the e2e suite exists, runs hermetically, and covers the
four scenarios; that the manifest row is registered; that the docs land.

## Verified Anchors

- `e2e/manifest.json:4` - comment block defining suite fields + census rules
- `e2e/manifest.json:310` - internal/metrics/ path_map list
- `e2e/suites/` - existing suite layout (grep an example, e.g. dispatch-trace)
- `docs/workflows/` - doc naming pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `e2e/suites/model-rating/gate_test.go` (`//go:build e2e`) - SH-SUITE-1 (suite
  compiles + scenarios registered), SH-MANIFEST-1 (manifest row + path_map entry),
  SH-CAPTURE-1, SH-RETENTION-1, SH-SURFACE-1, SH-JOIN-1 (scenario skeletons asserting
  the END-TO-END outcomes; RED until 01-e2e-suite fills the rigs)
- `docs_gate_test.go` equivalent not used - docs verified by orchestrator review +
  `make agents-check` (recorded in the report)

### What This Leaf Consumes

- C1-C6 as oracle; manifest contract from ../orchestrator.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite gate skeleton | Create: e2e/suites/model-rating/gate_test.go | `go test -tags e2e ./e2e/suites/model-rating/ -run SH-` | suite dir/scenarios missing | GREEN after 01 | Given the suite; When scenarios run against the rig; Then grade-capture-01 persists rows, retention-01 proves no sweep, surface-01 reads the endpoint, join-proof-01 proves the blame rule |
| T2 | Manifest gate | Modify: e2e/suites/model-rating/gate_test.go | `go test -tags e2e ./e2e/suites/model-rating/ -run SH-MANIFEST` | row missing | GREEN after 01 (manifest edit in 01) | Given manifest.json; When parsed; Then model-rating suite registered + internal/modelrating/ path key present + internal/metrics/ list contains model-rating |

## Self-Verification Checklist

- [ ] RED for missing suite/manifest only; `//go:build e2e` tag set
- [ ] No implementation rigs written
- [ ] go vet clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. RED evidence; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Four scenarios asserted by OUTCOME (not implementation detail)
- [ ] Manifest assertions match the census rules verbatim

Output: APPROVED or specific gaps.

## Notes

Scenario skeletons may skip(t) with a named reason until 01 lands - a RED-by-skip is
NOT acceptable for T1/T2 verdicts; use missing-symbol/build RED before 01, then the
full assertions activate.
