# Activity Aggregator (Cross-Store Read) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A read-only `internal/activity` package that merges task steps,
  session messages, auditlog chain records, and employee audit findings into
  one queryable, sorted `ActivityRecord` stream.
- **Dependencies:** none
- **Estimated Context:** 75K
- **Concurrency Group:** A

## Goal

Operators cannot ask "what did every agent do, in order?" because activity
lives in four silos. This leaf builds the single read path: take existing
store handles, pull rows, map each to the common `ActivityRecord` shape,
merge, sort (chronological / grouped by task / grouped by plan), filter, and
paginate. STRICTLY read-only - no new tables, no writes to any store.

## Context

Meept is a Go agent daemon. Activity data lives in:
- tasks.db: `tasks` + `task_steps` + `task_state_transitions` (richest
  chronological source) via `internal/task` stores.
- sessions.db: `sessions` + `session_messages` via `internal/session`
  SQLite store.
- audit_chain.db: hash-chained `audit_log_chain` via `internal/auditlog`
  Store (verdict records today).
- employee audit findings: `employee_audit_findings` via the
  `internal/employee` AuditStore.

All four handles already exist on the daemon's `Components`. This package
never opens files itself - it is CONSTRUCTED with the handles (pattern:
any `internal/daemon/*_rpc.go` closure receiving components). Plan linkage
is INVERTED: `plans.task_id` points to its parent task (there is no
plan_id on tasks). For plan grouping, accept a caller-provided
`planID -> taskID` resolver function so this package does not import
`internal/plan` (the daemon wiring supplies the resolver from the plan
store). Findings carry their own plan_id.

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing - siblings move lines.

- internal/task/step.go:419-465 - task_steps schema (id, task_id, description, agent_id, job_id, state, result, sequence, review_verdict, created_at, updated_at...) + task_state_transitions - source rows for task_step records.
- internal/task/store.go:79-100 - tasks table schema (id, name, state, assigned_agent, created_at...) - join target for step records.
- internal/session/store_sqlite.go:123-133 - sessions table (id, name, conversation_id, employee_id...).
- internal/session/store_sqlite.go:136-144 - session_messages table (session_id, role, content, timestamp, agent_id) - source rows for session_message records.
- internal/auditlog/store.go:15-24 + internal/auditlog/chain.go:13-21 - audit_log_chain table + Record{Seq, Type, EmployeeID, Payload, At, PrevHash, RecordHash} - source rows for audit_record.
- internal/employee/enforcement.go:1657-1676 - employee_audit_findings table (id, employee_id, plan_id, turn_id, severity, checkpoint, violated_rule, evidence, detected_at, resolved_at, resolution, drift_score); AuditFinding struct at enforcement.go:376.
- internal/employee/drift.go:50-99 - CalculateDriftScore(findings, now, halfLife) - reuse, do not reimplement.
- internal/plan/store_sqlite.go:73 - plans.task_id column (inverted linkage).
- e2e/suites/audit-chain/audit-chain_test.go - pattern for tests against sandbox sqlite stores.

Read-side access note: store packages may not expose every needed SELECT;
check each store's existing public query methods FIRST. If a needed read is
missing, ADD a read-only query method to that store (same package, same
style, table-driven test) rather than opening the DB file directly - never
import modernc.org/sqlite from internal/activity.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/activity/types.go - C1 (byte-identical to master)
type ActivityRecord struct { Kind, At, AgentID, SessionID, TaskID, StepID,
    PlanID, State, Severity, Summary string + Detail map[string]string with
    json tags exactly as master C1 }

// internal/activity/aggregator.go
type PlanResolver func(ctx context.Context, planID string) (taskIDs []string, err error)
type Options struct { Now func() time.Time; PlanResolver PlanResolver }
func NewAggregator(tasks *task.Registry, steps *task.StepStore,
    sessions session.Store, chain *auditlog.Store,
    findings *employee.AuditStore, opts Options) *Aggregator

func (a *Aggregator) Query(ctx context.Context, q Query) ([]ActivityRecord, int, error)
type Query struct {
    Since, Until time.Time; Sort string // "chrono"|"task"|"plan"
    TaskID, PlanID, AgentID string; Kinds []string
    Limit, Offset int
}
// Sort semantics: chrono = At ascending, nil tiebreak by Kind;
// task = group by TaskID (empty last), At ascending within group;
// plan = group by PlanID (empty last, resolved via PlanResolver for
// task_step records), At ascending within group.
// total = count BEFORE Limit/Offset (for pagination UI).
// Missing optional handles (nil chain / findings) degrade to fewer kinds,
// never an error.
```

### What This Leaf Consumes

- Existing public read methods of internal/task, internal/session,
  internal/auditlog, internal/employee stores (add read-only queries to
  those packages only where a read does not exist; keep them in their
  owning packages).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Types + constructor with nil-handle degradation | Create: internal/activity/types.go, internal/activity/aggregator.go; Test: internal/activity/aggregator_test.go | `go test -p 2 ./internal/activity/` | package/types undefined | compiles; NewAggregator with ALL nil handles returns empty result, no error | Given zero handles; When Query; Then empty records, total 0 |
| T2 | task_step + session_message mappers from sandbox sqlite | Modify: read-only query additions if needed in internal/task, internal/session; Test: internal/activity/aggregator_test.go | `go test -p 2 ./internal/activity/ ./internal/task/ ./internal/session/` | mappers undefined | steps+messages map to records with correct Kind/At/IDs/Summary (envelope-stripped prose, <=200 runes) | Given seeded sandbox DBs (audit-chain suite pattern); When Query; Then merged chrono-ordered records |
| T3 | audit_record + finding mappers; severity carried; PlanResolver wiring for plan sort | Test: internal/activity/aggregator_test.go | `go test -p 2 ./internal/activity/` | sort modes fail | plan sort resolves plan->tasks via injected resolver; findings carry Severity + own plan_id | Given seeded chain + findings; When Sort=plan; Then records grouped by resolved PlanID |
| T4 | Filters + pagination + total-before-pagination | Test: internal/activity/aggregator_test.go | `go test -p 2 ./internal/activity/ -count=1` | filter cases fail | kinds/task_id/agent_id/since/until filter; Limit/Offset slice correctly; total independent of paging | Given 25 records; When Limit=10,Offset=10; Then 10 records, total 25 |

Each task: write the failing test first, watch it fail for the stated RED
reason, implement minimally, watch it pass. Never edit a test to pass.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C1 JSON tags byte-identical)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] ZERO writes to any store (grep your code for INSERT/UPDATE/DELETE -
      none may appear in internal/activity)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C1 type byte-identical to master contract
- [ ] Read-only guarantee verified (no write statements; no new tables)
- [ ] PlanResolver seam present (no internal/plan import)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- modernc.org/sqlite sandbox DBs in tests: copy the setup pattern from
  e2e/suites/audit-chain/audit-chain_test.go (in-memory or t.TempDir files).
- Summary fields MUST be user-safe prose: strip claims/evidence envelopes
  the way the chat path does (search StripClaimsEvidenceOrProse in
  internal/agent for the existing helper; if importing internal/agent is a
  cycle, reimplement the 10-line strip locally and say so in the report).
- Keep the package free of daemon/config imports; handles in, records out.
