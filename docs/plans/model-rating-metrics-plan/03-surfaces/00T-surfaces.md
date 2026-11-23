# Surfaces Gate Tests - Test-Gate Leaf (00T)

> **For the implementing agent:** TESTS ONLY, no implementation. RED for
> missing-implementation reasons. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Gate tests HTTP-* (endpoint) and CLI-* (subcommand)
- **Dependencies:** 01-foundations COMPLETE
- **Estimated Context:** 25K
- **Concurrency Group:** A

## Goal

Executable acceptance for the two backend surfaces; UI leaves are gated by the frozen
response shape (compile-time keys) plus existing suites, not by new gate files.

## Verified Anchors

- `internal/comm/http/server.go:1672` - mux.Handle pattern for /api/v1/metrics/live
- `internal/comm/http/live_metrics_usage.go:23-33` - provider seam + degrade posture
- `cmd/meept/analytics.go:215-256` - newAnalyticsModelsCmd / runAnalyticsModels pattern
- `cmd/meept/analytics.go:57` - openMetricsDB

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `internal/comm/http/reviews_endpoint_gate_test.go` - HTTP-ROUTE-1, HTTP-WINDOW-1,
  HTTP-GROUP-1, HTTP-DEGRADE-1
- `cmd/meept/analytics_reviews_gate_test.go` - CLI-TABLE-1, CLI-WINDOW-1

### What This Leaf Consumes

- C2 QueryReviewVerdicts (exists), C5 response shape (frozen in ../orchestrator.md).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | HTTP endpoint gate | Create: internal/comm/http/reviews_endpoint_gate_test.go | `go test ./internal/comm/http/ -run HTTP-` | route 404 / handler missing | GREEN after 01 | Given a server with a verdict provider; When GET /api/v1/metrics/reviews with each window/group_by; Then 200 + frozen JSON keys; nil provider => empty rows; store error => empty rows |
| T2 | CLI gate | Create: cmd/meept/analytics_reviews_gate_test.go | `go test ./cmd/meept/ -run CLI-` | subcommand missing | GREEN after 02 | Given a temp metrics.db with verdicts; When `reviews --days N` runs; Then table rows match query output; 0 rows => clean empty table |

## Self-Verification Checklist

- [ ] RED for missing symbols only; go vet clean
- [ ] Frozen response keys asserted verbatim
- [ ] No implementation

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. RED evidence; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Matrix rows F5/F6 covered exactly; frozen keys asserted; no scope creep

Output: APPROVED or specific gaps.

## Notes

Use the existing http test harness in internal/comm/http (grep `httptest` there);
CLI tests call the cobra command's RunE directly with a temp DB path.
