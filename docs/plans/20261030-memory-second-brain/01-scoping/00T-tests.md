# Branch Test Gate: Scope Pins - Implementation Leaf (TEST GATE VARIANT)

> **For the implementing agent:** You are the test-gate implementer for this
> branch. Tests ONLY - the scope features DO NOT EXIST YET. Every test must
> fail RED for a missing-implementation reason. Unit pins are allowed here
> (store-level plumbing bootstrap); the live e2e surface lives in the parent
> gate's memory-scoping suite. Do NOT commit. Do NOT use read_file on
> existing source files. Write once and stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** RED unit pins for Scope enum/column/backfill, WithScopeFilter predicate, and B1 scope-input resolution.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** T
- **Audit references:** S1-S3 of branch Test Gate

## Goal

Commit RED tests that pin contracts C1, C2, and B1 so leaves 01 and 03
implement against enforced shapes, not prose.

## Context

- internal/memory tests live beside sources (package memory). Use the
  established store test setup pattern (grep existing *_test.go for temp-dir
  store construction).
- internal/agent scope pins: place in the package the resolution lands in
  (internal/agent) using existing test helpers for session/agent specs.

## Verified Anchors

- internal/memory/manager_test.go - existing Manager test setup pattern to
  follow.
- internal/memory/migrate_test.go - migration test pattern (legacy-db
  fixtures) to follow for T-s2.
- internal/agent/registry_test.go - spec-resolution test pattern.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
internal/memory/scope_gate_test.go      (S1, S2 pins)
internal/agent/scope_inputs_gate_test.go (S3 pin)
```

### What This Leaf Consumes

- C1/C2 from ../../master.md; B1 from ../orchestrator.md (shapes below).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T-s1 | S1: Scope type + column + global backfill pins | Create: internal/memory/scope_gate_test.go | `go test -p 2 ./internal/memory/ -run TestScope` | Scope undefined (compile RED = acceptable only for symbol-missing; the test bodies must assert behavior once symbols exist) | legacy db -> all rows scope=global; new scoped store roundtrips | Given legacy fixture db; When opened; Then column exists, rows backfill global |
| T-s2 | S2: WithScopeFilter matrix | Create: internal/memory/scope_gate_test.go (same file) | `go test -p 2 ./internal/memory/ -run TestScopeFilterMatrix` | WithScopeFilter undefined | 3x3 matrix: global/project/user rows x filter combos; no-option = unfiltered | Given rows in all scopes; When filtered; Then exact expected subsets return |
| T-s3 | S3: resolveScopeInputs precedence | Create: internal/agent/scope_inputs_gate_test.go | `go test -p 2 ./internal/agent/ -run TestResolveScopeInputs` | func undefined | ""->project; user->user; global->global; invalid->project; unbound session -> empty projectID | Given spec+session combos; When resolved; Then exact tuples |

## Self-Verification Checklist

- [ ] All tests compile except for the MISSING symbols (Scope,
      WithScopeFilter, resolveScopeInputs, MemoryMaxScope) - if they fail for
      any other reason (port, fixture, setup), fix the TEST
- [ ] Run each: capture the RED reason in the report
- [ ] No implementation changes anywhere

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Verify command + output:** [RED output per test]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] RED for missing-symbol reasons only
- [ ] Matrix covers: no-option unfiltered, empty-userID single-user match,
      unbound-session project miss (B1)
- [ ] No scope creep; gofmt clean

Output: APPROVED or specific gaps.

## Notes

- Compile-failure RED is acceptable for gate tests of not-yet-existing
  symbols, but the orchestrator validates that test BODIES assert the exact
  contract shapes (delta protocol) before committing the gate.
