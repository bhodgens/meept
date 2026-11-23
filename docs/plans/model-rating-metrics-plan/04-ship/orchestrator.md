# Ship: E2E Suite + Docs - Branch Orchestrator

> **For the executing agent:** Orchestrator for this branch. Dispatch, review,
> re-dispatch, track. Do NOT implement code yourself. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Commits in the worktree after review, by you.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaf documents (00T-ship, 01-e2e-suite, 02-docs)
- **Scope:** Hermetic e2e suite + manifest row, docs/workflows/model-rating.md +
  AGENTS.md updates - the repo's "same commit" ship gates.

## Goal

Prove capture, query, retention, and surfaces end-to-end hermetically, and land the
documentation the repo gates require in the same commit as the feature.

## Architecture

The e2e suite (e2e/suites/model-rating/) drives a test rig daemon + temp metrics.db:
grade a step via the review path, assert verdict rows, retention invariants, endpoint
output, CLI output. Docs: `docs/workflows/model-rating.md` (user-facing map) +
AGENTS.md row/pointer updates. Both leaves are Go/markdown; the suite is Go-only.

## Interface Contracts

C1-C6 frozen in ../master.md are the suite's oracle. The manifest row (frozen shape):

```json
"model-rating": {"path_prefix": "internal/modelrating/,internal/metrics/review_verdicts.go,internal/comm/http/reviews_endpoint.go,cmd/meept/analytics.go",
  "runner": "go", "status": "implemented", "diff": "M"}
```

(Grep e2e/manifest.json suites[] entries for the exact field names before writing;
the shape above is indicative, the file is the contract.)

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | [00T-ship.md] | leaf (test gate) | branches 01-03 COMPLETE | 25K | A |
| 01 | [01-e2e-suite.md] | leaf | 00 TESTS_VALIDATED; branches 01-03 COMPLETE | 65K | B |
| 02 | [02-docs.md] | leaf | branch 01 REVIEWED (suite names in doc) | 30K | B* |

B and B* share no files but 02 references 01's suite/scenario names - dispatch 01
first, then 02 (or dispatch together with 01's scenario list INLINED in 02's brief).

## Dispatch Protocol

1. Phase 0: dispatch 00T-ship; validate vs matrix; commit; TESTS_VALIDATED.
2. Phase 1: dispatch 01; review; commit. Phase 2: dispatch 02; review; commit.
3. Phase 3: integration - run the full master Integration Test Plan in the worktree;
   COMPLETE to master.

## Review Checklist

- [ ] Suite covers master.md Test Gate rows F1-F6 scenarios end-to-end
- [ ] Manifest row + path_map extension present; pre-commit-e2e census satisfied
- [ ] Fixture hygiene: _test.go + `//go:build e2e` tags per repo convention
- [ ] Docs: judge naming throughout; producer-site exclusions stated with reasons
- [ ] AGENTS.md routing-table row for internal/modelrating (make agents-check green)
- [ ] Gate diffs empty; no artifacts

Output: APPROVED or specific gaps.

## Coding Conventions

Master.md conventions; e2e fixtures per repo rule (hermetic, no live network).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-ship | PENDING | 0 | |
| 01-e2e-suite | PENDING | 0 | |
| 02-docs | PENDING | 0 | |

## Integration Test Plan

In the worktree: `make e2e-affected-area AREA=model-rating` green;
`make agents-check` green; `make build` green; `make test -p 2` (macOS) green;
`make graphs-check` green.

## Test Gate

Test leaf: 00T-ship.md. Matrix:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1-F6 | Capture -> store -> surfaces, retention | master.md matrix | (covered by branches 01-03 gates) | e2e: grade-capture-01, retention-01, surface-01, join-proof-01 | PENDING |
| F9 | Docs + AGENTS.md | R11-13 | n/a | feature-docs hook + agents-check | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: (empty).

## Frozen Documents

Leaf documents freeze at dispatch; corrections ride dispatch context as dated addenda.

## Notes

- The e2e census (manifest.json comment) requires every NEW internal/ package dir to
  map a path key: internal/modelrating/ is new - the row covers it.
- `internal/metrics/` already maps to four suites (manifest.json:310); ADD
  model-rating to that list, do not replace.
- AGENTS.md root is size-gated (16 KiB): add only the routing-table line for
  internal/modelrating -> docs/agents/ or the workflow doc pointer; keep under gate
  (`make agents-check`).
