## Summary

Add a durable, per-model work-quality record to meept and expose it through the existing analytics surfaces. The goal is to answer three questions from the analytics dashboard:

1. Which models are doing bad work?
2. What kind of bad work did each model do?
3. Which reviewer judged the work?

This is NOT a new analytics package. It extends `internal/metrics`, `GET /api/v1/metrics/live`, the Flutter metrics panel, the menubar dashboard, and `meept analytics`.

The hierarchical-plan implementation (plan tree, test-first gates, leaf sizing, isolation worktree, compliance scan) belongs in a separate tree. This issue covers the model-rating track only. The two trees share a frozen `ReviewVerdict` contract; neither owns the other.

## Motivation

A shared post described a harness that grades an agent's work during review, blames the model for missing work, extra work, or grievous bugs, and records those defects against the model. Meept has the parts but never joins them:

- Review events carry `task_id`, `step_id`, `status`, `feedback`, `confidence`, `reviewer`, `revision_count` (`internal/agent/review_manager.go:909-946`).
- The metrics collector records review status and reviewer (`internal/metrics/collector.go:388-405`).
- Task outcomes carry `model_id` (`internal/metrics/collector.go:856-887`, `internal/agent/loop.go:11441-11456`).
- Model usage already aggregates by `provider/model_id` (`internal/metrics/usage.go:21-32`, `:63-77`).
- `/api/v1/metrics/live` already returns models, agents, totals (`internal/comm/http/live_metrics_usage.go:23-33`).
- The Flutter metrics panel already renders per-model usage (`ui/flutter_ui/lib/features/metrics/metrics_panel.dart:9-16`).
- `meept analytics models` already compares model performance (`cmd/meept/analytics.go:215-256`).

No row links a reviewed model, a reviewer, and a defect class. No defect taxonomy exists. No dashboard view exists for review grades.

## Scope

### In scope

- Frozen defect taxonomy with category and severity.
- Capture of reviewed model id, reviewer id, reviewer model id, and defect class on each review.
- Durable SQLite record in `internal/metrics` with retention beyond the 24-hour raw `metrics_live` window.
- Query and aggregation by model, by category, and by reviewer.
- `GET /api/v1/metrics/reviews` HTTP endpoint.
- New section in the Flutter metrics panel at `/tools/metrics`.
- New tab in the menubar analytics dashboard.
- `meept analytics reviews` CLI subcommand.
- Hermetic e2e suite with a manifest row.
- `docs/workflows/model-rating.md` and AGENTS.md updates.

### Out of scope

- The hierarchical-plan execution profile and its plan-tree changes.
- Automated grading of defect categories. First version uses human-assigned categories; the record is the source of truth.
- New analytics package or new metrics database.
- Changes to `internal/agent/review.go` beyond the fields needed to carry the verdict.

## Proposed defect taxonomy

Two axes: category (what kind of defect) and severity (how bad). A model can have many low-severity style defects and one critical security defect; those mean different things for fit.

Categories:

1. `wrong_language` - code written in the wrong language for the task (example: Python where Go is required).
2. `wrong_api_or_dependency` - uses an API, library, or package the project does not have or forbids. Includes invented imports.
3. `logic_error` - code compiles but computes the wrong result.
4. `security_defect` - injection surface, secret exposure, missing auth check, unsafe deserialization, path traversal.
5. `incomplete_implementation` - required behavior absent.
6. `unrequested_change` - work outside task scope.
7. `build_or_test_failure` - generated code does not compile or its tests fail.
8. `style_bloat` - over-verbosity, excessive comments, redundant abstraction, needless boilerplate.
9. `convention_violation` - breaks a repo convention: naming, file layout, error handling, import rules.

Severity:

1. `cosmetic`
2. `functional`
3. `blocking`
4. `critical`

Deferred categories (do not add now): hallucinated citation, test tampering, data exfiltration. Rare enough to start as subclasses under `logic_error` and `security_defect`.

## Key decisions to resolve before authoring the plan tree

### 1. Join grain: per task or per step

- **Per task**: cheaper. Joins on `task_id`, which already links review to outcome. About 4-6 days less plumbing. Cannot say which exact step used which model.
- **Per step**: required for exact blame. `task_steps` has `id` and `model_override` (`internal/task/step.go:487-517`), but `model_override` is a directive, not proof of the serving model. `llm_calls` has `model_id`, `session_id`, `trace_id`, `span_id` but **no `step_id`** (`internal/metrics/store.go:246-268`). Adding `step_id` needs a migration plus a stamp at every call site that knows the step.

Recommendation: per step, because the stated goal is to review which models do bad work with a dashboard. Accept the migration now. Design the schema so per-task aggregate queries still work.

Question: accept the `llm_calls` migration and call-site threading now, or ship per-task first and add per-step in a follow-up?

### 2. Reviewer identity

- The review event carries `reviewer` as an agent id (`internal/agent/review_manager.go:921`), not a model id.
- A reviewer agent has a model binding through the precedence chain in `internal/agent/AGENTS.md` (`model:` in AGENT.md, `model_alias:`, same-id alias, global default).
- Which reviewer identity does the dashboard need: agent id, model id, or both?

Recommendation: store both. Reviewer agent id identifies policy; reviewer model id identifies the grader. When the reviewer model cannot be resolved, store empty and show "unknown" rather than guessing.

Question: confirm both identities, or only agent id?

### 3. Defect assignment: human or automated

- No code today emits the proposed taxonomy. The outcome judge (`internal/eval/judge.go:46-56`), oracle (`internal/eval/oracle.go:14-27`), and sequence grader (`docs/workflows/trajectory-grading.md:3-11`) do not produce these categories.
- Automated classification would need training labels. First version should let the human reviewer assign categories; later versions can train or prompt a grader from those labels.

Recommendation: human-assigned in v1.

Question: confirm human-assigned, or require automated grading in v1?

### 4. Retention and aggregation

- Raw `metrics_live` rows are deleted after 24 hours (`docs/workflows/metrics.md:118-120`). Aggregates keep counters, not the reviewer tag on each review.
- Review grades need a longer window: 30 days raw, indefinite aggregate.

Question: confirm 30 days raw and indefinite aggregate, or a different window?

### 5. Storage shape

- `internal/metrics/store.go:229-240` has a `model_performance` table keyed on model, provider, and agent, with request/error counters. It has no reviewer or defect fields.
- Options: extend `model_performance`, or add a new `model_review_verdicts` row table plus a rollup.

Recommendation: new row table for individual verdicts; reuse `model_performance` only for rollup if its key supports the window. A row table is required for drilldown by reviewer and defect class.

Question: confirm new row table, or extend `model_performance`?

### 6. Surface priority

- All three surfaces are already wired to `/api/v1/metrics/live`: Flutter panel, menubar dashboard, CLI analytics.
- A dedicated `GET /api/v1/metrics/reviews` is cleaner than stuffing a longer-window, drilldown query into the 24-hour live snapshot.

Question: build all three surfaces in one tree, or phase them (HTTP + CLI first, GUI second)?

## Differentiation from adjacent work

- **Issue #61** (tool-driven plan decomposer): about planner output shape and step verification. Does not record model quality grades.
- **Issue #28** (older trees fail compliance scanner): about plan-document structure. Unrelated to model review.
- **meept-bench**: external harness with LLM judge and scorecards. Its results are external; this issue makes grades first-class in the daemon and dashboard.
- **gatecapture**: pairs a human allowed/denied verdict with a command-safety score at two chokepoints (`internal/gatecapture/store.go:57-86`). Narrow; this issue covers general work quality.
- **hierarchical-plan workflow**: governs how work is executed. This issue grades the work after execution. Separate trees, shared verdict contract.

## Effort estimate (before decisions)

- Per-task capture, store, query, HTTP, CLI: 8-12 days.
- Additional per-step migration and call-site threading: 4-6 days.
- Flutter panel: 3-5 days.
- Menubar tab: 2-3 days.
- Hermetic e2e suite and docs: 3-5 days.
- Total per-step, all surfaces: 20-31 working days for one engineer.

The dominant risk is the `llm_calls` step-id migration, not the dashboard.

## Plan-tree requirements

Implementation must follow the Hermes hierarchical-plan skill suite:

- Test-gate leaves before implementation leaves, one per implementation leaf.
- Leaves of 3 or fewer files, one language per leaf.
- Isolation worktree at `.hermes/worktrees/<tree-name>` on branch `plan/<tree-name>`.
- Frozen interface contracts in `master.md`.
- Orchestrator reviews in-session; leaf agents do not commit.
- `check_template_compliance.py <tree> --strict-leaves` must print `ALL TREES COMPLIANT: True` before commit.
- New e2e suite and `e2e/manifest.json` row in the same commit.
- Any new bus topic requires regenerated connectivity graphs in the same commit.
- `docs/workflows/model-rating.md` and AGENTS.md updates in the same commit.

## Acceptance criteria

1. A reviewer can assign a category and severity to a reviewed step.
2. The record stores reviewed model id, reviewer agent id, reviewer model id, category, severity, and timestamp.
3. Per-step queries resolve the serving model from `llm_calls`.
4. `GET /api/v1/metrics/reviews` returns grades with a configurable window.
5. Flutter metrics panel shows grades by model and by reviewer.
6. Menubar dashboard shows the same data.
7. `meept analytics reviews` prints a readable table.
8. Hermetic e2e suite proves capture, query, and surface rendering.
9. All generated artifacts are fresh and all AGENTS.md summaries are updated.
