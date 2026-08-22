# Leaf 02 — Escalation gate (ShouldEscalate)

DISPATCH INSTRUCTION: You are the implementation agent for this leaf. Read
this document fully, implement every task with TDD, run the tests, and report
results. Do NOT commit. Do NOT run git add. Write code, run tests, report
results only.

- Parent: plans/tier3-autonomous-goalloop/master.md
- Scope: `ShouldEscalate` in internal/employee/enforcement.go; wire it as the
  GoalLoop EscalationGate where tier-3 employees are constructed
  (internal/employee/wiring.go / manager.go); integration test.
- Dependencies: leaf 01 (WithEscalationGate exists).
- Estimated context: ~40K.

## Interface Contract

```go
// enforcement.go
// ShouldEscalate reports whether a candidate plan matches any escalation
// trigger in the constitution and therefore must be routed to Plan signoff
// instead of immediate tier-3 execution. Returns the matching trigger's
// Reason when true.
func ShouldEscalate(c *Constitution, candidate CandidatePlan) (bool, string)
```

Matching semantics per EscalationTrigger.On:
- risk_level: Match is a RiskLevelCeiling band string. Estimate candidate risk
  via the existing risk estimation used by PreExecChecker; if no estimator is
  available for candidates, treat as "medium". Escalate when estimated band >=
  Match band using the existing RiskLevelCeiling ordering.
- tool: escalate if candidate.Prompt contains Match (case-insensitive
  substring). This is a heuristic at plan level; exact gating still happens
  per-tool-call in PreExecChecker.
- action: same substring semantics against candidate.Prompt.
- cost: parse Match as cents decimal string; escalate only if CandidatePlan
  carries an estimated cost field. If it does not, no match (document this).

Empty EscalationTriggers => (false, "").

Wiring: wherever GoalLoops are constructed for employees with
Tier3Autonomous constitutions (search manager.go/wiring.go for
NewGoalLoop/WithConstitution call sites), chain `.WithEscalationGate(...)`
wrapping ShouldEscalate so that escalate=true routes to signoff.

## Tasks (TDD)

### Task 1: ShouldEscalate table-driven tests
File: internal/employee/enforcement_test.go (append)
Cases: empty triggers; risk_level below/at/above band; tool substring hit and
miss; action miss; cost with and without estimate; unknown On value =>
no match + warn log.

### Task 2: Implement ShouldEscalate
Follow contract. Reuse RiskLevelCeiling ordering helpers already in
constitution.go; do not duplicate band comparisons.

### Task 3: Wiring
Find GoalLoop construction sites. Add WithEscalationGate wrapping
ShouldEscalate ONLY for tier-3 constitutions (tier 1/2 ignore the gate).
Unit test: constructing a tier-3 employee yields a loop whose gate escalates
on a constitution-matching fixture candidate; tier-2 construction unchanged.

### Task 4: Integration test
File: internal/employee/goal_loop_integration_test.go (append)
Real GoalLoop + stub executor/planner: constitution Tier3Autonomous with one
tool escalation trigger ("shell_execute"). Two-candidate assess result: one
matching, one not. Fire Decide once. Assert: matching candidate produced a
pending plan; non-matching executed immediately.

## Self-Verification Checklist

- [ ] go build ./... clean
- [ ] go test ./internal/employee/... -race green
- [ ] mutexio/predid analyzers clean
- [ ] No changes outside internal/employee/

## Review Checklist

- [ ] Band comparison reuses existing RiskLevelCeiling helpers
- [ ] Gate wired only for tier 3
- [ ] Cost semantics documented in code comment
- [ ] Integration test asserts both routing outcomes in ONE Decide call

Do NOT commit.
