# HTTP Reviews Endpoint - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests EXIST -
> make them pass; never weaken them. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** `GET /api/v1/metrics/reviews` + provider seam in internal/comm/http
- **Dependencies:** 00T-surfaces TESTS_VALIDATED; branch 01 COMPLETE
- **Estimated Context:** 45K
- **Concurrency Group:** B (disjoint from 02-cli: server.go + live_metrics_usage.go only)

## Goal

Serve verdict aggregates with a configurable window and group-by, degrading to empty
rows exactly like /api/v1/metrics/live does.

## Context

Mirror the live-metrics extension: `liveMetricsResponse` wraps a snapshot + usage
arrays (live_metrics_usage.go:23-33); the server holds a provider interface set via a
With* setter (nil-safe, server.go:256 comment documents the live pattern). This leaf
adds `reviewVerdictProvider` (calls Store.QueryReviewVerdicts), a `WithReviewVerdictProvider`
setter, the route registration beside :1672, and `handleReviewsMetrics`. Window parsing:
24h|7d|30d|all -> Since; unknown window => 400. Unknown group_by => 400.

## Verified Anchors

- `internal/comm/http/server.go:256` - provider-field comment + nil posture
- `internal/comm/http/server.go:1672` - route registration line
- `internal/comm/http/live_metrics_usage.go:23-33` - liveMetricsResponse + buildLiveMetricsResponse
- `internal/metrics/review_verdicts.go` - QueryReviewVerdicts (branch 01)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
GET /api/v1/metrics/reviews?window=24h|7d|30d|all&group_by=model|judge|category|rubric
200 {"window","group_by","rows":[{group,verdicts,defects,pass_rate}],"workflow_excluded"}
// nil provider or store error => 200 with rows:[] and workflow_excluded:0
```

Setter: `func (s *Server) WithReviewVerdictProvider(p ReviewVerdictProvider) *Server`
(nil-safe; guard per repo convention). Daemon wiring of the provider is 04-ship's
integration leaf scope only if components.go needs it - this leaf exposes the seam;
components.go wiring rides the same leaf ONLY if trivial (a one-line setter call next
to the live-metrics wiring); otherwise report the deviation.

### What This Leaf Consumes

- C2 `QueryReviewVerdicts(q ReviewVerdictQuery) ([]ReviewVerdictRow, error)`
- workflow_excluded = count of scope=workflow rows in-window (one extra COUNT query).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Provider type + nil-safe setter | Modify: internal/comm/http/live_metrics_usage.go (or sibling file reviews_endpoint.go) | `go build ./internal/comm/http/` | symbols missing | compiles; HTTP-DEGRADE-1 GREEN | Given nil provider; When GET; Then 200 empty rows |
| T2 | Route + handler + param validation | Modify: internal/comm/http/server.go (+ handler file) | `go test ./internal/comm/http/ -run HTTP-` | route 404 | HTTP-ROUTE/WINDOW/GROUP-1 GREEN | Given each window/group_by combo; When GET; Then 200 frozen shape; bad params => 400 with error JSON |
| T3 | Daemon wiring | Modify: internal/daemon/components.go (one setter call if the store is in scope) | `make build` | - | binary builds; live daemon serves the endpoint | Given the daemon's metrics store; When booting; Then the provider is wired |

## Self-Verification Checklist

- [ ] Gate tests pass; gate files unmodified
- [ ] Response keys byte-identical to C5; degrade posture identical to live metrics
- [ ] No auth/origin changes (endpoint inherits server middleware like metrics/live)
- [ ] gofmt clean; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. gate tails; 3. Deviations (esp. if components.go wiring deferred).

## Review Checklist (For Review Agent)

- [ ] Frozen shape exact; both error paths degrade to empty rows (never 500)
- [ ] No new bus topics; make graphs-check unaffected
- [ ] Existing metrics/live behavior untouched

Output: APPROVED or specific gaps.

## Notes

Keep the handler stateless; the provider interface lives in live_metrics_usage.go's
package so the Flutter/menubar leaves can read the shape from one file.
