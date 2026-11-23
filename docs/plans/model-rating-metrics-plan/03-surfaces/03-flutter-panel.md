# Flutter Metrics-Panel Grades Section - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Grades section in MetricsPanel (by model, by judge) + provider fetch
- **Dependencies:** 01-http-endpoint REVIEWED (frozen response shape)
- **Estimated Context:** 50K
- **Concurrency Group:** D (parallel with 04-menubar-tab; disjoint dirs)

## Goal

The metrics panel at /tools/metrics shows verdict grades grouped by model and by
judge, from the same endpoint the menubar reads.

## Context

`metrics_panel.dart` renders usage from GET /api/v1/metrics/live
(metrics_panel.dart:9-16, provider at ../../providers/metrics_provider.dart). Add a
`reviewGrades` fetch + a grades section below usage: one table by model, one by
judge. All UI text lowercase (repo hard rule). Shared code must not touch dart:io
unguarded (kIsWeb guard rule). Route already exists (router.dart:212); no nav changes.

## Verified Anchors

- `ui/flutter_ui/lib/features/metrics/metrics_panel.dart:5-16` - provider + watch pattern
- `ui/flutter_ui/lib/providers/metrics_provider.dart` - fetch + state pattern
- `ui/flutter_ui/lib/core/router.dart:212` - /tools/metrics route (no change needed)
- `ui/flutter_ui/test/features/metrics/` - existing widget-test placement (grep to confirm)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `MetricsProvider.reviewGrades` state + `fetchReviewGrades({window, groupBy})`
  hitting `GET /api/v1/metrics/reviews`
- Dart model mirroring the frozen response keys exactly:
  `{window, group_by, rows:[{group, verdicts, defects, pass_rate}], workflow_excluded}`
- Panel section: "grades by model" + "grades by judge" tables (group, defects,
  pass_rate), refresh button reuses the existing refresh action

### What This Leaf Consumes

- C5 frozen HTTP shape (01 REVIEWED)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Dart model + provider fetch | Modify: lib/providers/metrics_provider.dart | `cd ui/flutter_ui && flutter test test/features/metrics/` | model/fetch missing | model tests GREEN | Given the endpoint response; When decoded; Then keys map 1:1; empty rows => empty section |
| T2 | Panel grades section | Modify: lib/features/metrics/metrics_panel.dart | `cd ui/flutter_ui && flutter test` | section absent | widget tests GREEN | Given reviewGrades; When panel builds; Then two lowercase-titled tables render by model and by judge; loading + empty states match house style |
| T3 | Wire-shape stub test | Modify: test/e2e gui-flows stub data | `cd ui/flutter_ui && flutter test test/e2e` | - | gui-flows GREEN | Given the stub daemon; When the panel loads; Then grades section renders from wire-shape data |

## Self-Verification Checklist

- [ ] flutter test green; dart format clean
- [ ] Lowercase UI text; kIsWeb guards where dart:io would appear
- [ ] No route/nav changes; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. flutter test tails; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Response keys mirrored exactly (snake_case preserved)
- [ ] Judge labels (never reviewer); lowercase text
- [ ] Existing usage section untouched; parity with TUI/menubar data

Output: APPROVED or specific gaps.

## Notes

Judge by-model table shows top severity when the endpoint returns it in a later
wave - render pass_rate and defects only for v1 (frozen shape has no severity column).
