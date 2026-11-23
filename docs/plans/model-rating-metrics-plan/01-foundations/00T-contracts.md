# Foundations Gate Tests - Test-Gate Leaf (00T)

> **For the implementing agent:** Write TESTS ONLY. No implementation. Tests must
> fail RED for missing-implementation reasons (undefined symbols), never for
> syntax/compile errors in the tests themselves. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit - the orchestrator handles git.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Gate tests for the rubric catalog (MR-*) and verdict store (VR-*)
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A

## Goal

Pin the frozen contract set (master.md C1, C2) as failing tests so both implementation
leaves are judged against executable acceptance, not prose.

## Context

- `internal/modelrating/` does not exist yet - tests there fail on missing package (RED).
- `internal/metrics/store.go` exists; `retentionTables` at line ~727 lists the 30-day
  purge tables. VR-RET-1 greps/tests that list's construction to assert
  `model_review_verdicts` is absent once the symbol exists; until then RED.
- Query semantics per C2: `QueryReviewVerdicts` groups by model|judge|category|rubric;
  workflow-scope rows excluded from per-model groups unless `IncludeWorkflow`.

## Verified Anchors

- `internal/metrics/store.go:727` - retentionTables slice literal
- `internal/metrics/store.go:83-101` - StoreConfig / NewStore (test store construction)
- `internal/metrics/store.go:246-268` - llm_calls DDL house style for column conventions

## Interface Contracts (From Parent)

### What This Leaf Exposes

Test files only:
- `internal/modelrating/catalog_gate_test.go` (package modelrating_test) - MR-CAT-1..4
- `internal/metrics/review_verdicts_gate_test.go` (package metrics_test) - VR-REC-1,
  VR-Q-MODEL-1, VR-Q-JUDGE-1, VR-Q-CATEGORY-1, VR-Q-RUBRIC-1, VR-RET-1, VR-FINAL-1,
  VR-SCOPE-1

### What This Leaf Consumes

- C1 rubric catalog symbols (not yet defined - that is the RED reason)
- C2 store methods (not yet defined - that is the RED reason)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Rubric catalog gate tests | Create: internal/modelrating/catalog_gate_test.go | `go test ./internal/modelrating/ -run MR-CAT` | package `modelrating` does not exist | PASS after 01 lands | Given the catalog package; When enumerating IDs/scopes/categories/severities; Then IDs unique, categories = frozen 9+pass, severities = frozen 4, scopes = {model, workflow} |
| T2 | Verdict store gate tests | Create: internal/metrics/review_verdicts_gate_test.go | `go test ./internal/metrics/ -run 'VR-'` | RecordReviewVerdict/QueryReviewVerdicts undefined | PASS after 02 lands | Given a temp-dir Store; When recording N verdicts (mixed scopes, cycles, judges); Then queries group correctly, is_final marks last cycle, retentionTables excludes model_review_verdicts, workflow scope excluded from model group by default |

Tests use only `t.TempDir()` stores; no daemon, no bus. Table-driven where a test
sweeps vocab.

## Self-Verification Checklist

- [ ] Both test files compile ONLY against the frozen C1/C2 signatures (correct RED)
- [ ] `go vet` clean on the two test files
- [ ] No implementation code written anywhere
- [ ] No line-number corruption

**DO NOT COMMIT.** The orchestrator commits gate tests after in-session validation.

## Completion Report Format

1. Files touched; 2. `go vet` + test-run output (RED evidence); 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests fail RED for missing-implementation reasons only
- [ ] Assertions match master.md C1/C2 verbatim (column names, vocab strings)
- [ ] No implementation, no gate-test scope creep

Output: APPROVED or specific gaps.

## Notes

VR-RET-1: assert via the compiled slice, not a grep of source (tests cannot read
repo files portably): construct the retention purge through the exported path if one
exists, else export a `RetentionTables() []string` accessor in 02 for testability -
that accessor is part of C2's allowed additive surface.
