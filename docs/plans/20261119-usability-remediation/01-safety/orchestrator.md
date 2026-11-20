# 01-SAFETY - Terminal Honesty, Queues, Delivery, Watchdog, Failure Paths

> **For the executing agent:** You are the branch orchestrator. Dispatch
> test-gate leaf first, validate in-session, then implementation leaves;
> review IN-SESSION (main model); commit only after review. Do NOT
> implement code yourself. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 7 implementation leaves
- **Scope:** Every request ends in an honest user-visible terminal; reject
  means reject; steering queues work; critical events survive load and
  reconnect; approval waits are not reaped; silent failure paths terminalize.

## Goal

Close the report's Tier-1 correctness/safety items (W1-W7) plus W24-W27.
This branch is the foundation: C1/C2/C3/C6/C7 contracts it produces are
consumed by every other branch. Maps to F-ORCH-12/15/23/26/27/28 (partial),
queue-gates #1/#2/#3/#6/#7/#8/#11, classifier-routing F6-1/F6-2,
audit-routing-failures top-10 #1/#2/#3/#4/#5/#8/#9.

## Architecture

Five leaves along package seams: rpc approval handler (01), agent queue
lifecycle (02), bus delivery + TUI reconnect (03), watchdog (04), failure
terminalization across tactical/pair/plan/park/panic (05). Leaves 01+02+04
are file-disjoint (group A); 03 touches rpc/proxy.go + tui/events.go
(group A, disjoint from 01/02/04); 05 touches tactical.go/pair_manager.go/
handler.go park+panic regions (group B - handler.go overlaps 01's relay
subscription region, so 05 dispatches AFTER 01 REVIEWED).

## Interface Contracts

Produces C1 (terminal payload extensions), C2 (approval RPC), C3 (queue
lifecycle), C6 (critical delivery), C7 (watchdog exemption) - full
definitions in ../master.md. Consumes: none (foundation branch).

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 70K | A |
| 01 | 01-reject-honesty.md | leaf | 00T | 60K | A |
| 02 | 02-queue-lifecycle.md | leaf | 00T | 85K | A |
| 03 | 03-critical-delivery.md | leaf | 00T | 75K | A |
| 04 | 04-watchdog-exemption.md | leaf | 00T | 50K | A |
| 05 | 05-failure-terminalization.md | leaf | 00T, 01 | 95K | B |
| 06 | 06-evidence-review.md | leaf | 00T | 55K | A |
| 07 | 07-replan-storm-plans-panic.md | leaf | 00T | 70K | A |

## Dispatch Protocol

Phase 0: dispatch 00T-gate.md (tests only, RED). Validate in-session vs
Test Gate matrix; commit gates; TESTS_VALIDATED.

FILE-SERIALIZATION OVERRIDE (gate leaf is ONE agent writing many packages):
the 00T gate is dispatched as a SINGLE subagent (not split) to avoid
shared-file races on e2e/manifest.json. If it times out, audit disk and
re-dispatch only the missing test files (pitfall 25).
Phase 1: dispatch 01+02+03+04+06+07 (group A, file-disjoint; 6 leaves -
split into batches of 2-3 per delegation cap). 06 (evidence review) touches
review_manager.go only; 07 (replan storm + plans panic) touches
ralph_loop.go/orchestrator.go ralph branch + plan_service.go/daemon.go
evolver wiring - disjoint from 01/02/03/04. NOTE: 07's orchestrator.go
ralph-branch region and 05's failure-terminalization both touch
orchestrator.go - serialize 07 before 05 (05 is group B already).
Phase 2: review each in-session; commit per leaf; then dispatch 05 (B).
Phase 3: integration - run branch verify commands; report COMPLETE to root.
Every brief: worktree abs path, consumed contracts, "Do NOT commit. Do NOT
git add. Explore with search_files/terminal cat; write once, never read
back." Max 3 review cycles per leaf, then BLOCKED.

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] C1/C2/C3/C6/C7 signatures exact; ws_events.schema.json updated (C1)
- [ ] Gate tests GREEN and unmodified
- [ ] Live-gate: scratch rig boot, one real turn, reject/approve/steer
      exercised via RPC; evidence cited
- [ ] No scope creep / debug artifacts / line-number corruption
- [ ] gofmt clean; no `_ = ` on added lines

## Coding Conventions

Per ../master.md Coding Conventions. Branch specifics: queue CAS changes
must keep the one-way Close semantics for the OLD queue object (fresh
instance per turn, not reopen-in-place - reopen races drain); bus critical
ring must preserve seq monotonicity across rings (single nextSeq counter);
watchdog exemption reads task state via the registry edge, never a store
query per tick (cost).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-reject-honesty | PENDING | 0 | |
| 02-queue-lifecycle | PENDING | 0 | |
| 03-critical-delivery | PENDING | 0 | |
| 04-watchdog-exemption | PENDING | 0 | |
| 05-failure-terminalization | PENDING | 0 | |
| 06-evidence-review | PENDING | 0 | |
| 07-replan-storm-plans-panic | PENDING | 0 | |

## Integration Test Plan

1. `go test -p 2 ./internal/rpc/... ./internal/agent/... ./internal/bus/...
   ./internal/tui/...` (worktree).
2. e2e: task-approval, steering, bus-delivery, approval-wait, failure-paths
   suites (new/extended by 00T) via `make e2e-fast-area AREA=<suite>`.
3. Live rig: boot scratch daemon; submit turn -> task.approve/task.reject
   roundtrip (reject must NOT execute - assert task state + tool ledger);
   steer mid-turn on turn 2 of a cached loop (must inject, full text);
   kill+restart daemon mid-turn (TUI re-subscribes, terminal still lands);
   approval-wait turn survives >600s without reap.

## Test Gate

Test leaf: 00T-gate.md. Matrix (branch slice of root F1-F5):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | reject honesty + relay | 01 T1-T3 | TestRejectDoesNotApprove, TestRejectedRelayTerminal | e2e/suites/task-approval | PENDING |
| F2 | queue lifecycle + remap + full text | 02 T1-T4 | TestFreshQueuePerTurn, TestCloneQueueIsolation, TestThreadRemapLookup, TestSteerFullText | e2e/suites/steering (extend) | PENDING |
| F3 | critical delivery + reconnect | 03 T1-T3 | TestCriticalRingNoTrim, TestBlockingPublishers, TestTUIResubscribe | e2e/suites/bus-delivery (new) | PENDING |
| F4 | watchdog exemption | 04 T1-T2 | TestApprovalWaitNotReaped, TestSealActivityTouch | e2e/suites/approval-wait (new) | PENDING |
| F5 | failure terminalization | 05 T1-T5 | TestPairFailureTerminal, TestPlanRequestErrorTerminal, TestSynthesisFailureEvent, TestParkEmptyReply, TestPanicRecovery | e2e/suites/failure-paths (new) | PENDING |
| F5b | evidence review gate | 06 T1-T3 | TestFileStepZeroEvidenceGate, TestFileStepWithEvidenceApproves, TestZeroEvidenceReason | e2e/suites/failure-paths (extend) | PENDING |
| F5c | replan storm ceiling + plans typed-nil | 07 T1-T3 | TestReplanHardCeiling, TestFailedTaskNeverReplans, TestFailureRelayedOnce, TestPlansTypedNil, TestNilSafeEvolverStore | e2e/suites/failure-paths (extend) | PENDING |

Delta log: (none yet)

## Open Questions

- 05 is the largest leaf (95K, 5 seams). If it times out, split per
  pitfall 25: audit disk, re-dispatch only missing halves (pair/plan vs
  park/panic).

## Notes

- W21 (GUI build) is NOT here - parallel branch fix/usability-gui-openexternal.
- F-ORCH-28 lease leak: 02-routing/04 owns instrumentation; 05 must NOT
  guess-fix the semaphore.
