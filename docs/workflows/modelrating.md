# Model Rating (Work-Quality Verdicts)

Per-model work-quality rating for reviewed plan steps: the model judge grades
each reviewed step, and the verdicts persist indefinitely for analytics.

Terminology: the **model judge** is the code review agent acting as grader.
`reviewer` in existing code identifiers means the review-step machinery; only
this feature's new surfaces (table columns, API fields, CLI columns) say judge.

## What it answers

- Which models did bad work (defect counts + pass rate per reviewed model).
- What kind of bad work (category + severity per defect).
- Which judge graded the work (judge agent id + judge model id).

## How verdicts are captured

1. The model judge grades a reviewed step against the rubric catalog
   (`internal/modelrating`): rubric ID, category, severity. Trace-scoped blame
   wiring (step trace stamp, judge exclusion):
   `docs/agents/leaf-02-trace-stamping.md`.
2. `ReviewResult` carries the verdicts; the enriched `step.review_completed`
   bus event adds judged identities (reviewed model id, judge agent/model ids,
   cycle, is_final, trace id).
3. The metrics collector persists one `model_review_verdicts` row per verdict.

## Rubric scopes and attribution

- MODEL scope defects (line corruption, stray TODOs, heredocs, grep-abort,
  paraphrase drift, convention drift, rate limits ignored, + the 9 defect
  categories) blame the reviewed model.
- WORKFLOW scope defects (leaf sizing, contracts, test gate, review skipped,
  status trickle/reset, circular deps, context budget, sequential dispatch,
  leaf commits, stale plan sections, sibling design drift) blame the
  orchestrator agent + model and are EXCLUDED from per-model aggregates by
  default.

## Defect categories

`wrong_language`, `wrong_api_or_dependency`, `logic_error`, `security_defect`,
`incomplete_implementation`, `unrequested_change`, `build_or_test_failure`,
`style_bloat`, `convention_violation`. Passing steps record `category='pass'`
(the pass-rate denominator). Severities: `cosmetic`, `functional`, `blocking`,
`critical` (empty on pass rows).

## Storage and retention

`model_review_verdicts` (in metrics.db) is append-only and NOT in
`retentionTables`: verdicts survive the 24-hour metrics_live sweep and the
30-day audit purge. The reviewed model is resolved from the serving ledger by
`(trace_id, agent_id)`: the implementer's final successful `llm_calls` row for
the step's `stp-<stepID>` trace; judge-loop calls are excluded by agent_id.

## Surfaces

- `GET /api/v1/metrics/reviews?window=24h|7d|30d|all&group_by=model|judge|category|rubric`
  (a bad window/group_by answers 400; rows carry `group`, `verdicts`, `defects`,
  `pass_rate`, `label`, and the response includes `workflow_excluded`, the
  workflow-scope rows omitted from the aggregate)
- `meept analytics reviews [--days N] [--group-by model|judge|category|rubric]
  [--include-workflow]` (defaults: `--days 30`, `--group-by model`; rubric
  rows add a `label` column with the catalog rubric statement)
- Flutter metrics panel section at `/tools/metrics` (grades by model, by judge)
- Menubar analytics tab (same data)

## Producer sites (v1)

Emitting: ReviewManager review path + review evidence gate. Excluded in v1:
inline review tool (`internal/tools/builtin/review_tools.go`, no
review_completed event) and pair manager reviews - both keep the existing
behavior; extension is a follow-up.

## Tests

Hermetic e2e suite `model-rating` (e2e/suites/model-rating/): grade-capture-01,
retention-01, surface-01, join-proof-01. Foundations gates: MR-CAT-*
(internal/modelrating), VR-* (internal/metrics).

Source: bhodgens/meept issue #168; plan tree
docs/plans/model-rating-metrics-plan/.
