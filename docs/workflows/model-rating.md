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
- Under WHICH agent the model was graded (L6 model x agent dimension:
  `agent_id` per row; a model can be good for docs work and bad for code).

## How verdicts are captured

1. The model judge grades a reviewed step against the rubric catalog
   (`internal/modelrating`): rubric ID, category, severity. Trace-scoped blame
   wiring (step trace stamp, judge exclusion):
   `docs/agents/leaf-02-trace-stamping.md`.
2. `ReviewResult` carries the verdicts; the enriched `step.review_completed`
   bus event adds judged identities (reviewed model id, judge agent/model ids,
   cycle, is_final, trace id).
3. The metrics collector persists one `model_review_verdicts` row per verdict.
4. Rollups (HTTP/CLI/breakdown/counts) aggregate `is_final = 1` rows only
   (L7, bughunt 2026-11-24): a `needs_info` round is superseded by a later
   cycle of the same step, so its rows would double-count transient defects.
   `FinalVerdictsForStep` stays full-fidelity (raw last-cycle rows).

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
`style_bloat`, `convention_violation`. A CLEAN approval records exactly one
final `category='pass'` row with empty severity and no rubric id — that row is
the pass-rate denominator. An approved-with-findings review carries only its
defect verdicts (a pass row never offsets defects); rejections never carry
one. Severities: `cosmetic`, `functional`, `blocking`, `critical` (empty on
pass rows).

## Catalog versioning

Every verdict row stamps the rubric catalog version it was graded under
(`rubric_version`; current catalog: `internal/modelrating` `RubricCatalogVersion`).
Rows recorded before the column existed are backfilled to version 1 on first
open after upgrade. Any future rubric change (add, remove, re-scope, or
reword a rubric) MUST bump `RubricCatalogVersion` in the same commit, so
stored history stays interpretable instead of silently mixing into aggregates.

## Breakdown rollup (model x category)

`QueryReviewVerdictBreakdown` (and `group_by=model_category` on the surfaces)
answers "what kind of bad work did each model do" in one query: one row per
(reviewed model, category) pair with its count, pass rows included so the
denominator rides along, workflow rows always excluded. HTTP: rows land under
the `breakdown` key (the `rows` key stays an empty array in that mode; both
keys are always present). CLI: `meept analytics reviews --group-by
model_category` prints a MODEL/CATEGORY/COUNT table; `--json` on any mode
emits the same shape the HTTP endpoint returns, so scripts can consume both
identically.

## Storage and retention

`model_review_verdicts` (in metrics.db) is append-only and NOT in
`retentionTables`: verdicts survive the 24-hour metrics_live sweep and the
30-day audit purge. The reviewed model is resolved from the serving ledger by
`(trace_id, agent_id)`: the implementer's final successful `llm_calls` row for
the step's `stp-<stepID>` trace; judge-loop calls are excluded by agent_id.

## Surfaces

- `GET /api/v1/metrics/reviews?window=24h|7d|30d|all&group_by=model|judge|category|rubric|agent|model_category`
  (a bad window/group_by answers 400; rows carry `group`, `verdicts`, `defects`,
  `pass_rate`, `label`, and the response includes `workflow_excluded`, the
  workflow-scope rows omitted from the aggregate; `group_by=agent` answers the
  L6 model x agent breakdown in the `breakdown` array with rows
  `{model, category, agent, count}` — the `agent` key is absent from
  `model_category` rows, so pre-L6 payloads stay byte-identical)
- `meept analytics reviews [--days N] [--group-by model|judge|category|rubric|agent|model_category]
  [--include-workflow]` (defaults: `--days 30`, `--group-by model`; rubric
  rows add a `label` column with the catalog rubric statement; `agent` prints
  the MODEL/AGENT/COUNT table)
- Flutter metrics panel section at `/tools/metrics` (grades by model, by judge)
- Menubar analytics tab (same data)

## Producer sites

Emitting (L6 ruling 2026-11-27 — ALL review producers publish):

1. **Step-review lane** — `ReviewManager.ReviewStep` reviewer pass and the
   file-evidence gate (`review_evidence_gate.go`; gate rows carry catalog
   rubric ids MR-LC1/MR-PD1/MR-TD1).
2. **Pair-review lane** (`pair_manager.go`) — every actor→reviewer round
   publishes through `ReviewManager.PublishReviewVerdictFromProducer`
   (`review_source: "pair-review"`). Judge identities are the PAIR
   REVIEWER agent/model, the reviewed side is the ACTOR agent — never the
   step reviewer. A converged/exhausted round is final; a continuing
   round publishes `is_final=false` so the L7 rollups never double-count
   superseded rounds. Rejected rounds carry one `incomplete_implementation`
   row; approved rounds the pass row.
3. **Adversarial verification hook** (`verification_hook.go`) — parsed
   `VERDICT: PASS|PARTIAL|FAIL` lines publish (`review_source:
   "verification-hook"`, judge_agent_id `adversarial-verifier`, judge
   model = the real verifier serving ref). Unknown/unparseable verdicts
   never emit. A FAIL inside the fix-loop budget is non-final (the
   re-verify supersedes it); PASS/PARTIAL and an exhausted-fix FAIL are
   final.

Rows are distinguishable per lane by rubric/source: the gate lane tags
catalog ids, and the two L6 lanes emit an EMPTY rubric_id plus a detail
string naming the lane (the frozen C1 catalog has no pair/adversarial
rubric; the store is lenient — empty/unknown ids are kept, never
dropped), plus the unique hook judge_agent_id and the machine-readable
`review_source` payload key. The step-reviewer lane's own rows carry the
ids its judge graded.

Non-emitting by design: the inline review tool
(`internal/tools/builtin/review_tools.go`) publishes no review_completed
event at all — its output is a tool result, not a graded review.

## Model x agent grading (L6)

`model_review_verdicts.agent_id` stores the EXECUTING agent of the graded
work (the reviewed side's `step.AgentID`, threaded through the payload's
additive `reviewed_agent_id` key; the pair and hook lanes supply their
actor/loop agent). This answers "which AGENT was this model graded
under" — a model can be strong for docs work and weak for code work.

- Rollup: `QueryReviewVerdicts(GroupBy: "agent")` (single dimension).
- Pairing: `QueryReviewVerdictModelAgentBreakdown` — one row per
  (reviewed model, agent) pair; `reviewed_model_id` stays always in the
  GROUP BY, the same windowed/is_final/workflow-exclusion filters apply.
- Legacy rows: a migration backfills agent_id BEST-EFFORT from the same
  blame-join llm_calls row that resolved reviewed_model_id (trace +
  model match). Rows with no reliable source keep '' — the surfaces
  GROUP them as the unknown-agent bucket (`agent: ""` in JSON, `-` in
  the CLI table); they are never dropped from totals and never invented.
- Flutter/menubar agent grouping is NOT part of L6 (tracked by the
  parent audit); the HTTP endpoint and CLI surfaces carry the dimension.

## Tests

Hermetic e2e suite `model-rating` (e2e/suites/model-rating/): grade-capture-01,
retention-01, surface-01, join-proof-01, producer-lane-01 (L6: pair-review +
adversarial-hook emissions land in model_review_verdicts with agent_id through
the real collector, plus endpoint/CLI group_by=agent parity). Foundations
gates: MR-CAT-* (internal/modelrating), VR-* (internal/metrics), EM-*
(internal/agent).

Source: bhodgens/meept issue #168; plan tree
docs/plans/model-rating-metrics-plan/.
