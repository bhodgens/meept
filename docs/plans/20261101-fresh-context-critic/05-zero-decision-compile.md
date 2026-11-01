# Zero-Decision Compile Check - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT edit C10 files.
> Re-activate only e2e/suites/plan-critic/zero_decision_test.go.staged.

## Meta

- Parent: master.md
- Scope: Fail compile when a phase changes state and names no decision artifact.
- Dependencies: 00T TESTS_VALIDATED
- Estimated Context: 35K
- Concurrency Group: A

## Goal

The compiler already rejects a consume of an artifact no earlier phase produces (internal/plan/compiler.go:125). Add the decision-lineage rule in contract C7. This is a mechanical slice of the zero-decision bar. It does not add an LLM call.

## Context

Artifact kinds already include decision (compiler.go:477). Phase drafts carry produces and consumes (compiler.go:83-84). A mutating tool_hint is one of: code, shell, file_write, git, commit. Match case-insensitively on the step tool_hint string.

A phase passes the new check when either:

- it produces an artifact whose kind is decision, or
- it consumes a decision artifact that an earlier phase produces.

A phase with no steps, or only non-mutating tool hints, is unchanged.

## Verified Anchors

- internal/plan/compiler.go:83 - Produces and Consumes fields
- internal/plan/compiler.go:125 - consumeMsgUnknown format to mirror for the new error
- internal/plan/compiler.go:477 - kind allow-list includes decision

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract C7 error string: `phase %q changes state but names no decision artifact`

### What This Leaf Consumes

- C7 from master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add the check in the existing compile walk, after the consume check | Modify: internal/plan/compiler.go; Modify: the existing compiler test file only if a table test already exists, else add cases to that file | `go test -p 2 ./internal/plan/ -count=1 -run 'Compile\|Decision'` | Mutating phase with empty produces compiles | Error text matches C7 | Given a phase with tool_hint code and no decision produce or consume; When CompileSealed runs; Then error contains the phase name and "names no decision artifact" |
| T2 | Re-activate zero_decision_test.go.staged and make TestMutatingPhaseWithoutDecisionFailsCompile pass | e2e/suites/plan-critic/zero_decision_test.go | `go test -p 2 -tags e2e ./e2e/suites/plan-critic/ -count=1 -run TestMutatingPhaseWithoutDecisionFailsCompile` | Assertion fails because err is nil | Test passes | Given the draft in the gate test; When compiled; Then err != nil |

Do not change non-mutating phases. A phase that only has tool_hint "report" or "chat" must still compile without a decision artifact.

## Self-Verification Checklist

- [ ] C7 error text matches exactly
- [ ] Non-mutating phases still compile
- [ ] A phase that consumes an earlier decision compiles
- [ ] No C10 file in the diff
- [ ] No LLM call added

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Mutating set is exactly code, shell, file_write, git, commit
- [ ] decision kind is the existing kind, not a new string
- [ ] Gate test not weakened

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit verifier files or AGENT.md files listed in C10. Do not edit plan_critique_loop.go. This leaf is disjoint from the critic leaves.
