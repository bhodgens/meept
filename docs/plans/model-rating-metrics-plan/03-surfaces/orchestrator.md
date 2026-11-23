# Surfaces: HTTP + CLI + Flutter + Menubar - Branch Orchestrator

> **For the executing agent:** Orchestrator for this branch. Dispatch, review,
> re-dispatch, track. Do NOT implement code yourself. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Commits in the worktree after review, by you.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 4 leaf documents (00T-surfaces, 01-http-endpoint, 02-cli,
  03-flutter-panel, 04-menubar-tab) - 5 total counting the gate
- **Scope:** Expose verdict queries on four surfaces, all reading C2's query API.

## Goal

The same grades, four windows: `GET /api/v1/metrics/reviews` (HTTP),
`meept analytics reviews` (CLI), a grades section in the Flutter metrics panel
(`/tools/metrics`), and a menubar analytics tab. Backend leaves land first; UI leaves
consume the frozen HTTP shape so they can dispatch in parallel.

## Architecture

HTTP: route in internal/comm/http/server.go beside the metrics/live route
(mux registration :1672); data via a `WithReviewVerdictProvider` seam mirroring
`WithMetricsUsageProvider` (live_metrics_usage.go:23-33); degrade-to-empty on store
error (same posture). CLI: new subcommand beside `analytics models`
(cmd/meept/analytics.go:215-256), opening the metrics DB via the file's existing
`openMetricsDB`. Flutter: metrics_provider + metrics_panel section; route exists
(router.dart:212). Menubar: Swift tab beside LiveMetricsView.swift; build gate +
manual visual check.

## Interface Contracts

C5 and C6 frozen in ../master.md. HTTP response shape (frozen):

```json
{"window": "30d", "group_by": "model",
 "rows": [{"group": "openai/gpt-x", "verdicts": 42, "defects": 9, "pass_rate": 0.786}],
 "workflow_excluded": 5}
```

Query params: `window` = 24h|7d|30d|all (default 30d), `group_by` =
model|judge|category|rubric (default model). Flutter Dart model mirrors these keys
exactly; menubar decodes the same JSON.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | [00T-surfaces.md] | leaf (test gate) | 01-foundations COMPLETE | 25K | A |
| 01 | [01-http-endpoint.md] | leaf | 00 TESTS_VALIDATED; branch 01 COMPLETE | 45K | B |
| 02 | [02-cli.md] | leaf | 00 TESTS_VALIDATED; branch 01 COMPLETE | 35K | C |
| 03 | [03-flutter-panel.md] | leaf | 01 REVIEWED (HTTP shape frozen) | 50K | D |
| 04 | [04-menubar-tab.md] | leaf | 01 REVIEWED (HTTP shape frozen) | 40K | D |

Groups B and C are FILE-DISJOINT (server.go+live_metrics_usage.go vs
cmd/meept/analytics.go) - dispatch together. Group D (both UI leaves) needs 01's
frozen response shape, not its code; dispatch after 01 REVIEWED.

## Dispatch Protocol

1. Phase 0: dispatch 00T-surfaces; validate; commit; TESTS_VALIDATED.
2. Phase 1: dispatch 01 + 02 together (disjoint files). Review each; commit.
3. Phase 2: dispatch 03 + 04 together with the frozen response JSON INLINED.
4. Phase 3: integration - `make build` in the worktree (Go + menubar),
   `flutter test` in ui/flutter_ui; COMPLETE to master.

## Review Checklist

- [ ] Response keys/query params byte-identical to C5 across all four surfaces
- [ ] HTTP degrades to empty rows on store error; provider seam nil-safe
- [ ] CLI table readable; sorts defects DESC then model ASC
- [ ] Naming contract: labels say judge, never reviewer
- [ ] Gate diffs empty; no artifacts; lower-case UI text (repo rule)
- [ ] Flutter shared code guards dart:io with kIsWeb where needed

Output: APPROVED or specific gaps.

## Coding Conventions

Master.md conventions. Dart: flutter format; Swift: match LiveMetricsView.swift style.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-surfaces | COMPLETE | 1 | gate tests landed with their implementation leaves |
| 01-http-endpoint | COMPLETE | 1 | 3e7fa710 |
| 02-cli | COMPLETE | 1 | 908e06ad |
| 03-flutter-panel | COMPLETE | 1 | dfb95627 |
| 04-menubar-tab | COMPLETE | 1 | 4394cfe5 |

## Integration Test Plan

In the worktree: `go test ./internal/comm/http/... ./cmd/meept/...` green;
`cd ui/flutter_ui && flutter test` green; `make build` green; manual: boot daemon,
`curl 'localhost:<port>/api/v1/metrics/reviews?group_by=judge'` returns rows,
`meept analytics reviews --days 7` prints the table, Flutter panel renders the
grades section, menubar tab shows the same numbers.

## Test Gate

Test leaf: 00T-surfaces.md. Matrix:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F5 | HTTP reviews endpoint | master.md C5 | HTTP-ROUTE-1 (200 shape), HTTP-WINDOW-1 (4 windows), HTTP-GROUP-1 (4 group_bys), HTTP-DEGRADE-1 (store error -> empty rows) | e2e surface scenario (04-ship) | PENDING |
| F6 | CLI reviews table | master.md C6 | CLI-TABLE-1 (rows render), CLI-WINDOW-1 (--days) | e2e surface scenario (04-ship) | PENDING |
| F7 | Flutter grades section | master.md C6 | gui-flows Dart suite (grades widget test) | gui-flows | PENDING |
| F8 | Menubar tab | master.md C6 | n/a (Swift; build gate) | n/a (manual) | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: (empty).

## Frozen Documents

Leaf documents freeze at dispatch; corrections ride dispatch context as dated addenda.

## Notes

- e2e/manifest.json already maps `internal/comm/http/` and `cmd/` prefixes to suites;
  the NEW suite row (04-ship) extends the map for `internal/metrics/` + this endpoint;
  no path_map conflict.
- Menubar reads the daemon over HTTP (Services/); no new Swift networking beyond the
  existing client pattern.
