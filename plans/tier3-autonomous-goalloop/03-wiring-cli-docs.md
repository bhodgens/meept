# Leaf 03 — Metrics, audit surfacing, docs, end-to-end

DISPATCH INSTRUCTION: You are the implementation agent for this leaf. Read
this document fully, implement every task with TDD where testable, run
builds/tests/lint, and report results. Do NOT commit. Do NOT run git add.

- Parent: plans/tier3-autonomous-goalloop/master.md
- Scope: tier-3 metrics + audit surfacing + docs/workflows/employees.md +
  end-to-end wiring test.
- Dependencies: leaves 01 and 02 COMPLETE.
- Estimated context: ~35K.

## Interface Contract

- Metric `employee.tier3.escalated` (counter, tags employee_id) emitted from
  decideTier3's escalation branch via the existing SetEmitMetricFunc path.
  Leaf 01 already emits employee.invocations with tier="3".
- `meept agents audit <id>` output includes tier-3 escalation events. Audit
  events flow through the existing audit log; add an event record when a
  candidate is escalated (fields: plan_id, reason).
- docs/workflows/employees.md gains a "Tier 3 (autonomous)" section: behavior,
  escalation semantics, example constitution JSON5 with
  `"autonomy_tier": 2` (Tier3Autonomous), assessment_interval, one
  escalation trigger.
- Feature-docs pre-commit hook requires this file to exist/update whenever
  internal/employee/ changes — this leaf satisfies it for the whole tree.

## Tasks

### Task 1: Escalated metric test
Test the emit path: GoalLoop with metric func captured; escalated candidate
=> counter "employee.tier3.escalated" seen with employee_id tag. Implement in
decideTier3 escalation branch (leaf 01 code).

### Task 2: Audit event on escalation
Find the existing post-turn/pre-exec audit write path in enforcement.go /
manager.go; write an audit record at escalation time. Test: audit store stub
receives record containing plan_id and trigger Reason.

### Task 3: CLI surfacing
`internal/rpc/agents.go` or wherever agents audit responses are assembled:
include the new audit records (likely automatic if the audit query is
unfiltered). Add/extend one handler test proving an escalation record appears
in `meept agents audit` output shape.

### Task 4: Docs
Update docs/workflows/employees.md per contract. Include the spec reference
(2026-06-23-ai-employee-design.md lines 298-300). Note cost-match caveat
(cost triggers need candidate cost estimates; currently no-op).

### Task 5: End-to-end wiring test
File: internal/employee/goal_loop_integration_test.go (append)
Full stack within package: tier-3 constitution + gate wired (leaf 02) +
metric capture + audit capture. Fire Decide with mixed candidates. Assert all
four signals: executed candidate reflected, pending plan exists, escalated
metric emitted, audit record written.

## Self-Verification Checklist

- [ ] go build ./... clean
- [ ] go test ./internal/employee/... ./internal/rpc/... -race green
- [ ] make analyzers clean
- [ ] docs/workflows/employees.md updated
- [ ] No changes outside internal/employee/, internal/rpc/, docs/

## Review Checklist

- [ ] Metric name/tags match contract exactly
- [ ] Audit record carries plan_id + reason
- [ ] Docs include working example JSON5
- [ ] End-to-end test asserts all four signals in one run

Do NOT commit.
