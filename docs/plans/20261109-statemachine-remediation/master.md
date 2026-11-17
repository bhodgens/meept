# Implicit State Machine Remediation - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 branch directories (01-queue, 02-scheduler, 03-plans-goals, 04-parked-turns, 05-voice)
- **Scope:** Fix every finding from the 2026-11-09 implicit state machine audit of meept @ 63a7cf90.

## Goal

The audit (report in dispatch context; findings re-verified at HEAD by the
parent) found 1 CRITICAL and 18 MAJOR findings plus ~15 minors across five
subsystems: job queue, scheduler delivery, plan/goal lifecycle, parked-turn
resume, and voice call lifecycle. Each branch remediates one subsystem.
Every finding ID below traces to exactly one leaf.

Audit provenance: findings were proven against committed HEAD 63a7cf90.
Before dispatching any branch, re-grep its cited anchors at current HEAD
(audit anchors drift; the sibling session's voice/employee wave may have
landed). If an anchor moved, update the leaf's Verified Anchors BEFORE
dispatch - the leaf doc is not yet frozen pre-dispatch.

## Architecture

No redesign: each finding is fixed in place at the smallest seam that
restores the invariant. Cross-cutting rules:
- Every persisted state write gains or keeps a CAS predicate (token, lease,
  or expected-from-state). No bare read-check-write survives.
- Every non-terminal persisted state has a revisiting path (sweeper,
  reclaimer, or operator surface). No state exists that nothing revisits.
- Every parker owns its persistence rows exclusively (parker identity).
- Exactly-once semantics follow the repo's existing patterns: effects-ledger
  PK claims, delete-before-callback parker drains, releaseOnce map deletes.

## Interface Contracts

### C1: ClaimProcessing signature (queue)

```
func (s *Store) MarkProcessing(jobID, claimToken string) error
// UPDATE jobs SET state='processing' ... WHERE id=? AND claim_token=? AND state IN ('claimed','processing')
// Owner: 01-queue/01-mark-processing-token.md; Consumers: internal/worker
```

### C2: Parker identity (parked turns)

```
ParkKind gains no new value; SQLiteParkStore rows gain an owner column:
func (s *SQLiteParkStore) SetOwnerFilter(owner string)   // load-scoped
Park records persist owner; Load(kind) returns only rows whose owner matches the loading parker.
// Owner: 04-parked-turns/01-parker-identity.md; Consumers: 04-parked-turns/02..04, daemon/components.go wiring
```

### C3: Synthesis reclaimer driver (plans)

```
func (m *PlanManager) StartSynthesisReclaimer(ctx context.Context, interval time.Duration)
// ticker loop: RecoverStrandedSynthesis + re-drive Synthesize for reclaim-to-approved plans
// Owner: 03-plans-goals/01-synthesis-reclaimer.md; Consumers: daemon.go wiring
```

### C4: Conditional plan transitions (plans)

```
func (s *SQLiteStore) UpdatePlanStateWhere(ctx, id string, from []PlanState, to PlanState) (bool, error)
// Single CAS primitive; manager.go Submit/Cancel/Reject/Confirm/complete/Revise migrate onto it.
// Owner: 03-plans-goals/03-plan-cas.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-queue/orchestrator.md | branch | none | - | A |
| 02 | 02-scheduler/orchestrator.md | branch | none | - | A |
| 03 | 03-plans-goals/orchestrator.md | branch | none | - | B |
| 04 | 04-parked-turns/orchestrator.md | branch | none | - | B |
| 05 | 05-voice/orchestrator.md | branch | sibling voice wave landing | - | C (BLOCKED-START) |

Branches 01/02 run together (group A), then 03/04 (group B), 05 dispatches
only after the sibling session's voice/employee tree lands on main.

## Dispatch Protocol

Per references/tests-first-gate.md and the skill's standard protocol:
Phase 0 test gates per branch (tests-only, RED), in-session validation
against each branch's Test Gate matrix, commit gate tests, then parallel
implementation dispatch, in-session review, orchestrator commit per leaf,
integration review per branch, final merge per the isolation-worktree rule.

FILE-SERIALIZATION OVERRIDES (execution errata 2026-11-09):
- Branch 01 implementation leaves 01/02/03/04 ALL modify
  internal/queue/store.go — they dispatch ONE AT A TIME (their A/B group
  letters describe ordering, not parallelism).
- Branch 03 leaves 04/06 both touch manager.go — serialize (04 before 06).
- Branch 03 leaf 06 T6 touches employee store schemas — after leaf 05.
- Scheduler (02) and parked-turns (04-branch) leaves share no files with
  queue or plan leaves and may pair with one of those in any wave.

- Execution runs in an isolation worktree:
  `git worktree add .hermes/worktrees/20261109-statemachine-remediation -b plan/20261109-statemachine-remediation`
- Batch size: 2 concurrent implementation leaves (user standard cap).
- Each dispatch context carries: full leaf text, branch contracts, repo
  coding conventions (below), test commands with `-p 2` port-exhaustion cap,
  and the no-commit / no-git-add rule.
- Verification re: audit anchors - each leaf lists anchors to re-grep at
  dispatch time; anchors are quoted from the audit at 63a7cf90.

## Review Checklist

Per-child in-session review (main model, never delegated):

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied exactly
- [ ] The specific audit finding's defect class is closed (not just the symptom)
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] New/changed state writes are CAS-guarded; no new bare `WHERE id=?` state write
- [ ] `go build ./...` + `go vet` on touched packages clean; `gofmt` applied
- [ ] No debug artifacts, TODOs, or line-number corruption
- [ ] AGENTS.md package invariants touched by the change are updated in the same commit (repo rule)

Output: APPROVED or specific gaps.

## Coding Conventions

- Go 1.x (toolchain pinned by Makefile); stdlib style; `slog` logging.
- Errors: `fmt.Errorf("...: %w", err)` wrapping; sentinels for caller matching.
- Tests: table-driven `_test.go` beside the code; run with `go test -p 2`
  (port-exhaustion cap - never unbounded `./...` on macOS).
- DB: modernc sqlite; DSN pragmas only via `_pragma=` form; transactions via
  `BEGIN IMMEDIATE` where the code says IMMEDIATE.
- Comments state invariants, cite finding IDs (audit 2026-11-09) in the
  fix comment.
- gofmt + `go vet` before reporting; no `//nolint` without reviewer sign-off.

## Requirement Traceability

| Req | Source (audit finding) | Plan element | Verified by |
|-----|------------------------|--------------|-------------|
| R1 | Queue C1..F9 (M-1..M-4, F6..F9) | 01-queue leaves 01-04 | gate tests T-Q* |
| R2 | Scheduler M-5 (F5) | 02-scheduler/01-crash-window | gate test T-S1 |
| R3 | Plans/goals M-6..M-11, F1,F2,F5..F13,F16 | 03-plans-goals leaves 01-06 | gate tests T-P* |
| R4 | Parked C1, M-12..M-13, F2..F9 | 04-parked-turns leaves 01-04 | gate tests T-K* |
| R5 | Voice 1-11 | 05-voice leaves 01-04 | gate tests T-V* |
| R6 | Could-not-verify items (scheduler_jobs re-driver check, activity-tracker feeder, splice error path) | leaves 01-synthesis-reclaimer T4, 05-voice/03 T3, master integration plan | verification commands in leaves |

Rule: every audit finding maps to a leaf task. DEFERRED rows are listed in
each branch's Open Questions with the named decision owner.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-queue | COMPLETE | 1+ | merged e729937d (2026-11-10); leaves 6e73912b/328945fb/cf83d64e/12e93ef6(+wiring 43958b96)/6ef73902 |
| 02-scheduler | COMPLETE | 1 | merged e729937d; leaves 6e73912b/893109a8 |
| 03-plans-goals | COMPLETE | 1 | merged e729937d; leaves a59fbc4f/7c626b69/a94995c8/d191cb34/3b04d3ef/091cc90b/5950547d |
| 04-parked-turns | COMPLETE | 1 | merged e729937d; leaves 222578ac/bb24843f/fa5720fa/407932d6/80659bb5 |
| 05-voice | COMPLETE | 1+ | merged e729937d; sibling voice wave landed; leaves f9304084/47d03b60/97c67a59/117cd338/d0802617 |

## Integration Test Plan

- Per branch: `go build ./...` (module), branch-scoped `go test -p 2 ./internal/<pkgs>/...`.
- Cross-boundary: parked-turn restart re-arm integration test (park a
  throttle turn, restart parkers, assert exactly one owner resumes) -
  extends 04-parked-turns/01 T2 into a daemon-wiring test.
- Full suite before merge: `make test` (short mode, -p 2) + `make graphs`
  + `make graphs-check` (bus topology may change if events are added/removed).
- Repo gates on merged diff: analyzers (`make mutexio`, `make predid`),
  `golangci-lint` scoped to touched packages.

## Test Gate

Test leaves per branch (00T-), dispatched in Phase 0, validated in-session.

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Queue CAS/sweeper/CHECK | 01-queue/* | per-leaf gate tests | n/a - store-level | VALIDATED (merged e729937d; branch gate rows VALIDATED) |
| F2 | Scheduler crash window | 02-scheduler/01 | gate test | n/a | VALIDATED (merged e729937d; TestAdvanceLastWakeAfterDelivery/TestRecoverInFlightWalksHistory) |
| F3 | Plan/goal lifecycle | 03-plans-goals/* | per-leaf gate tests | n/a | VALIDATED (merged e729937d) |
| F4 | Parker identity + resume parity | 04-parked-turns/* | per-leaf gate tests | restart re-arm (integration) | VALIDATED (merged e729937d; integration re-arm not separately re-run 2026-11-16) |
| F5 | Voice terminal parity | 05-voice/* | per-leaf gate tests | n/a | VALIDATED (merged e729937d) |

## Open Questions (decision-gated leaves)

- Q1 (owner: user): Legacy token-less Complete surface (queue F8) - keep as
  documented operator contract or require token? Recommendation: keep, add
  warn log + doc. Default if unanswered: recommendation.
- Q2 (owner: user): PhaseFailed/PhaseConfirmed - add writers (recommended)
  or delete the enum values? Default: add writers.
- Q3 (owner: user): Goal retirement semantics - soft-delete only (align
  employee delete) vs hard-delete documented? Default: soft-delete.
- Q4 (owner: user): scheduler crash-window fix shape - move advanceLastWake
  after delivery (recommended) vs pre-execute receipt. Default: recommendation.

## Addendum (2026-11-16): Owner-timeout defaults RATIFIED (veto pending)

The owner (user) timed out on the Open Questions; the stated defaults are
RATIFIED as of 2026-11-16, veto pending. As-built verification at HEAD:

- Q1 (token-less Complete, queue F8): RATIFIED default = keep token-less
  Complete + warn log + doc. Verified: the legacy token-less path still
  exists (`internal/queue/store.go` Complete/CompleteAttempt with empty
  token degrades to the state-only predicate; `internal/queue/queue.go`
  PersistentQueue.Complete). WARN LOG NOT FOUND: no warn is emitted on the
  token-less completion path (existing warns cover refused/superseded/
  unknown-job cases only). NOTED AS FOLLOW-UP (not fixed, docs-only tree):
  add a warn log on the legacy token-less Complete call site.
- Q2 (PhaseFailed/PhaseConfirmed): RATIFIED default = add writers.
  Verified writers exist: `internal/plan/manager.go` sets PhaseConfirmed
  on plan confirm cascade (~L777, pending/in_progress -> PhaseConfirmed)
  and PhaseFailed via OnStepFailed failure-threshold accounting (~L1337,
  ~L1436); phases_test/transitions_gate_test.go pins both. No longer
  dormant enum values.
- Q3 (goal retirement): RATIFIED default = soft-delete only. Verified:
  `internal/employee/manager.go` Retire() sets state='retired' + retired_at
  (soft-delete, audit trail preserved); goal.go documents RetiredAt as
  soft-delete; hard-delete path does not exist.
- Q4 (scheduler crash-window shape): RATIFIED default = move
  advanceLastWake after delivery. Verified: `internal/scheduler/scheduler.go`
  advances the last-wake watermark ONLY on err==nil after Execute (and only
  for explicit ticks); M-5 fix comment in place.

Per-leaf landing proof: all branch leaf commits are ancestors of merge
e729937d (verified 2026-11-16 via git merge-base --is-ancestor): queue
6e73912b/328945fb/cf83d64e/12e93ef6/43958b96/6ef73902; scheduler 6e73912b/
893109a8; plans-goals a59fbc4f/7c626b69/a94995c8/d191cb34/3b04d3ef/091cc90b/
5950547d; parked-turns 222578ac/bb24843f/fa5720fa/407932d6/80659bb5; voice
f9304084/47d03b60/97c67a59/117cd338/d0802617. Merge e729937d (2026-11-10)
is in HEAD. Drift-resolution merge 724b9dba (2026-11-09) also in HEAD.

## Design Amendments (2026-11-09, user-approved direction)

This tree fixes defects in place. The following design improvements are
incorporated or deferred as noted:

1. **ADOPTED - Transition table for plans (branch 03, leaf 04 / C-P4):**
   the CAS migration lands as one declared from->to table with a single
   guarded entry point; leaf 04 T3 proves table == audited machine.
2. **ADOPTED - DB-declared invariants (branch 01 leaf 04 T2):** partial
   unique index enforcing at-most-one non-terminal row per natural job key,
   where a natural key exists (skip-with-note otherwise).
3. **DEFERRED - Parked turns as delayed queue jobs:** correct destination
   (kills the C1 bug class structurally), but a quarter-scale migration
   with session-context coupling. This tree's parker identity (04/01) is
   the compatible stepping stone. Schedule as a follow-on tree after this
   one lands.
4. **OPPORTUNISTIC - One lease abstraction:** branches touching expiry
   timers migrate them onto a single injected-clock lease type where the
   touch is already happening; no dedicated campaign.
5. **DEFERRED - Unified voice CallLeg interface:** largest refactor, sits
   in files the sibling session owns. Sequence after the voice wave AND
   this tree both land.
6. **REJECTED - State-machine frameworks and event sourcing:** no defect
   class removed; dependency weight. Clean machines stay implicit.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda.

## Notes

- The audit found 3 machines CLEAN (effects ledger, messages, idle reaper) -
  no leaf touches them; the exactly-once patterns there are the reference
  implementations for the fixes.
- Findings M-14..M-18 and voice minors sit in files the sibling session
  holds dirty; branch 05 re-anchors against landed main before dispatch.
- The audit skill + full findings live in the session transcript; a copy of
  the audit report should be committed as
  `docs/plans/20261109-statemachine-remediation-audit-findings.md` before
  wave 1 (source of truth for the traceability table; kept OUTSIDE the tree
  dir because check_plan_tree.py treats every tree .md as a leaf).
