# Tier 3 Autonomous GoalLoop — Implementation Plan (master)

## Goal

Implement Tier 3 (Autonomous) for the AI Employee GoalLoop per
`docs/superpowers/specs/2026-06-23-ai-employee-design.md` (spec lines 298-300):
tier 3 employees run ASSESS → EXECUTE immediately, with no PendingApproval
signoff gate. Authority boundaries and `escalation_triggers` in the
constitution are the only gates.

Today `GoalLoop.Decide` (internal/employee/goal_loop.go:818) returns
`fmt.Errorf("tier 3 not yet implemented")`.

## Architecture Overview

- `GoalLoop` already branches on `AutonomyTier`. Tier 1 = assess→execute
  (implicit single-step plan, auto-approved). Tier 2 = assess→plan(pending)→
  approve→execute. Tier 3 = tier 2's assess + tier 1's immediate execution,
  PLUS per-candidate escalation-trigger gating before execute.
- The existing `PreExecChecker` (internal/employee/enforcement.go) already
  enforces constitution constraints per tool call at execute time. Tier 3
  adds a plan-level escalation check BEFORE Execute: if a candidate matches an
  EscalationTrigger, it is routed to Plan signoff (tier 2 path) instead of
  executed directly.
- All tier-blind machinery (scheduler, goal store, bot runner, reflect,
  metrics) is reused unchanged.

## Interface Contracts

Shared across all leaves:

```go
// internal/employee/goal_loop.go
// decideTier3(ctx, trigger, logger) error — assesses candidates, then for
// each candidate:
//   1. Enforce MaxActivePlans cap (same as tier 2).
//   2. Check ShouldEscalate(candidate, constitution): if true, create a
//      pending plan via l.Plan (tier 2 path) instead of executing.
//   3. Otherwise execute immediately: PlanRef{ID: id.Generate("gloop_"),
//      State: "executing", Prompt: candidate.Prompt, ApproverID: "system"},
//      then l.Execute + l.Reflect (identical to tier 1 flow).
//   4. Track consecutive failures via the existing synthetic-result path.
// Decide() switch case Tier3Autonomous calls decideTier3.

// internal/employee/enforcement.go
// ShouldEscalate(c *Constitution, candidate CandidatePlan) (bool, string)
//   — returns (true, reason) when the candidate matches any
//   ConstitutionalConstraints.EscalationTrigger. Matching semantics:
//     on=risk_level: Match is a RiskLevelCeiling band string; escalate when
//       candidate risk >= band.
//     on=tool/action/cost: substring/glob match against candidate metadata
//       (candidate.Prompt for tool/action; cost uses the candidate's
//       estimated cost if present, else no match).
//   Empty EscalationTriggers => never escalate.

// CandidatePlan (existing type, internal/employee/goal_loop.go) gains NO new
// required fields; risk estimation uses the existing RiskEstimator if wired,
// else "medium".
```

CLI/config surface (leaf 03):
- No new config keys: tier is already on the constitution
  (`AutonomyTier: Tier3Autonomous` in the employee def JSON5).
- `meept agents show` already renders the tier; add tier-3 escalation events
  to `meept agents audit` output via existing audit log.
- Metric: reuse `employee.invocations` counter with `tier="3"` tag; add
  `employee.tier3.escalated` counter.

## Coding Conventions

- Go, table-driven tests, no I/O under lock (mutexio analyzer), IDs via
  `pkg/id.Generate` (never time/rand — predid analyzer).
- Every feature must be wired: core logic + at least one interface (CLI/TUI/
  HTTP) + agent wiring + tests. Feature-docs pre-commit hook requires
  `docs/workflows/employees.md` update when `internal/employee/` is touched.
- Implementation agents do NOT commit. Orchestrator commits per-leaf after
  in-session review passes.

## Child Index

| Doc | Scope | Est. context | Depends on |
|-----|-------|--------------|------------|
| 01-decidetier3-core.md | decideTier3 in goal_loop.go + tests | ~45K | — |
| 02-escalation-gate.md | ShouldEscalate in enforcement.go + integration + tests | ~40K | 01 |
| 03-wiring-cli-docs.md | metrics, audit surfacing, docs/workflows/employees.md, end-to-end test | ~35K | 01, 02 |

01 runs first; 02 after; 03 last. No parallelism available (shared file
goal_loop.go between 01 and 02).

## Dispatch Protocol

For each child, in order:
1. Dispatch implementation agent via delegate_task with the child document
   content + this contract section as context. Include: "Do NOT commit.
   Do NOT run git add. Write code, run tests, report results only."
2. Review IN-SESSION (main model): read changed files, run
   `go test ./internal/employee/... -race`, verify contracts.
3. Re-dispatch on gaps (max 3 iterations).
4. Commit: `git add <exact files>` + `git commit -m "feat(employee): <leaf>"`.
5. Update the tracking table.

## Completion Tracking Table

| Doc | Status |
|-----|--------|
| 01-decidetier3-core.md | COMPLETE (ae232989) |
| 02-escalation-gate.md | COMPLETE (c3185b6f) |
| 03-wiring-cli-docs.md | COMPLETE (c9edfe37) |

## Integration Test Plan

After all leaves: run `go build ./... && go test ./internal/employee/... ./internal/bot/... -race`
plus `make lint-ci`. End-to-end: register a tier-3 test employee with an
escalation trigger, fire a trigger event, verify immediate execution for
non-matching candidates and pending-plan creation for matching ones
(covered by leaf 03's integration test with a real GoalLoop).

## Review Checklist

- [ ] Decide(Tier3Autonomous) no longer errors
- [ ] Escalation-trigger match routes to Plan signoff, not execute
- [ ] MaxActivePlans cap enforced per candidate
- [ ] Consecutive-failure auto-pause still applies
- [ ] No new config keys; constitution JSON5 unchanged in shape
- [ ] docs/workflows/employees.md updated (feature-docs hook)
- [ ] All tests race-clean

## Open Questions

None — spec lines 298-300 define behavior; constraints already in
ConstitutionalConstraints.
