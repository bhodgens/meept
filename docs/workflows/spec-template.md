# Feature Specification Template

Use this template when creating new feature specifications. This ensures all critical implementation details are captured, especially timestamp semantics that commonly cause domain logic bugs.

---

## Overview

**Feature Name:** [Name]
**Status:** draft | pending_approval | approved | rejected
**Created:** [Date]
**Last Updated:** [Date]

---

## Data Model

Document all data structures used by this feature. Include the source file location for each field to enable spec-to-code verification.

| Field | Type | Meaning | Source |
|-------|------|---------|--------|
| `LastAssessed` | `time.Time` | When goal health was last computed | `internal/employee/goal.go:173` |
| `Plan.CreatedAt` | `time.Time` | When plan was submitted for approval | `internal/planner/plan.go:51` |
| `Job.QueuedAt` | `time.Time` | When job was added to queue | `internal/queue/job.go:22` |

**Critical:** For timestamp fields, document what event triggers the timestamp. This is essential for timeout/deadline verification.

---

## Lifecycle States

Document state transitions and their associated timestamps.

```
1. Plan created     → State: `draft`
2. Plan submitted   → State: `pending_approval` (timestamp: `CreatedAt`)
3. Plan approved    → State: `approved` (timestamp: `ApprovedAt`)
4. Plan rejected    → State: `rejected` (timestamp: `RejectedAt`)
5. Plan completed   → State: `completed` (timestamp: `CompletedAt`)
```

### State Transition Diagram

```
draft → pending_approval → approved → completed
                ↓
            rejected
```

---

## Timeout Semantics

**CRITICAL SECTION:** This section prevents domain timestamp mismatch bugs (Bug Class #9).

For each timeout or deadline in the feature:

### Approval Timeout

**Spec Reference:** [line number in this document]
**Timeout Duration:** N hours

| Question | Answer |
|----------|--------|
| What timestamp measures timeout? | `Plan.CreatedAt` (submission time) |
| Why not other timestamps? | `LastAssessed` is goal health, unrelated to plan submission |
| Where is timeout enforced? | `internal/daemon/scheduler_jobs.go:350` |
| What happens on timeout? | Auto-reject with reason "approval_timeout" |

**Implementation Pattern:**
```go
// WRONG: Using wrong timestamp (goal health vs plan age)
if time.Since(goal.LastAssessed) > timeout {
    return StatusRejected
}

// RIGHT: Using correct timestamp (plan submission time)
if time.Since(plan.CreatedAt) > timeout {
    return StatusRejected
}
```

### Job Execution Timeout

| Question | Answer |
|----------|--------|
| What timestamp measures timeout? | `Job.StartedAt` (execution start) |
| Why not `Job.QueuedAt`? | Queue wait time is separate from execution time |
| Where is timeout enforced? | `internal/queue/worker.go:142` |
| What happens on timeout? | Job marked as failed, retry scheduled |

---

## Adapter Pattern for Cross-Package Field Access

When a sweeper or job needs to access fields from a struct in another package (and importing would cause a cycle), use the adapter pattern.

### Pattern Structure

**Step 1:** Define interface in the sweeper package:
```go
// internal/sweeper/plan_sweeper.go
type PlanLookup interface {
    GetPlan(ctx context.Context, id string) (*Plan, error)
}
```

**Step 2:** Implement adapter in a package that can import both:
```go
// internal/daemon/plan_lookup_adapter.go
type planLookupAdapter struct {
    pm *planner.Manager
}

func (a *planLookupAdapter) GetPlan(ctx context.Context, id string) (*Plan, error) {
    return a.pm.GetPlan(ctx, id)
}
```

**Step 3:** Inject via setter:
```go
// internal/sweeper/plan_sweeper.go
func (s *PlanSweeper) SetPlanLookup(lookup PlanLookup) {
    if lookup != nil {
        s.lookup = lookup
    }
}

// In wiring:
sweeper.SetPlanLookup(&planLookupAdapter{pm: plannerManager})
```

### When to Use

| Situation | Use Adapter |
|-----------|-------------|
| Sweeper needs field X from struct Y | Yes |
| Sweeper is in package A, Y is in package B | Yes |
| Import A → B creates cycle | Yes |
| Direct import possible | No, import directly |

### Example from Codebase

See `internal/daemon/employee_service_adapter.go` for a working example.

---

## Verification Checklist

Before implementation is complete, verify:

- [ ] All timestamp fields in Data Model have clear meaning
- [ ] Timeout semantics specify exact timestamp field used
- [ ] Code at specified location uses correct timestamp
- [ ] Adapter pattern used if cross-package field access needed
- [ ] Spec-to-code verification run (use `verify-plan-against-code` skill)

---

## Testing Requirements

Document test coverage requirements:

| Test Type | Coverage Required |
|-----------|-------------------|
| Unit tests | Core logic, edge cases |
| Integration tests | Timeout enforcement, state transitions |
| Mutation tests | At least 2 mutation variants |

---

## Rollback Plan

If feature causes issues:

1. [ ] Feature flag to disable
2. [ ] Data migration rollback
3. [ ] State recovery procedure
