# 00T: Branch Test Gate for 01-permissions - Test-Only Leaf

> **For the implementing agent:** Write TESTS ONLY. No implementation.
> Tests must fail RED for missing-implementation reasons. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Unit + integration tests for the absorption work (fence stage,
  decision audit, engine deletion precondition).
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** T

## Goal

Prove the branch's three features RED before any of them lands, so absorption
cannot silently degrade the live permission path.

## Tasks

| Task ID | Objective | Files | RED reason | Given/When/Then |
|---------|-----------|-------|------------|-----------------|
| T1 | FenceStage tests: nil fence = unchanged legacy result; fence deny => Allowed:false Reason=err; stage order (fence after path glob pass, before confirmation); shell op mapping (read/write/delete, CheckCommand w/ workdir) | pkg/security/fence_stage_test.go | FenceStage symbol absent | Given fence denying /tmp/x write; When CheckPermissionForAgent file_write /tmp/x; Then denied with fence source; nil fence => legacy allow |
| T2 | Decision-audit tests: RuleSource stamped on every CheckResult return (table over actions); vocabulary membership | pkg/security/decision_audit_test.go | RuleSource field absent | Given each builtin action; When checked; Then RuleSource in frozen vocabulary, non-empty on deny |
| T3 | Concurrency pin: concurrent CheckPermissionForAgent with distinct fences (two checker instances) - no shared-state interference, -race clean | pkg/security/fence_stage_concurrent_test.go | absent | Given two checkers w/ different fences; When 100 parallel checks; Then each result matches its own fence |

## Self-Verification Checklist

- [ ] All RED for missing-symbol reasons (record evidence)
- [ ] Table-driven; -race clean shape (no shared captured vars)
- [ ] gofmt clean

**DO NOT COMMIT.** Orchestrator validates against the branch Test Gate
matrix, commits, sets TESTS_VALIDATED.

## Completion Report Format

1. Files touched: [paths]
2. RED evidence: [per-test failure reason]
