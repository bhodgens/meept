# Synthesis Reclaimer + Approved Re-drive - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Periodic synthesis lease reclaimer with a REAL re-drive path (M-8, M-9): reclaim-to-approved plans get Synthesize re-invoked; mid-run lease expiry recovers without restart.
- **Dependencies:** 00T-plan-goal-tests.md
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Audit references:** M-8, M-9, F12 (partially)

## Goal

No plan strands in synthesizing or approved: the reclaimer is the
machinery-driven revisit for both, bounded by MaxSynthesisAttempts.

## Context

- internal/plan/manager.go:188 RecoverStrandedSynthesis (startup-only driver, daemon.go:1076)
- internal/plan/store_sqlite.go:461-491 ReclaimExpiredSynthesis (flips to approved; nothing re-drives)
- internal/plan/manager.go:684-746 Synthesize claim + attempt bound
- The missing edge: ClaimPlanSynthesis is called ONLY inside Synthesize; Synthesize ONLY from ApprovePlan; ApprovePlan requires pending_approval.

## Verified Anchors

Re-grep at dispatch. Also verify F12's open question FIRST (T4): check
internal/daemon/scheduler_jobs.go and any other callers of Synthesize for an
existing external re-driver — if one exists, shrink this leaf accordingly
and report the finding.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func (m *PlanManager) StartSynthesisReclaimer(ctx context.Context, interval time.Duration)
// ticker loop:
// 1. RecoverStrandedSynthesis (existing) — flips expired synthesizing leases
// 2. For each plan flipped to approved by reclaim: re-invoke Synthesize (respecting MaxSynthesisAttempts; attempt-exhausted -> ParkPlanSynthesis failed)
// Idempotent under concurrency: claim CAS remains the single-winner guard.
```

### What This Leaf Consumes

- C-P2 parent contract; existing RecoverStrandedSynthesis/ReclaimExpiredSynthesis.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Reclaim report | Modify: internal/plan/store_sqlite.go (ReclaimExpiredSynthesis returns reclaimed IDs or manager queries approved-with-expired-lease-recently-reclaimed) | gate tests | RED | reclaimer knows WHICH plans it reclaimed (current API returns count only) | Given 2 expired leases; When reclaim; Then both IDs returned |
| T2 | Re-driver | Modify: internal/plan/manager.go | gate TestSynthesisReclaimerRedrivesApproved GREEN | RED | GREEN: reclaimed-to-approved plan enters Synthesize again; exhausted -> failed park | Given reclaim-to-approved plan under attempt bound; When reclaimer pass; Then Synthesized or parked-failed |
| T3 | Ticker + wiring | Modify: internal/plan/manager.go (StartSynthesisReclaimer) + internal/daemon/daemon.go (replace startup-only call; keep startup invocation) | gate TestSynthesisReclaimerPeriodic + `go build ./...` | RED/absent | GREEN: interval default 5m; startup pass still runs; nil-safe when plan manager disabled | - |
| T4 | Verify no existing re-driver (audit could-not-verify item) | Read-only grep: `git grep -n Synthesize` across daemon/scheduler | grep | - | finding documented in leaf report; if an external driver exists, note it and keep the reclaimer as the only path (remove duplication) | - |
| T5 | AGENTS/docs | Modify: internal/plan docs + root AGENTS.md summary line if contract changed | grep | - | documented | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] Reclaimer idempotent; concurrent run cannot double-synthesize (claim CAS proof)
- [ ] Attempt bound respected across restarts (attempts column read, not memory)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations; T4 finding.

## Review Checklist (For Review Agent)

- [ ] Contract C-P2 exact
- [ ] No bus payload reshapes (plan.approved consumers unaffected)
- [ ] Attempt bound tested at boundary

## Notes

Do not make RecoverStrandedSynthesis itself re-drive (startup callers expect
count semantics); the reclaimer composes it.
