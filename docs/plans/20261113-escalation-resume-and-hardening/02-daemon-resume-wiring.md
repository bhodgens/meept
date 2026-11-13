# 02 Daemon Resume Subscriber Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test. Do NOT commit - the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file,
> do NOT read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Subscribe "plan.approved" in the daemon under id "employee-plan-resume" and call Manager.ResumeApprovedPlan (C1) with the payload's plan_id/by.
- **Dependencies:** 00T gate tests committed; Contract C1 frozen (leaf 01 parallel — do NOT wait for it, code against the signature)
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Audit references:** plan.approved has no employee subscriber (docs/generated/bus-topology.md:103)

## Goal

Close the resume gap at the transport layer: when any approval surface (CLI plans approve, agents.goals.approve, HTTP/GUI) publishes `plan.approved`, a daemon subscriber resolves the plan's goal owner and invokes `Manager.ResumeApprovedPlan`. Fan-out stays best-effort: a resume skip or error never fails the approval flow that triggered it.

## Context

- Publisher: internal/plan/manager.go:563-566 publishes `m.publishEvent("plan.approved", map[string]any{"plan_id": planID, "by": by})`. DO NOT edit internal/plan.
- Existing subscriber precedent: internal/daemon/gate_park_wiring.go:266 — `c.msgBus.Subscribe("gate-resume-pump", "plan.approved")`. The other subscriber is internal/daemon/evolver_approval_bridge.go:75/266 (evolver-owned plans only). Read both; do not disturb them.
- The daemon holds the employee Manager (components.go) and the plan manager/store. Plan->goal linkage: prefer the plan record (created via CreatePlanWithGoal — check whether the record struct exposes GoalID); fallback: scan the goal store for a goal whose ActivePlanIDs contains planID (internal/employee/goal.go:257/511-527) — via the goal store seam the daemon already holds.
- Bus topic registry: new subscriber lines must appear in docs/generated/bus-topology.md — run `make graphs` and include the regenerated files in this leaf's commit scope.

## Verified Anchors

- internal/daemon/gate_park_wiring.go:266 - Subscribe("gate-resume-pump", "plan.approved") - the pattern to mirror
- internal/daemon/gate_park_wiring.go:10-16 - gate-park checkpoint contract - park resume is the OTHER path; at-most-once coordination rides the C1 once-map, not here
- internal/plan/manager.go:563-566 - publisher + payload keys
- internal/daemon/components.go - wiring site for daemon components (place construction near the gate-park wiring)
- internal/daemon/employee_service_adapter.go:334-353 - planCreatorAdapter -> CreatePlanWithGoal - where the goal linkage was created

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/daemon/employee_resume_wiring.go
// subscriber id: "employee-plan-resume"  topic: "plan.approved"
// Payload decode: map[string]any; plan_id must be a non-empty string, else
// WARN + drop (never panic on malformed payloads).
// Resolution: plan record -> GoalID if available, else goal-store scan by
// ActivePlanIDs. If NO goal owner resolves: INFO skip (a plan without a
// goal owner — e.g. evolver plans — is not this subscriber's business;
// leave evolver_approval_bridge as the only consumer for those).
// Call: employeeManager.ResumeApprovedPlan(ctx, planID, by) — errors are
// WARN-logged and dropped; the subscriber NEVER re-publishes or retries.
```

### What This Leaf Consumes

- C1 ResumeApprovedPlan — `func (m *Manager) ResumeApprovedPlan(ctx context.Context, planID, callerID string) error` — leaf 01 (parallel; signature frozen in master.md)
- msgBus Subscribe seam — existing

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement subscriber: decode payload, resolve goal owner, call C1, structured logs (subscriber id in every line) | Create: internal/daemon/employee_resume_wiring.go; Modify: internal/daemon/components.go (wire construction + Start/Stop alongside gate-park wiring); Test: gate internal/daemon/employee_resume_wiring_test.go | `go test -p 2 ./internal/daemon/ -run TestEmployeeResumeSubscriber_OnApproved_InvokesResume` | gate test RED (no subscriber) | GREEN | Given published {plan_id, by}; When processed; Then C1 called once with exact args |
| T2 | Malformed payload: missing/non-string plan_id -> WARN + drop, no panic, no C1 call | same files | `go test -p 2 ./internal/daemon/ -run TestEmployeeResumeSubscriber_MalformedPayloadSkips` | RED | GREEN | Given {"by":"x"} only; When processed; Then WARN, no call, no panic |
| T3 | Regenerate connectivity graphs (new subscriber must appear) and commit-scope them | Modify: docs/generated/bus-topology.md, docs/generated/bus-topology.json (+per-layer exports if the generator touches them) | `make graphs && make graphs-check` | subscriber absent from bus-topology.md | subscriber row present, graphs-check green | Given new subscriber; When graphs regenerate; Then "employee-plan-resume" listed under plan.approved |
| T4 | Docs: add the subscriber + resume flow to docs/workflows/ (matching employee/goal workflow page — find it under docs/workflows/, e.g. goal/employee doc) in the same change per repo doc policy | Modify: docs/workflows/<matching-page>.md | grep resumeApprovedPlan docs/workflows/ | page silent on resume | flow documented | Given feature landed; When docs read; Then approve->resume flow is described |

## Self-Verification Checklist

- [ ] Subscriber id exactly "employee-plan-resume"; topic exactly "plan.approved"
- [ ] No edits to internal/plan, internal/employee, gate_park_wiring.go, evolver_approval_bridge.go
- [ ] Malformed payload path never panics; C1 errors never retry
- [ ] components.go wiring has nil guards (typed-nil rule) and clean shutdown alongside siblings
- [ ] Graphs regenerated; graphs-check green
- [ ] Gate tests GREEN; `go build ./...` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Contract C2 exact (subscriber id, decode rules, skip semantics)
- [ ] components.go wiring placed/ordered like sibling subscriber wiring (boot path before F-D8 gate if applicable — follow gate_park_wiring precedent)
- [ ] Graphs diff contains ONLY the new subscriber rows
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Leaf 01 lands in parallel: this file references only the frozen C1 signature, so `go build ./...` will be red for internal/daemon until 01 merges — build within this package may still compile if the test stubs the manager interface; if the daemon test needs the concrete Manager, gate the test behind the same seam the gate_park tests use. Report which approach you took.
- If the plan record does NOT expose GoalID and the goal-store scan is used, note it in the completion report — a future leaf can add the direct link.
