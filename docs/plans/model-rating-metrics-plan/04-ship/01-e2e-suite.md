# Hermetic E2E Suite + Manifest Row - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests EXIST -
> make them pass; never weaken them. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** e2e/suites/model-rating/ + e2e/manifest.json row
- **Dependencies:** 00T-ship TESTS_VALIDATED; branches 01-03 COMPLETE
- **Estimated Context:** 65K
- **Concurrency Group:** B

## Goal

Hermetic proof that grades capture, persist indefinitely, resolve the blamed model by
the trace join, and surface on HTTP + CLI.

## Context

Repo rule: all new feature tests live in the hermetic e2e tier. Fixture style:
`_test.go` files + `//go:build e2e`, temp dirs, no live network. Study one existing
suite for the rig pattern (e.g. e2e/suites/dispatch-trace/ - grep for its rig setup)
and mirror it. The suite boots the minimal in-process pieces (store + collector +
server handler via httptest), not a full daemon, unless the existing suites' pattern
is a rig daemon - follow the house pattern either way and note it.

## Verified Anchors

- `e2e/manifest.json:4` - census/comment contract
- `e2e/manifest.json:310` - internal/metrics/ path_map list (EXTEND, not replace)
- `e2e/suites/<nearest analog suite>/` - rig + runner pattern (grep before writing)
- `e2e/manifest.json` suites[].scenarios[] - scenario registration shape

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `e2e/suites/model-rating/` with scenarios: `grade-capture-01` (review verdict ->
  row with judged identities), `retention-01` (24h sweep + 30d purge leave verdict
  rows; retentionTables excludes the table), `surface-01` (HTTP rows + CLI table for
  the same verdicts), `join-proof-01` (rotation fixture: final successful model
  blamed; judge calls excluded)
- manifest.json: new suite entry + path_map extension (internal/modelrating/ new key;
  internal/metrics/ list gains model-rating)

### What This Leaf Consumes

- C1-C6 (branch 01-03 landed code) as the system under test

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite scaffold + grade-capture-01 | Create: e2e/suites/model-rating/{suite files per house pattern} | `make e2e-affected-area AREA=model-rating` | SH-* RED | SH-SUITE-1 + SH-CAPTURE-1 GREEN | Given a step graded with 2 defect verdicts + judge identity; When the capture pipeline runs; Then 2 rows with rubric/category/severity/judge ids/confidence/cycle |
| T2 | retention-01 | Extend the suite | command above | SH-RETENTION-1 RED | GREEN | Given verdict rows older than 24h and 30d; When both sweeps run; Then rows survive; retentionTables accessor excludes model_review_verdicts |
| T3 | surface-01 | Extend the suite | command above | SH-SURFACE-1 RED | GREEN | Given persisted verdicts; When GET /api/v1/metrics/reviews and `meept analytics reviews` run against the same db; Then identical group counts; frozen response keys |
| T4 | join-proof-01 + manifest row | Extend suite; Modify: e2e/manifest.json | command above + pre-commit-e2e hook dry run | SH-JOIN-1 RED | GREEN | Given llm_calls [error(m1), success(m2), success(m3)] + judge calls under a different agent id; When blame resolves; Then m3; manifest parses per census rules |
| T5 | Full gate | Verify only | `go test -tags e2e ./e2e/suites/model-rating/ -v` | - | all SH-* GREEN | Given the suite; When full run; Then green, hermetic (no network), < 60s |

## Self-Verification Checklist

- [ ] Gate tests pass unmodified; `//go:build e2e` on all suite files
- [ ] Manifest row satisfies the census comment (new package dir mapped)
- [ ] No live network; temp dirs only; suite < 60s
- [ ] No artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. `make e2e-affected-area AREA=model-rating` tail; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Four scenarios prove the master Test Gate F1-F6 outcomes end-to-end
- [ ] Manifest shape byte-valid (jq parses; census grep passes)
- [ ] House fixture pattern followed; hermetic; fast

Output: APPROVED or specific gaps.

## Notes

If the house e2e pattern requires a rig daemon binary, use the existing rig helper;
do not invent a new harness (skill: meept-e2e-harness).
