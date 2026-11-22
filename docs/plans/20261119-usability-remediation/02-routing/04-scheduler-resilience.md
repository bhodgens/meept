# Scheduler Resilience + Dir Binding - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T6) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.
> START WITH T1 INSTRUMENTATION (I1): prove the lease-leak site before
> fixing it. The bounded retry ticker (T2) is safe regardless of proof.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Blocked steps reschedule; step jobs bind the session dir; no shared-loop dir mutation
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 90K
- **Concurrency Group:** A
- **Audit references:** W12; R5/A2/A3/A4/A5; F-ORCH-28; I1

## Goal

Close the semaphore-hang class (ready steps never rescheduled), the
wrong-dir fallbacks (singleton mutation, step-job daemon-CWD fallback,
thread-scoped payload hint), and instrument lease accounting to prove or
refute the leak hypothesis under controlled concurrency.

## Context

- internal/agent/tactical.go:513 ScheduleReadySteps; ~571-583 semaphore
  skip at Debug; acquireSlotLease ~870; releaseStepSlot ~974; holdStepSlot;
  reschedule callers only event-driven (2172/2456/3616, orchestrator 400/632).
- internal/daemon/components.go:11165-11220 - AgentJobProcessor fallback to
  p.agentLoop; dir injection inside GetForTask success branch only;
  resolveStepWorkingDir LinkedSessions chain ~11031-11073 (thread unwrap at
  11051) vs payload-hint fallback ~11004-11013 (no unwrap).
- internal/agent/dispatcher.go:3689-3698 - singleton workingDir mutation;
  handler.go:3534-3537 twin.
- internal/agent/manager.go:109-141 - GetOrCreateWired eviction.

## Verified Anchors

- internal/agent/tactical.go:513 - ScheduleReadySteps
- internal/agent/tactical.go:870 - acquireSlotLease (re-grep)
- internal/daemon/components.go:11182 - dir injection success-branch-only
- internal/agent/dispatcher.go:3692 - singleton mutation warn

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Bounded retry ticker for ready-but-unscheduled steps (config
  orchestrator.step_retry_interval, default 15s, 0 disables);
  lease accounting counters (metrics seam); step-job dir binding or
  actionable refusal; no shared-dir mutation.

### What This Leaf Consumes
- C7 (exempt states - ticker must not schedule planning/awaiting tasks).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Lease instrumentation + repro | Modify: internal/agent/tactical.go (counters acquire/release/hold per agent+step); Create: internal/agent/tactical_lease_repro_test.go | `go test ./internal/agent/ -run TestLeakRepro -count=1 -timeout 120s` | counters absent | GREEN (instrumented) | Given 2/4/8 concurrent session waves; Then per-step lease ledger balances OR the leaking finalize path is named in the report; NO guess-fix |
| T2 | Retry ticker | Modify: internal/agent/tactical.go (ticker + ready-step sweep); internal/config/schema.go (step_retry_interval) | `go test ./internal/agent/ -run TestBlockedStepRetry -count=1` | gate T6 RED (no retry) | GREEN | Given semaphore-blocked wave with no in-flight steps; When ticker fires; Then steps schedule; blocked state surfaced in task.progress |
| T3 | Dir binding + no mutation | Modify: internal/daemon/components.go step-job region; internal/agent/dispatcher.go:3689 region; internal/agent/handler.go:3534 region; payload-hint thread unwrap | `go test ./internal/daemon/ ./internal/agent/ -run 'TestStepJobDirBinding|TestNoSingletonDirMutation' -count=1` | gate T6 RED | GREEN | Given GetForTask error/empty agent; Then fallback binds session dir via ctx or refuses with ErrNoWorkingDir-style guidance; GetOrCreateWired failure never mutates shared loop; thread-scoped payload hint resolves |
| T4 | Lease fix (CONDITIONAL on T1 proof) | Modify: the proven leak site only | `go test ./internal/agent/ -run TestLeakRepro -count=1` | T1 names the site | GREEN | Given the proven path; Then ledger balances under 8-session load; if T1 finds NO leak, document and skip (ticker alone closes the hang) |

## Self-Verification Checklist

- [ ] Gate T6 GREEN; gate diff empty
- [ ] T1 report names the leak site or refutes the hypothesis with evidence
- [ ] Live-gate: rig 8 concurrent sessions, no stuck ready steps after 60s
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. T1 lease-ledger evidence. 4. Live-gate. 5. Deviations.

## Review Checklist (For Review Agent)

- [ ] Ticker bounded + config-gated; never schedules exempt-state tasks
- [ ] No guess-fix: T4 only touches the T1-proven site
- [ ] Refusal path actionable (user-visible), never silent unbound run
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- F-ORCH-28 observed under CONCURRENT probe load; the clean serial bench
  did not hang - the leak (if real) is concurrency-triggered. T1 must
  reproduce under load, not serially.
