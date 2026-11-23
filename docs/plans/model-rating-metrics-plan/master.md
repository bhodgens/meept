# Model-Rating Metrics (Issue #168) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 branch documents (16 leaf documents total)
- **Scope:** Implement bhodgens/meept#168 - per-model work-quality rating with rubric grades, model-judge attribution, indefinite retention, and analytics surfaces (HTTP, CLI, Flutter panel, menubar tab).

## Isolation Worktree (MANDATORY)

ALL work in this tree executes in the isolation worktree `model-rating-metrics-plan`:

1. Create before wave 1 (orchestrator, once):
   `git worktree add .hermes/worktrees/model-rating-metrics-plan -b plan/model-rating-metrics-plan`
2. Every leaf dispatch context names the ABSOLUTE worktree dir
   (`/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`) as the working directory.
   Leaf agents do NOT commit; the orchestrator commits IN THE WORKTREE after each review.
3. Integration gates (build, `make test -p 2` on macOS, compliance scan) run in the worktree.
4. Merge to main at top-level completion only: every leaf COMPLETE ->
   in the PRIMARY checkout `git merge plan/model-rating-metrics-plan`, re-run the gate
   set on the merged diff, then `git worktree remove .hermes/worktrees/model-rating-metrics-plan`
   and delete the branch. NEVER merge per-leaf.

## Terminology (frozen naming contract)

**Model judge** is this implementation's name for the grading role: the existing code
review agent, acting as grader. The rename avoids overloading `reviewer`, which in
today's code means the review-step machinery (`ReviewResult.ReviewerID`, the review
events' `reviewer` payload key, `reviewerLoop` in `internal/agent/review_manager.go`).
Existing identifiers keep their names. ONLY new surfaces introduced by this tree say
"judge": the `model_review_verdicts` columns (`judge_agent_id`, `judge_model_id`),
`/api/v1/metrics/reviews` fields, CLI columns, Flutter/menubar labels, and docs.

## Goal

A durable, append-only record linking: the model that wrote a step's code, the model
judge (agent + model) that graded it, a rubric ID, a defect category, and a severity -
queryable by model, judge, category, and rubric on four surfaces. Verdicts persist
indefinitely (never in `retentionTables`). Workflow-scope rubric failures blame the
orchestrator agent+model, never the reviewed model, and are excluded from per-model
aggregates. This tree records and displays only; plan-execution changes are #169
(never in scope here).

## Architecture

- New package `internal/modelrating`: rubric catalog (IDs, scopes, categories,
  severities) as Go constants plus the `Verdict` wire struct. One language, leaf-sized.
- `internal/metrics`: new `model_review_verdicts` table (append-only, NOT in
  `retentionTables`) plus record/query/rollup API in a new `review_verdicts.go`.
  No `llm_calls` schema change: the join keys on `(trace_id, agent_id)`.
- `internal/agent`: review producers (ReviewManager `ReviewStep` path + evidence gate)
  attach `modelrating.Verdict` values to `ReviewResult`; the enriched
  `step.review_completed` payload carries them; the metrics collector persists rows.
  The handler's existing trace mint seam (`WithTraceContext`, handler.go:917) gains a
  step-scoped trace id on implementer calls so `llm_calls.trace_id` resolves the
  serving model of the implementer's final successful completion.
- Surfaces: `GET /api/v1/metrics/reviews` (comm/http), `meept analytics reviews`
  (cmd/meept), Flutter metrics-panel section (dart), menubar analytics tab (Swift).
- Ship: hermetic e2e suite + `e2e/manifest.json` row in the same commit; docs +
  AGENTS.md updates in the same commit.

## Interface Contracts

### C1: Rubric catalog (`internal/modelrating`)

```go
package modelrating

type RubricID string   // e.g. "MR-LC1" line corruption, "MR-TD1" stray TODOs; see 01-rubric-catalog.md
type RubricScope string
const (ScopeModel RubricScope = "model"; ScopeWorkflow RubricScope = "workflow")
type Category string
// Categories (frozen, issue #168): "pass", "wrong_language", "wrong_api_or_dependency",
// "logic_error", "security_defect", "incomplete_implementation", "unrequested_change",
// "build_or_test_failure", "style_bloat", "convention_violation"
type Severity string
// Severities (frozen): "cosmetic", "functional", "blocking", "critical"; "" on pass rows
type Verdict struct {
    RubricID RubricID; Scope RubricScope; Category Category; Severity Severity
    Detail string; Confidence float64
}
// Owner: 01-foundations/01-rubric-catalog.md
// Consumers: 01-foundations/02-verdict-store.md, 02-capture/01-verdict-emission.md,
//            03-surfaces/* (names only), 04-ship/01-e2e-suite.md
```

### C2: Verdict store (`internal/metrics/review_verdicts.go`)

```go
type ReviewVerdictRecord struct {
    StepID, TaskID string; Cycle int; IsFinal bool
    RubricID string; Scope string; Category string; Severity string
    ReviewedModelID, JudgeAgentID, JudgeModelID string
    OrchestratorAgentID, OrchestratorModelID string
    Confidence float64; TraceID string; Timestamp time.Time
}
func (s *Store) RecordReviewVerdict(rec ReviewVerdictRecord) error
type ReviewVerdictQuery struct { Since time.Time; GroupBy string /* model|judge|category|rubric */; IncludeWorkflow bool }
func (s *Store) QueryReviewVerdicts(q ReviewVerdictQuery) ([]ReviewVerdictRow, error) // one row per group + count
func (s *Store) FinalVerdictsForStep(stepID string) ([]ReviewVerdictRecord, error)
// Table: model_review_verdicts(id, timestamp, step_id, task_id, cycle, is_final,
//   rubric_id, scope, category, severity, reviewed_model_id, judge_agent_id,
//   judge_model_id, orchestrator_agent_id, orchestrator_model_id, confidence, trace_id)
// Append-only; NEVER added to retentionTables (store.go:727 purge list).
// Rollups: aggregation queries over this table; model_performance is NOT reused
// (its key (model_id, provider, agent_id, period_start) has no category dimension).
// Owner: 01-foundations/02-verdict-store.md
// Consumers: 02-capture/01-verdict-emission.md, 03-surfaces/01-http-endpoint.md,
//            03-surfaces/02-cli.md, 04-ship/01-e2e-suite.md
```

### C3: Serving-model join (stamping + query rule)

```go
// Step-scoped trace id format: "stp-" + stepID, stamped on the IMPLEMENTER agent's
// LLM calls for that step via agent.WithTraceContext on the step-job ctx
// (pattern: internal/agent/handler.go:917 mint seam; stamp site loop.go:7405-7406).
// Blame rule: the reviewed model = the model_id of the implementer agent's FINAL
// SUCCESSFUL llm_calls row for the step's trace id (rotation/failover may produce
// several). The judge loop's own calls are excluded by agent_id.
// Owner: 02-capture/02-trace-stamping.md (stamping) + 01-foundations/02-verdict-store.md (join query)
// Consumers: 02-capture/01-verdict-emission.md, 04-ship/01-e2e-suite.md
```

### C4: Verdict emission and persistence

```go
// ReviewResult gains: Verdicts []modelrating.Verdict (additive, omitempty).
// publishReviewEvent (internal/agent/review_manager.go:909-946) adds payload keys:
//   "verdicts": [{rubric_id, scope, category, severity, detail, confidence}...],
//   "reviewed_model_id", "judge_agent_id", "judge_model_id", "cycle", "is_final"
// Collector recordReviewMetrics (internal/metrics/collector.go:408-420) unmarshals
// the new keys and calls Store.RecordReviewVerdict per verdict.
// Producer-site rule (issue #168): ReviewManager + review_evidence_gate.go EMIT;
// review_tools.go (inline review) and pair_manager.go are EXCLUDED in v1 (reason
// recorded in docs/workflows/model-rating.md).
// Owner: 02-capture/01-verdict-emission.md
// Consumers: 04-ship/01-e2e-suite.md
```

### C5: HTTP surface

```
GET /api/v1/metrics/reviews?window=30d|7d|24h|all&group_by=model|judge|category|rubric
// 200 {"window": "...", "rows": [{group, verdicts, defects, pass_rate}], "workflow_excluded": N}
// Degrades to empty rows on store error (pattern: live_metrics_usage.go buildLiveMetricsResponse).
// Route registered in internal/comm/http/server.go mux beside :1672; provider wired
// via a WithReviewVerdictProvider seam (mirror of WithMetricsUsageProvider).
// Owner: 03-surfaces/01-http-endpoint.md  |  Consumers: 03-surfaces/03-flutter-panel.md, 03-surfaces/04-menubar-tab.md
```

### C6: CLI + UI consumption shapes

```
// CLI: `meept analytics reviews [--days N]` - readable table: model, judge, category,
// severity counts, pass rate. Opens metrics DB via cmd/meept openMetricsDB (analytics.go:57).
// Flutter: metrics_provider gains reviewGrades; MetricsPanel gains a grades section
// (by model, by judge). Route /tools/metrics unchanged (router.dart:212).
// Menubar: Analytics tab in menubar/MeeptMenuBar/Views/Analytics/ beside LiveMetricsView.swift;
// reads the same HTTP endpoint.
// Owners: 03-surfaces/02-cli.md, 03-surfaces/03-flutter-panel.md, 03-surfaces/04-menubar-tab.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | [01-foundations/orchestrator.md] | branch | none | - | - |
| 02 | [02-capture/orchestrator.md] | branch | 01 COMPLETE | - | - |
| 03 | [03-surfaces/orchestrator.md] | branch | 01 COMPLETE (C2); 02 for e2e only | - | - |
| 04 | [04-ship/orchestrator.md] | branch | 01, 02, 03 COMPLETE | - | - |

Branches dispatch in index order. Within a branch, the orchestrator owns grouping.

## Dispatch Protocol

Full recipe: skill `hierarchical-plan-execution`.

1. Create the isolation worktree (see Isolation Worktree section) BEFORE any dispatch.
   All dispatches, reviews, commits, and gates run in the worktree.
2. Per branch, in order 01 -> 02 -> 03 -> 04: follow the branch orchestrator's own
   Dispatch Protocol (test gate first, then concurrency groups, in-session review,
   orchestrator commits in the worktree).
3. `delegate_task` shape per canon: `tasks: [{goal: <imperative one-liner>, context:
   <full leaf text + contracts + conventions + INLINED anchors + worktree dir +
   "Do NOT commit. Do NOT run git add.">}]`.
4. Review in-session (main model) - NEVER a delegated reviewer. Gate-test diff must
   be empty on implementation review.
5. After ALL branches COMPLETE: run the Integration Test Plan in the worktree,
   then merge per the Isolation Worktree section.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied exactly (signatures, types, paths, column names)
- [ ] Naming contract holds: new surfaces say judge; no existing `reviewer*` identifier renamed
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] No scope creep; no debug artifacts, TODOs, placeholders
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Retention invariant: `model_review_verdicts` absent from `retentionTables` (store.go:727)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.27 (GOTOOLCHAIN from go.mod), Dart 3, Swift; one language per leaf.
- **Naming:** exported symbols documented; new IDs use `pkg/id.Generate()` - never
  `time.Now().UnixNano()` or `math/rand`.
- **Error handling:** wrap with `%w`, return early; no ignored errors; nil-guard every `Set*`.
- **No I/O under mutex; no bare `panic(err)`; two-value map assertions.**
- **Testing:** hermetic e2e tier for feature behavior (`e2e/suites/` + manifest row);
  unit tests only where the gate leaves place them. No new standalone unit-test files
  for features (repo rule).
- **Formatting:** gofmt before reporting; dart format; swift untouched style.

## Requirement Traceability

| Req | Source (issue #168, verbatim intent) | Plan element | Verified by |
|-----|--------------------------------------|--------------|-------------|
| R1 | Rubric IDs + defect taxonomy frozen | C1, 01-rubric-catalog.md | gate tests MR-* |
| R2 | Store reviewed model id, judge agent+model ids, rubric ID, category, severity, confidence, cycle, is_final, timestamp | C2, 02-verdict-store.md | gate tests VR-* |
| R3 | Serving model resolved from serving ledger; reviewer's own calls excluded | C3, 02-trace-stamping.md + 02-verdict-store.md | e2e capture scenario |
| R4 | Verdicts persist indefinitely; not in 24h sweep or 30d purge | C2 retention invariant | gate test VR-RET-1 |
| R5 | Query/aggregation by model, judge, category, rubric | C2 QueryReviewVerdicts | gate tests VR-Q* |
| R6 | Workflow-scope verdicts blame orchestrator, excluded from per-model aggregates | C1 scopes, C2 scope column | gate test VR-SCOPE-1 |
| R7 | GET /api/v1/metrics/reviews with configurable window | C5, 01-http-endpoint.md | gate test HTTP-* |
| R8 | `meept analytics reviews` readable table | C6, 02-cli.md | gate test CLI-* |
| R9 | Flutter panel grades by model and by judge | C6, 03-flutter-panel.md | gui-flows Dart suite |
| R10 | Menubar analytics tab, same data | C6, 04-menubar-tab.md | manual + build gate |
| R11 | Producer-site inventory; v1 exclusions stated | C4, docs leaf | docs review |
| R12 | Hermetic e2e suite + manifest row same commit | 04-ship/01-e2e-suite.md | pre-commit-e2e hook |
| R13 | docs/workflows/model-rating.md + AGENTS.md updates same commit | 04-ship/02-docs.md | feature-docs hook |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-foundations | PENDING | 0 | |
| 02-capture | PENDING | 0 | |
| 03-surfaces | PENDING | 0 | |
| 04-ship | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

Run in the worktree after all branches REVIEWED:

1. `make build` - all binaries compile.
2. `go test ./internal/modelrating/... ./internal/metrics/... ./internal/agent/... ./internal/comm/http/...` - green.
3. `make e2e-affected-area AREA=model-rating` (after manifest row lands) - suite green.
4. `python3 scripts/check_template_compliance.py docs/plans/model-rating-metrics-plan --strict-leaves` - `ALL TREES COMPLIANT: True` (authoring gate; re-run only if docs changed).
5. Manual: boot daemon in the worktree, run one reviewed step, `meept analytics reviews`
   prints the verdict row; `curl /api/v1/metrics/reviews` returns it.
6. `make graphs-check` - no new bus topics added; check must stay green.

## Test Gate

Per-branch test gates live in each branch orchestrator (`00T-` leaves, one gate per
implementation leaf pair). This root holds the cross-branch matrix:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Rubric catalog + taxonomy | C1 / 01-rubric-catalog.md | MR-* (00T-contracts) | e2e grade capture | PENDING |
| F2 | Verdict store + retention + rollup | C2 / 02-verdict-store.md | VR-* (00T-contracts) | e2e retention scenario | PENDING |
| F3 | Emission from review producers | C4 / 01-verdict-emission.md | EM-* (00T-capture) | e2e capture scenario | PENDING |
| F4 | Step-scoped trace join | C3 / 02-trace-stamping.md | TJ-* (00T-capture) | e2e capture scenario (join proof) | PENDING |
| F5 | HTTP reviews endpoint | C5 / 01-http-endpoint.md | HTTP-* (00T-surfaces) | e2e surface scenario | PENDING |
| F6 | CLI reviews table | C6 / 02-cli.md | CLI-* (00T-surfaces) | e2e surface scenario | PENDING |
| F7 | Flutter grades section | C6 / 03-flutter-panel.md | gui-flows Dart suite | gui-flows | PENDING |
| F8 | Menubar tab | C6 / 04-menubar-tab.md | n/a (Swift, build gate) | n/a (manual) | PENDING |
| F9 | Docs + AGENTS.md | R11-13 / 02-docs.md | n/a | n/a | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED
Delta log: (empty at authoring)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda.
Never edit a dispatched leaf document mid-execution.

## Notes

- Issue #168 (corrected 2026-11-23) is the source spec; its Resolved decisions are
  contracts, already reflected in C1-C6.
- The e2e manifest maps `internal/metrics/` to four suites today (manifest.json:310);
  the new suite row EXTENDS that list - it does not replace existing mappings.
- The menubar is Swift outside `internal/`; its gate is a successful build + manual
  visual check, recorded in the tracking table.
- No new bus topics: emission extends the existing `step.review_completed` payload
  additively. `make graphs-check` must stay green; if a topic change becomes
  unavoidable, STOP and escalate - it violates the frozen contract set.
- Collector is bus-subscribed at collector.go:260 (`step.review_completed`); the
  enriched keys are additive so the TUI/ChatHandler subscribers (tui/sidebar.go:602,
  handler.go:434) are unaffected.
