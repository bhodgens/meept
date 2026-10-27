# Ticket->Goal Linkage — Implementation Leaf (02)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT use
> read_file on existing source files — explore with search_files or
> terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** `internal/intake/linkage.go`: IntakeKey dedup + the Linker
  (create / no-op / re-dispatch semantics on the employee goal store).
- **Dependencies:** 01-poller.md (Ticket exists)
- **Estimated Context:** 60K
- **Concurrency Group:** B

## Goal

The factory's identity layer: a ticket maps to exactly one live goal;
identical re-arrivals cost nothing; a changed mandate re-dispatches the
existing goal. Dedup rides the #64 effects ledger; linkage rides the
existing goal store (TriggerRef convention — no schema change).

## Context

The employee goal machinery: `employee.Manager.CreateGoal(GoalSpec)`
(internal/employee/manager.go:1984 — validates, enforces tier-sensitive
source rules, persists via GoalStore). `Goal.TriggerRef`
(internal/employee/goal.go:175) is a free-form string column — this tree
uses the `intake:` prefix convention (Contract 2). `GoalStore.Update`
(goal.go:714) persists mandate/state/health changes. Goal states:
GoalActive/GoalPaused/GoalRetired (goal.go:41-47); health:
GoalUnknown -> GoalHealthy/AtRisk/Broken (goal.go:84-97).

Key files:
- internal/employee/goal.go — GoalStore, Goal, states, health
- internal/employee/manager.go:1984 — CreateGoal
- internal/effects/effects.go — Run/Claim/Complete (the #64 pattern)
- internal/tools/builtin/github_pr.go — EffectMeta composition precedent

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 2 verbatim: `TriggerRefPrefix`, `IntakeKey`, `Linker`,
`NewLinker`, `EnsureGoal` — plus the manager seam:

```go
// internal/intake/linkage.go

// GoalCreator is the minimal manager surface Linker needs (satisfied by
// *employee.Manager; interface kept local so unit tests can stub it —
// same pattern as employee.Scheduler in scheduler_jobs.go).
type GoalCreator interface {
    CreateGoal(ctx context.Context, spec employee.GoalSpec) (*employee.Goal, error)
    ListGoals(ctx context.Context, employeeID string) ([]*employee.Goal, error)
}
```

### What This Leaf Consumes

```
github.com/caimlas/meept/internal/employee  (CreateGoal, GoalSpec, Goal)
github.com/caimlas/meept/internal/effects   (Claim/Complete via Run)
pkg/id (NOT needed here — goal IDs come from the store)
```

## Tasks

### Task 1: IntakeKey + Linker.EnsureGoal create path (F2)

**Objective:** create a trigger-source goal keyed to the ticket.

**Files:**
- Create: `internal/intake/linkage.go`
- Test: gate `TestLinker_EnsureGoal_Creates`, `TestIntakeKey_Stable`

Steps:
- `IntakeKey(repo, ticketID, body)` =
  `effects.EffectKey("intake_ticket_goal", repo, ticketID, sha256hex(body))`
  (sha256hex = the `prBodySHA256` pattern from github_pr.go:86 — copy
  the 6 lines, do not import from package builtin).
- `findLinkedGoal(ctx, employeeID, ticketID)`: scan ListGoals for
  TriggerRef == `intake:<repo>#<ticketID>` (prefix constant +
  composition helper `TriggerRefFor(repo, ticketID) string`).
- Create path: no linked goal -> `effects.Run` (Claim/Complete, meta
  Tool: "intake_ticket_goal", ProviderIdempotent: false, Payload: the
  marshaled ticket) wrapping the CreateGoal call. On `reused` (a prior
  completed claim), return the linked goal from a fresh TriggerRef scan
  (the goal row is the receipt).
- GoalSpec: Source "trigger", TriggerRef from the helper, Gate passed
  through when non-nil.

Run: `go test ./internal/intake/ -run "TestLinker_EnsureGoal|TestIntakeKey" -v` -> PASS.

### Task 2: dedup no-op path (F3)

**Objective:** identical re-arrival changes nothing.

**Files:**
- Modify: `internal/intake/linkage.go`
- Test: gate `TestLinker_Dedup_IdenticalBody`

Linked goal exists AND `sha256hex(body) == sha256hex(goal.Mandate)` ->
return the existing goal; no effects claim, no store write. (The hash
comparison is the cheap gate; the IntakeKey claim is what makes a
crash-window double-create impossible.)

### Task 3: re-dispatch path (F4)

**Objective:** changed mandate updates the goal, never duplicates.

**Files:**
- Modify: `internal/intake/linkage.go`
- Test: gate `TestLinker_Redispatch_OnBodyChange`

Linked goal exists AND body hash differs -> update
`goal.Mandate = t.Body`, reset `goal.State = GoalActive`,
`goal.Assess(GoalUnknown, now)`, persist via the manager's goal store
through GoalCreator (extend the local interface with the minimal
update method the real Manager already exposes — check
`internal/employee/manager.go` for the goal-update method; if none
exists on Manager, add `UpdateGoal` to Manager delegating to
GoalStore.Update, a 5-line addition, and note the deviation). No
effects claim (an update to an existing internal row is not an
external effect). Log the re-dispatch at Info (goal id, ticket ref,
no body content).

## Self-Verification Checklist

- [ ] Gate tests F2, F3, F4 PASS with `-race`
- [ ] Key derivation byte-identical to Contract 2
- [ ] No duplicate goals possible under any re-arrival ordering
- [ ] ErrNoToken-style fail-closed: nil ledger -> clear error before any
      CreateGoal call (the create path claims first)
- [ ] gofmt/vet clean; no unused code; no body content in logs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests green; no gate test modified
- [ ] TriggerRef format `intake:<repo>#<id>` everywhere (grep it)
- [ ] Dedup: identical body -> zero store writes
- [ ] Re-dispatch: mandate updated IN PLACE, state reset, health unknown
- [ ] effects meta carries Tool + Payload; ProviderIdempotent=false

## Notes

- If `employee.Manager` lacks a goal-update method, adding one is in
  scope for this leaf (5-line delegate to GoalStore.Update) — record it
  as a deviation, keep it minimal, do not refactor the store.
- Never log ticket bodies (mandates can contain sensitive material);
  log lengths and IDs only.
