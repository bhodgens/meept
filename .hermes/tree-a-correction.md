## Summary

Add a durable, per-model work-quality record to meept and expose it through the existing analytics surfaces. The goal is to answer three questions from the analytics dashboard:

1. Which models are doing bad work?
2. What kind of bad work did each model do?
3. Which reviewer judged the work?

This is NOT a new analytics package. It extends `internal/metrics`, a new reviews HTTP endpoint, the Flutter metrics panel, the menubar dashboard, and `meept analytics`.

Tree A (this issue) records and displays ratings. Tree B (#169) changes how meept executes plans. They are separate trees with one shared seam: the reviewer agent's rubric-based verdict. Tree A owns the rating and severity IDs; Tree B owns the workflow rubric IDs.

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

## Resolved decisions

These were answered by the owner. They are contracts for the plan tree.

1. **Join grain: per step.** Accept the `llm_calls` migration and call-site threading. `llm_calls` has no `step_id` (`internal/metrics/store.go:246-268`); `task_steps.model_override` is a directive, not proof of serving model.
2. **Reviewer: the code review agent.** The review agent is automatically part of the planner's regimen. The orchestrator ensures review and scoring happen before the implementation step passes or fails. Store both reviewer agent id and reviewer model id, resolved through the existing model precedence chain; unknown means empty, never guessed.
3. **Automated grading by the reviewing agent.** The reviewer agent tracks which issues the implementation agent messed up. No human-assigned categories in v1.
4. **Indefinite retention.** 24 hours is not enough. Extend or restructure metric storage so review verdicts persist indefinitely. Decided: change the schema so the metrics database is independent from short-lived raw metrics. The verdict table is append-only and not pruned by the 24-hour raw sweep.
5. **Storage: new row table.** Add a `model_review_verdicts` table for individual verdicts. Reuse `model_performance` only for rollup if its key supports the window.
6. **Surfaces: HTTP + CLI first, then Flutter and menubar in the same tree, later wave.**

## Rating criteria: hierarchical-planning rubrics

The reviewer agent grades work using the rubrics from the hierarchical-planning skill suite. These criteria determine whether what a model did was "bad." A step passes or fails under the existing criteria after review and scoring.

The rubric set to freeze in the plan (from `hierarchical-planning` and its canon):

- Leaf sizing: more than 3 files or more than one language in a leaf is a defect.
- Interface contracts: missing or vague contracts between leaves is a defect.
- Test gate: implementation without validated test leaves is a defect.
- Orchestrator review skipped is a defect.
- Status not trickled to the tracking table is a defect.
- Circular dependencies between leaves is a defect.
- Context budget underestimated is a defect.
- Sequential dispatch of independent leaves is a defect.
- Status not reset on re-dispatch is a defect.
- Leaf agent committed code is a defect.
- Line-number corruption in generated files is a defect.
- Provider rate limits ignored is a defect.
- Inconsistent code conventions across leaves is a defect.
- Stray debug artifacts, TODOs, or placeholder values is a defect.
- Heredocs used in code assembly where write_file is required is a defect.
- grep -c/grep -q zero-match aborting a chain is a defect.
- Paraphrase drift from source specs is a defect.
- Design drift from sibling sessions is a defect.
- Missing or stale plan sections is a defect.

The reviewer emits a verdict per step with a rubric ID, a category, a severity, the reviewed model id, and the reviewer model id.

## Defect categories

Two axes: category (what kind of defect) and severity (how bad).

Categories:

1. `wrong_language` - code written in the wrong language for the task.
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

## Scope

### In scope

- Frozen rubric IDs and defect taxonomy.
- Automated grading by the reviewing agent per step.
- Capture of reviewed model id, reviewer agent id, reviewer model id, rubric ID, category, and severity.
- Indefinite retention. Metrics database restructured so review verdicts are independent from the 24-hour raw sweep.
- `model_review_verdicts` append-only table plus rollup.
- Query and aggregation by model, by category, by reviewer, and by rubric.
- `GET /api/v1/metrics/reviews` HTTP endpoint.
- `meept analytics reviews` CLI subcommand.
- New section in the Flutter metrics panel at `/tools/metrics`.
- New tab in the menubar analytics dashboard.
- Hermetic e2e suite with a manifest row.
- `docs/workflows/model-rating.md` and AGENTS.md updates.

### Out of scope

- Changes to how plans execute. That is Tree B (#169).
- New analytics package or new metrics database file unless the schema restructure requires one.
- Human-assigned categories.
- Changing the review agent's core decision logic beyond emitting the frozen verdict.

## Differentiation from adjacent work

- **#169** (Tree B, hierarchical-plan workflow) - changes planner, steps, gates, review lane, and worktree. Tree A grades the result of that workflow. Separate trees, shared verdict contract.
- **#61** (tool-driven plan decomposer) - planner output shape and step verification. Does not record model quality grades.
- **#28** (older trees fail compliance scanner) - plan-document structure. Unrelated to model review.
- **meept-bench** - external harness with LLM judge and scorecards. External; this issue makes grades first-class in the daemon and dashboard.
- **gatecapture** - pairs a human allowed/denied verdict with a command-safety score at two chokepoints (`internal/gatecapture/store.go:57-86`). Narrow; this issue covers general work quality.

## Effort estimate

- Per-step capture, store, query, HTTP, CLI: 8-12 days.
- Indefinite retention and metrics schema restructure: 4-8 days.
- Flutter panel: 3-5 days.
- Menubar tab: 2-3 days.
- Hermetic e2e suite and docs: 3-5 days.
- Total: 20-33 working days for one engineer.

The dominant risk is the `llm_calls` step-id migration and the reviewer model id resolution, not the dashboard.

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

1. A reviewer agent grades each reviewed step with a hierarchical-plan rubric ID, category, and severity.
2. The record stores reviewed model id, reviewer agent id, reviewer model id, rubric ID, category, severity, and timestamp.
3. Per-step queries resolve the serving model from `llm_calls`.
4. Verdicts persist indefinitely and survive the 24-hour raw metrics sweep.
5. `GET /api/v1/metrics/reviews` returns grades with a configurable window.
6. `meept analytics reviews` prints a readable table.
7. Flutter metrics panel shows grades by model and by reviewer.
8. Menubar dashboard shows the same data.
9. Hermetic e2e suite proves capture, query, retention, and surface rendering.
10. All generated artifacts are fresh and all AGENTS.md summaries are updated.
