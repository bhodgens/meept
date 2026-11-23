# Foundations: Rubric Catalog + Verdict Store - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Dispatch implementation agents, review their work, re-dispatch if incomplete,
> track completion. Do NOT implement code yourself.
> Work ONLY in the isolation worktree
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`
> (branch `plan/model-rating-metrics-plan`). Commits happen in the worktree,
> after review, by you.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaf documents (00T-contracts, 01-rubric-catalog, 02-verdict-store)
- **Scope:** The `internal/modelrating` package (rubric catalog, taxonomy, Verdict
  wire struct) and the `model_review_verdicts` table + query API in `internal/metrics`.

## Goal

Two self-contained Go foundations with zero surface dependencies: the rubric/taxonomy
constants every later branch imports, and the append-only verdict store with
model/judge/category/rubric aggregation. Nothing here touches the agent loop, HTTP,
CLI, or any UI.

## Architecture

`internal/modelrating` is a leaf package (imports: stdlib only) holding frozen IDs
and the Verdict struct. `internal/metrics` gains `review_verdicts.go`: schema init in
the existing migration path, record/query functions on `*Store`. The store references
`modelrating` constants for validation (scope/category/severity vocab) - one import
direction only: metrics -> modelrating. Never the reverse.

## Interface Contracts

Contracts C1 and C2 are frozen in ../master.md. Restated obligations:

- This branch OWNS C1 (rubric catalog) and C2 (verdict store) exactly as written there.
- Column names, category/severity strings, and function signatures are frozen verbatim
  from master.md. Deviation = review failure.
- Table `model_review_verdicts` is NEVER added to `retentionTables`
  (`internal/metrics/store.go:727` list). Gate test VR-RET-1 pins this.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | [00T-contracts.md] | leaf (test gate) | none | 25K | A |
| 01 | [01-rubric-catalog.md] | leaf | 00 TESTS_VALIDATED | 40K | B |
| 02 | [02-verdict-store.md] | leaf | 00 TESTS_VALIDATED | 55K | B |

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch `00T-contracts.md` (concurrency A). Tests only, RED for
missing-implementation reasons. Validate in-session against the Test Gate matrix;
commit gate tests in the worktree; set 01+02 to TESTS_VALIDATED.

### Phase 1: Dispatch Group B

Dispatch 01 and 02 together (FILE-DISJOINT: 01 writes only `internal/modelrating/**`;
02 writes only `internal/metrics/review_verdicts.go` + `internal/metrics/store.go`
schema-init lines + `internal/metrics/review_verdicts_init_test.go`). 02's gate tests
compile against 01's constants - if 02 dispatches before 01 lands, include C1's
constant block INLINED in 02's dispatch context so tests compile standalone; reconcile
at review.

### Phase 2: Review and Commit

In-session review per master.md Review Checklist; orchestrator commits per leaf in
the worktree: `git commit -m "feat(model-rating): <leaf>" -- <leaf paths>`.

### Phase 3: Integration

`go build ./... && go test ./internal/modelrating/... ./internal/metrics/...` green
in the worktree; set children COMPLETE; report to master.

## Review Checklist

- [ ] C1/C2 satisfied verbatim (column names, signatures, vocab strings)
- [ ] metrics imports modelrating, never the reverse (no import cycle)
- [ ] Gate tests pass GREEN; gate-test diff empty on impl review
- [ ] `model_review_verdicts` absent from `retentionTables`
- [ ] No line-number corruption; no debug artifacts
- [ ] New IDs use `pkg/id.Generate()` where minted (none expected in this branch)

Output: APPROVED or specific gaps.

## Coding Conventions

Same as master.md Coding Conventions (Go sections). Leaves are Go-only.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-contracts | PENDING | 0 | |
| 01-rubric-catalog | PENDING | 0 | |
| 02-verdict-store | PENDING | 0 | |

## Integration Test Plan

In the worktree: `go vet ./internal/modelrating/... ./internal/metrics/...` clean;
`go test ./internal/modelrating/... ./internal/metrics/ -run 'MR|VR'` green;
`gofmt -l internal/modelrating internal/metrics` empty.

## Test Gate

Test leaf: 00T-contracts.md. Matrix (validated in-session before 01/02 dispatch):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Rubric catalog + taxonomy | master.md C1 | MR-CAT-1 (IDs unique), MR-CAT-2 (categories exactly the frozen 9+pass), MR-CAT-3 (severities frozen 4), MR-CAT-4 (scopes model/workflow) | n/a (package-level) | PENDING |
| F2 | Verdict store: record, query, rollup, retention, final-flag | master.md C2 | VR-REC-1, VR-Q-MODEL/JUDGE/CATEGORY/RUBRIC-1, VR-RET-1 (not in retentionTables), VR-FINAL-1 (is_final per cycle), VR-SCOPE-1 (workflow excluded from per-model rows unless IncludeWorkflow) | n/a (covered in 04-ship) | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: (empty).

## Frozen Documents

Leaf documents freeze at dispatch; corrections ride dispatch context as dated addenda.

## Notes

- store.go schema init: add the table DDL beside the existing
  `initAgentTaskSchema` pattern (collector.go:856-887 shows the house style).
- Column types follow house style: TEXT NOT NULL DEFAULT '', confidence REAL,
  is_final BOOLEAN, timestamp TEXT DEFAULT strftime UTC (see llm_calls DDL, store.go:246-268).
