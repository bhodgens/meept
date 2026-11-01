# Branch 01 Test Gate - Implementation Leaf

> **For the implementing agent:** Write TESTS ONLY. No implementation code.
> Tests must fail RED for missing-implementation reasons (compile failures do
> not count as RED - stub nothing; reference the target behavior via existing
> exported surfaces where possible, otherwise the test asserts the NEW field
> and fails to compile is acceptable ONLY when the field is added in the same
> test file's expectations... prefer: add tests that compile against current
> types and fail on behavior, plus one test that requires the new field
> (will fail compile until impl - mark it with a build-tag-free skip check
> `t.Skip("gate: needs C1 field")` is NOT allowed - gate tests must fail, not
> skip). Practical rule: tests asserting tail-keep behavior compile today and
> fail RED (current code keeps head). The C1 field test goes in leaf 01's
> scope if it cannot compile here. Do NOT commit. Do NOT run git add.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Failing tests for tail-keep shell truncation and spill sweep bounds
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** solo (gate)

## Goal

Pin GAP-1 fix behavior in tests before implementation: (1) shell output over
MaxOutputSize keeps the TAIL, not the head; (2) truncation note states omitted
byte count; (3) spill sweep helpers enforce 24h age and 512MB size bounds,
oldest-first eviction.

## Context

- internal/tools/builtin/shell.go:31 - MaxOutputSize = 50000
- internal/tools/builtin/shell.go:510,719 - current HEAD truncation
  (`stdoutStr[:MaxOutputSize]`) - the bug class under test
- internal/llm/spill.go:46 - SpillService ($MEEPT_HOME/spill root)
- Repo test policy: shell.go already has tests (bug-fix class -> unit tests
  allowed in the existing file).

## Verified Anchors

- internal/tools/builtin/shell.go:31 - `MaxOutputSize = 50000`
- internal/tools/builtin/shell.go:510 - `stdoutStr = stdoutStr[:MaxOutputSize] + ...`
- internal/llm/spill.go:46-61 - `type SpillService struct`

## Interface Contracts (From Parent)

### What This Leaf Exposes

Test functions (names pinned):
```go
// internal/tools/builtin/shell_test.go
func TestShellTruncationKeepsTail(t *testing.T)
func TestShellTruncationNoteStatesOmittedBytes(t *testing.T)
func TestShellTruncationUnderCapByteIdentical(t *testing.T)
// internal/llm/spill_test.go (sweep helpers per C2; if the pure funcs do not
// exist yet, tests reference the pinned signatures below and fail RED at
// compile is NOT allowed - instead test via exported SpillService surface
// once leaf 02 lands its funcs. Gate variant: write these two as the pinned
// contract in the leaf 02 brief and cover only what compiles today.)
func TestSpillSweepRemovesFilesOlderThan24h(t *testing.T)
func TestSpillSweepEvictsOldestPast512MB(t *testing.T)
```

### What This Leaf Consumes

- C1 tail-truncation note format: `... (truncated, N bytes total; first M bytes omitted; full output: <path>)` (path portion asserted only in leaf 01 tests).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Tail-keep test | Modify: internal/tools/builtin/shell_test.go | `go test -p 2 -run TestShellTruncation ./internal/tools/builtin/` | current head truncation | tail bytes present, head bytes absent | Given a command emitting >50KB with unique head/tail markers; When the tool returns; Then output contains the tail marker and not the head marker |
| T2 | Note format test | Modify: internal/tools/builtin/shell_test.go | `go test -p 2 -run TestShellTruncationNote ./internal/tools/builtin/` | current note lacks omitted-bytes | note matches C1 prefix (path optional) | Given truncated output; When formatting the note; Then it states total bytes and omitted bytes |
| T3 | Under-cap regression | Modify: internal/tools/builtin/shell_test.go | `go test -p 2 -run TestShellTruncationUnderCap ./internal/tools/builtin/` | may pass today (acceptable for gate) | byte-identical output, Truncated=false | Given output < 50KB; When returned; Then equal to raw output and no truncation note |
| T4 | Sweep age bound | Modify: internal/llm/spill_test.go | `go test -p 2 -run TestSpillSweep ./internal/llm/` | sweep funcs absent (coordinate with orchestrator: pin signature `SweepToolSpills(root string, maxAge time.Duration, maxBytes int64, now time.Time) (removed int, err error)`; if compile-RED is unavoidable, orchestrator records it as a leaf-02-pinned contract and marks this row DEFERRED) | oldest-first eviction honored | Given files with mtimes spanning 48h totaling >512MB; When swept with now; Then >24h files removed and total <= 512MB |

## Self-Verification Checklist

- [ ] T1-T3 fail RED for behavior reasons (compile against current types)
- [ ] No implementation code written
- [ ] Tests deterministic (no sleeps > 100ms; use temp dirs via t.TempDir())

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify command output (last 20 lines per test); 3. deviations.

## Review Checklist (For Review Agent)

- [ ] RED reasons are behavioral, not accidental compile breaks
- [ ] Test names match the pinned contract exactly
- [ ] No weakening/skips

## Notes

T4 signature is the pinned contract for leaf 02. If it cannot compile
without implementation, orchestrator moves T4 into leaf 02's brief as a
tests-first task pair (write test, see RED, implement, see GREEN inside the
leaf) and records the deviation in the Test Gate matrix.
