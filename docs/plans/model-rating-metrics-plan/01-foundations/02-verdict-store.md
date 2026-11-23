# Verdict Store (model_review_verdicts) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate
> tests from 00T-contracts.md ALREADY EXIST - make them pass; never weaken a gate
> test. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** `model_review_verdicts` table + record/query/final API on
  `metrics.Store`, in a new `internal/metrics/review_verdicts.go`
- **Dependencies:** 00T-contracts (TESTS_VALIDATED); 01 catalog constants (INLINED
  in dispatch context if concurrent)
- **Estimated Context:** 55K
- **Concurrency Group:** B

## Goal

Append-only persistence for verdicts with model/judge/category/rubric aggregation
and the retention invariant. No agent-loop, HTTP, or CLI code in this leaf.

## Context

`metrics.Store` (store.go:47-101) owns the SQLite handle and schema init.
Retention: the 24-hour sweep deletes only `metrics_live` (store.go:711); the 30-day
purge iterates `retentionTables` (store.go:727: events, error_records, dispatch_log,
response_quality, lint_runs, test_runs). The new table must NOT be added there -
indefinite retention is the feature. Join rule C3 (master.md): the reviewed model is
resolved by the consumer via `(trace_id, agent_id)` over `llm_calls` - this leaf
stores `trace_id` on the row; the join query lives in 02-capture/02-trace-stamping's
surface, not here. This leaf DOES provide the blame resolution helper on the store,
because both it and the e2e suite need the exact same rule.

## Verified Anchors

- `internal/metrics/store.go:47-101` - Store struct, StoreConfig, NewStore
- `internal/metrics/store.go:246-268` - llm_calls DDL (column house style)
- `internal/metrics/store.go:727` - retentionTables slice
- `internal/metrics/store.go:227-240` - model_performance DDL (NOT reused; key lacks category)
- `internal/metrics/collector.go:856-887` - initAgentTaskSchema pattern for table init

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master.md C2:

```go
type ReviewVerdictRecord struct {
    StepID, TaskID string; Cycle int; IsFinal bool
    RubricID, Scope, Category, Severity string
    ReviewedModelID, JudgeAgentID, JudgeModelID string
    OrchestratorAgentID, OrchestratorModelID string
    Confidence float64; TraceID string; Timestamp time.Time
}
func (s *Store) RecordReviewVerdict(rec ReviewVerdictRecord) error
type ReviewVerdictQuery struct{ Since time.Time; GroupBy string; IncludeWorkflow bool }
type ReviewVerdictRow struct{ Group string; Verdicts, Defects int64; PassRate float64 }
func (s *Store) QueryReviewVerdicts(q ReviewVerdictQuery) ([]ReviewVerdictRow, error)
func (s *Store) FinalVerdictsForStep(stepID string) ([]ReviewVerdictRecord, error)
func (s *Store) ResolveReviewedModel(traceID, agentID string) (string, error)
// ResolveReviewedModel = C3 blame rule: model_id of the FINAL SUCCESSFUL
// llm_calls row (error=0) for (trace_id, agent_id), ordered by timestamp DESC/id DESC,
// limit 1. "" with nil error when the trace has no rows (never guessed).
func RetentionTables() []string // testability accessor over the retentionTables slice
```

DDL (frozen column names):

```sql
CREATE TABLE IF NOT EXISTS model_review_verdicts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    step_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    cycle INTEGER NOT NULL DEFAULT 0,
    is_final BOOLEAN NOT NULL DEFAULT 0,
    rubric_id TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    reviewed_model_id TEXT NOT NULL DEFAULT '',
    judge_agent_id TEXT NOT NULL DEFAULT '',
    judge_model_id TEXT NOT NULL DEFAULT '',
    orchestrator_agent_id TEXT NOT NULL DEFAULT '',
    orchestrator_model_id TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    trace_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_mrv_step ON model_review_verdicts(step_id);
CREATE INDEX IF NOT EXISTS idx_mrv_model_ts ON model_review_verdicts(reviewed_model_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_mrv_trace ON model_review_verdicts(trace_id);
```

### What This Leaf Consumes

- C1 vocab (`modelrating` package) for scope/category validation on Record.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | DDL + table init + RetentionTables accessor | Create: internal/metrics/review_verdicts.go; Modify: internal/metrics/store.go (init call beside initAgentTaskSchema pattern); Modify: store.go retentionTables -> RetentionTables() accessor (keep the slice; add method) | `go test ./internal/metrics/ -run 'VR-RET'` | table/method undefined | VR-RET-1 GREEN; existing store tests green | Given a fresh Store; When listing retention tables; Then model_review_verdicts absent; When opening a migrated DB; Then table exists |
| T2 | RecordReviewVerdict with vocab validation | Modify: internal/metrics/review_verdicts.go | `go test ./internal/metrics/ -run 'VR-REC'` | method undefined | VR-REC-1 GREEN | Given records with valid/invalid scope+category+severity combos; When recording; Then invalid rejected with error, valid rows persisted with UTC timestamp |
| T3 | QueryReviewVerdicts (4 group-bys, workflow exclusion) | Modify: internal/metrics/review_verdicts.go | `go test ./internal/metrics/ -run 'VR-Q'` | method undefined | VR-Q-* GREEN | Given mixed model/workflow rows; When grouping by model with IncludeWorkflow=false; Then orchestrator rows absent; by judge/category/rubric; Then counts and PassRate (pass rows denominator) correct |
| T4 | FinalVerdictsForStep + ResolveReviewedModel | Modify: internal/metrics/review_verdicts.go | `go test ./internal/metrics/ -run 'VR-FINAL'` | methods undefined | VR-FINAL-1 GREEN | Given cycles 0,1,2 for one step; When FinalVerdictsForStep; Then only cycle 2 rows. Given llm_calls rows for (trace,agent) incl. an error row then a success row; When ResolveReviewedModel; Then the success row's model_id, error row never |

`make test -p 2 ./internal/metrics/...` full-package green before reporting
(macOS: ALWAYS `-p 2`).

## Self-Verification Checklist

- [ ] Gate tests pass; gate files unmodified
- [ ] Column names byte-identical to the DDL above
- [ ] model_review_verdicts NOT in retentionTables
- [ ] Rollups via SQL aggregation over model_review_verdicts; model_performance untouched
- [ ] gofmt clean; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. `go test ./internal/metrics/ -run 'VR|MR' -v` tail; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] C2 verbatim (signatures, columns); C3 blame rule exact (final successful row, agent_id filter)
- [ ] Retention invariant holds; model_performance untouched
- [ ] No gate diff; no import cycle (metrics -> modelrating only)
- [ ] Full package tests green

Output: APPROVED or specific gaps.

## Notes

- Timestamps RFC3339 text like llm_calls (store.go:64 comments the byte-wise compare).
- GroupBy values: "model", "judge", "category", "rubric" - validate and error on others.
- PassRate = pass-category finals / total finals in the group; 0 rows => 0.
