# Parked-Turns-as-Queue-Jobs (Adaptive Migration) - Implementation Orchestrator

> **For the executing agent:** orchestrator for this tree. Dispatch, review, re-dispatch, track. Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 3 leaves
- **Scope:** Move parked-turn persistence onto the job queue behind an opt-in flag (design amendment 3 of the 20261109 state-machine tree). TurnParker stays as the API; its STORAGE becomes queue rows when enabled.
- **Execution:** worktree plan/20261110-followups (branch), tree docs at docs/plans/20261110-parked-turns-as-jobs/.

## Goal

C1's bug class (two owners, one store) dies structurally: parked turns
become queue jobs with `next_retry_at` as the wake time, claimed by ONE
dispatcher (the parker identity survives as `claimed_by`). The migration
is ADAPTIVE, not big-bang: `parking.queue_backend` opt-in default false;
disabled = byte-identical TurnParker path (repo opt-in-defaults rule).
The old parks.db path is NOT deleted in this tree — deletion is a
follow-on once the flag has shipped.

## Architecture

TurnParker gains a backend seam: `parkBackend` interface with two impls —
sqliteParkBackend (today's behavior, wrapped) and queueParkBackend (new:
INSERT job type `parked_turn`, payload in job payload JSON, wake via
next_retry_at, owner in claimed_by, give-up = queue retry accounting).
Drain: queueParkBackend.Start subscribes claimed jobs of type parked_turn
and routes to the SAME resumeFunc callbacks. MaxAttempts maps to job
max_retries (cap give-up = moveToDead path? NO — parked-turn give-up must
route to the give-up callback, not dead-letter; use FailAttempt's
retry-exhaustion branch wired to the callback). Budget/gate/throttle/quota
owners all key through claimed_by values from C-K1 (quota-watcher,
throttle, gate, budget).

## Interface Contracts

### C-A1: parkBackend seam
```
type parkBackend interface {
    Save(ctx context.Context, rec ParkedTurnRecord) error
    Delete(ctx context.Context, kind ParkKind, key string) error
    LoadOwned(ctx context.Context, kind ParkKind, owner string) ([]ParkedTurnRecord, error)
    Arm(ctx context.Context) // start drain/claim loop
}
func (p *TurnParker) SetBackend(b parkBackend)
```
### C-A2: parked_turn job shape
```
type: "parked_turn"; payload JSON = ParkedTurnRecord (kind, key, owner,
class, resume_at->next_retry_at, attempt, max_attempts, park payload blob).
Claim honors next_retry_at (verify ClaimNextForAgent respects it — leaf 01 T1 pins).
```

## Child Index

| # | Document | Type | Deps | Est. Context | Group |
|---|----------|------|------|--------------|-------|
| 00T | 00T-gate-tests.md | leaf (gate) | none | 30K | A |
| 01 | 01-backend-seam.md | leaf | none | 50K | A |
| 02 | 02-queue-backend.md | leaf | 00T, 01 | 60K | B |
| 03 | 03-parity-test.md | leaf | 02 | 45K | B (serialized after 02) |

## Dispatch Protocol

Standard tests-first per skill; leaves serialize on parked_turn.go (all
touch it) — ONE at a time. Leaf 02+03 may pair only if leaf 03 files are
test-only additions.

## Review Checklist

- [ ] disabled flag = byte-identical behavior (parity test proves)
- [ ] owner semantics preserved (no cross-parker resume, both backends)
- [ ] give-up routes to callback, never dead-letter
- [ ] restart test green on BOTH backends (C1 killer ported)
- [ ] no gate/AGENTS drift; docs same-commit

## Coding Conventions

Repo AGENTS.md; -p 2; opt-in defaults per docs/agents/opt-in-defaults.md;
fix comments cite tree 20261110-parked-turns-as-jobs + audit finding C1.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | Design amendment 3 (master 20261109) | 01-03 | TestParkQueueBackendParity, TestRestartRearmSingleOwner both backends |
| R2 | opt-in default false, byte-identical | 03 | parity + config gate tests |
| R3 | give-up semantics preserved (M-4-park) | 02 | TestQueueBackendGiveUpRoutesToCallback |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests | TESTS_VALIDATED | 1 | committed RED (4 pins); parent-verified build-fail on target symbols only |
| 01-backend-seam | COMPLETE | 1 | committed fb47ab4c; seam+wrapper refactor, gate1 GREEN (scratch), frozen gates zero-diff; clock deviation adjudicated w/ constraint |
| 02-queue-backend | COMPLETE | 1 | committed 3a4964a3; owner-as-claimed_by + due_at-sentinel unclaimable design (no queue-pkg edits); 4 gates parent-verified PASS; flag-off byte-identical |
| 03-parity-test | COMPLETE | 1 | committed + merged; C1 restart parity port GREEN (2.16s), flag-off identity, no-worker-claim sentinel; -race stable -count=3 after real timing-flake fix |

BRANCH MERGED TO MAIN: de39440e (includes main-side fakeEmployeeResolver test fixup + post-merge gates: make test RC=0, graphs-check fresh). Flag parking.queue_backend default false; C1 bug class now structurally impossible on the queue backend.

## Integration Test Plan

`go test -p 2 ./internal/agent/ ./internal/queue/ ./internal/daemon/ -run 'Park|park'`
+ full suite; graphs regen if bus edges change (queue.job.* reuse, expect none new).

## Test Gate

| # | Feature | Source | Unit tests | E2E | Status |
|---|---------|--------|-----------|-----|--------|
| F1 | backend seam | 01 | TestParkBackendSeamWrapped (existing parker tests green through seam) | n/a | COMPLETE (merged de39440e; leaf fb47ab4c) |
| F2 | queue backend | 02 | TestQueueBackendSaveLoadDelete / GiveUp | restart re-arm parity | COMPLETE (merged de39440e; leaf 3a4964a3) |
| F3 | flag parity | 03 | TestParkingQueueBackendDefaultFalse | byte-identical suite | COMPLETE (merged de39440e; merge/docs evidence fd17e145 / 3a4964a3) |

## Open Questions

- Q-A1: give-up on exhausted attempts — dead-letter rows would leak
  parked payloads to operator surfaces. Default: route to callback (this
  tree's pin). Owner decision needed only if you prefer dead-letter.

## Frozen Documents

Leaves freeze at dispatch; corrections as dated addenda.

## Notes

Audit anchor: docs/plans/20261109-statemachine-remediation-audit-findings.md C1.
C-K1 owner semantics (merged 20261109) are the compatibility contract:
queue rows MUST carry owner in a claimable field.
