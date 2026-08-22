# Leaf 01 — decideTier3 core

DISPATCH INSTRUCTION: You are the implementation agent for this leaf. Read
this document fully, implement every task with TDD, run the tests, and report
results. Do NOT commit. Do NOT run git add. Write code, run tests, report
results only.

- Parent: plans/tier3-autonomous-goalloop/master.md
- Scope: implement `decideTier3` in `internal/employee/goal_loop.go`; wire the
  `Decide()` switch; table-driven tests.
- Dependencies: none.
- Estimated context: ~45K.

## Interface Contract (what this leaf exposes)

```go
// goal_loop.go
func (l *GoalLoop) decideTier3(ctx context.Context, trigger TriggerEvent,
    logger *slog.Logger) error
```

Behavior:
1. Read constitution under lock; nil => error "tier3: no constitution
   configured".
2. Enforce MaxActivePlans cap exactly as decideTier2 does (DefaultMaxActivePlans
   fallback; skip when `!goal.CanAddActivePlan(maxActive)`), re-checked per
   candidate.
3. `candidates, err := l.Assess(ctx, trigger)`; empty => no-op nil.
4. Per candidate:
   a. Call the escalation gate hook. This leaf adds a field + setter on
      GoalLoop that leaf 02 will populate:

      ```go
      // EscalationGate reports whether a candidate must be routed to plan
      // signoff instead of immediate execution. Returns (escalate, reason).
      type EscalationGate func(c *Constitution, candidate CandidatePlan) bool

      func (l *GoalLoop) WithEscalationGate(g EscalationGate) *GoalLoop
      ```

      Default (nil gate) = never escalate (pure autonomy). When escalate is
      true: create pending plan via `l.Plan(ctx, candidate)` (tier 2 path),
      add to ActivePlanIDs same as tier 2, log at info with reason, continue
      to next candidate WITHOUT executing.
   b. Non-escalating: execute immediately — build
      `PlanRef{ID: id.Generate(goalLoopIDPrefix), State: "executing",
      Prompt: candidate.Prompt, ApproverID: "system"}`, call
      `l.Execute(ctx, ref)`; on error build synthetic failed result
      (identical to decideTier1 lines ~845-855).
   c. `l.Reflect(ctx, ref, result)`; warn-and-continue on reflect error
      (tier 1 pattern).
5. Emit metric via existing SetEmitMetricFunc path if wired: name
   "employee.invocations", tags employee_id, tier="3", outcome success/failure/
   escalated.

Do NOT modify Decide()'s Tier1/Tier2 cases. Only replace the Tier3Autonomous
case body with `return l.decideTier3(ctx, trigger, logger)` and delete the
"not yet implemented" comment block.

## Tasks (TDD)

### Task 1: WithEscalationGate setter test
File: internal/employee/goal_loop_test.go (append)
Table-driven: nil default; set then read via a package-private accessor or
behavioral test. Run: `go test ./internal/employee/ -run TestGoalLoopEscalationGate -v`.

### Task 2: decideTier3 happy path (no gate)
Test: constitution with AutonomyTier=Tier3Autonomous, stub executor capturing
executed prompts, no escalation gate. Fire trigger producing 1 candidate.
Assert: executor called once with candidate.Prompt, PlanRef.ApproverID=="system",
Reflect invoked, no pending plans left in ActivePlanIDs after completion
(follow tier 2's removal pattern).

### Task 3: MaxActivePlans cap
Constitution.MaxActivePlans=1, pre-seeded active goal at cap. Assert assess
skipped (executor not called), debug-level behavior mirrors tier 2.

### Task 4: Escalation gate routes to Plan signoff
Stub gate returning true for the candidate. Assert: l.Plan called (planner
stub records it), executor NOT called, ActivePlanIDs contains new pending
plan, info log emitted (assertable via slog test handler or just behavioral).

### Task 5: Failure path
Executor returns error. Assert synthetic failure result reaches Reflect,
consecutive-failure counter increments (existing machinery), and processing
continues to next candidate.

### Task 6: Switch wiring
Replace the Tier3Autonomous case in Decide(). Test: Decide with Tier3
constitution dispatches (no error) using the task-2 fixture.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `go test ./internal/employee/... -race` all green
- [ ] `go run ./tools/analyzers/mutexio/... ./internal/employee/...` clean
- [ ] `go run ./tools/analyzers/predid/... ./internal/employee/...` clean
- [ ] No changes outside internal/employee/

## Review Checklist (review agent)

- [ ] decideTier3 matches contract steps 1-5 exactly
- [ ] Cap re-checked per candidate (flood guard)
- [ ] Gate-nil default = full autonomy; gate present = signoff route works
- [ ] No lock held across LLM/executor calls (mutexio)
- [ ] IDs from pkg/id.Generate only

Do NOT commit.
