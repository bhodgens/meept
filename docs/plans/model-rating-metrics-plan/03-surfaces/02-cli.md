# CLI: meept analytics reviews - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests EXIST -
> make them pass; never weaken them. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** `meept analytics reviews [--days N] [--group-by X]` subcommand
- **Dependencies:** 00T-surfaces TESTS_VALIDATED; branch 01 COMPLETE
- **Estimated Context:** 35K
- **Concurrency Group:** C (disjoint from 01: cmd/meept/analytics.go only)

## Goal

A readable terminal table answering "which models did bad work, judged by whom" over
a day window, straight from the metrics DB.

## Context

Follow `newAnalyticsModelsCmd`/`runAnalyticsModels` (analytics.go:215-256) exactly:
same command wiring, same openMetricsDB (analytics.go:57), same row-scan style. The
query uses Store.QueryReviewVerdicts (metrics import - cmd/meept already imports
internal packages; if the import graph objects, query by SQL in cmd/meept mirroring
runAnalyticsModels and note the deviation - prefer the store API).

## Verified Anchors

- `cmd/meept/analytics.go:57` - openMetricsDB
- `cmd/meept/analytics.go:215-256` - models subcommand pattern (wiring + query + table)
- `internal/metrics/review_verdicts.go` - QueryReviewVerdicts

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
meept analytics reviews [--days N] [--group-by model|judge|category|rubric]
// defaults: --days 30, --group-by model
// columns: group | verdicts | defects | pass_rate | (top severity when group-by=model|judge)
// sort: defects DESC, group ASC
```

### What This Leaf Consumes

- C2 QueryReviewVerdicts; IncludeWorkflow=false default, `--include-workflow` flag flips it.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Subcommand + query + table | Modify: cmd/meept/analytics.go | `go test ./cmd/meept/ -run CLI-` | subcommand missing | CLI-TABLE/WINDOW-1 GREEN | Given a temp metrics.db with mixed verdicts; When RunE executes; Then rows sorted defects DESC, columns labeled with judge naming, --days filters the window |
| T2 | Empty-db + error posture | Modify: cmd/meept/analytics.go | `go test ./cmd/meept/ -run CLI-TABLE` | - | GREEN | Given 0 rows; When RunE; Then clean "(no verdicts recorded)" message, exit 0; DB open failure => wrapped error exit 1 |
| T3 | Full package green | Verify only | `go test ./cmd/meept/` | - | green | Given the package; When full run; Then no regressions |

## Self-Verification Checklist

- [ ] Gate tests pass; gate files unmodified
- [ ] Table sized for both axes per repo TUI table rule where applicable (plain text table here)
- [ ] gofmt clean; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. gate tails + sample table output; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Output columns use judge naming; no reviewer label
- [ ] Behavior matches the models subcommand's error/exit conventions
- [ ] No gate diff; no artifacts

Output: APPROVED or specific gaps.

## Notes

Man/long-help text one sentence per flag; keep help text lowercase (repo rule).
