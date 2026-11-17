# Parked-Turn Remediation - Implementation Orchestrator

> **For the executing agent:** orchestrator for this branch. Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test-gate leaf + 4 implementation leaves
- **Scope:** Close C1 (restart double-execution via kind collision) + M-12 (gate strand on rejection) + M-13 (throttle resume discards result) + M-4-park (unbounded quota re-park) + F5 (budget park no persistence/observability) + F6..F9 minors.

## Goal

Each parker owns its persistence rows; every park has a visible, bounded
exit; every resume delivers its result the way the forward path does.

## Architecture

Parker identity is the keystone fix (C2 in master): park rows record the
owning parker; Load returns only own rows; boot re-arm cannot cross-feed.
On top of that: gate parks gain a rejection-driven release; throttle resume
gains the quota resume's persist+push+terminal sequence; quota re-park
gains an attempt bound; budget parker gains persistence + quota_wait-topic
events.

## Interface Contracts

### C-K1 (master C2): Parker identity

```
// ParkedTurnRecord gains Owner string (persisted column).
func (s *SQLiteParkStore) SetOwnerFilter(owner string)
func (p *TurnParker) SetPersistenceOwner(owner string) // stamps Save, filters Load
// Owner: 01-parker-identity.md; Consumers: 02/03/04 + daemon/components.go wiring
```

### C-K2: Throttle resume delivery parity

```
// throttledTurnPayload gains TurnID string. resumeThrottledTurn, on success:
// persistExchange + client push + turn.terminal — identical sequence to resumeQuotaParkedTurn (handler.go:2720-2746).
// Owner: 02-throttle-resume-delivery.md
```

### C-K3: Gate release on rejection

```
// GateParker.ReleaseByKey(gateID string, reason string) — deletes park row + memory record, emits resume-event with reason.
// Wired to plan rejection/cancel paths in gate_park_wiring.go.
// Owner: 03-gate-release-on-rejection.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-park-tests.md | leaf (gate) | none | 35K | A |
| 01 | 01-parker-identity.md | leaf | 00T | 55K | A |
| 02 | 02-throttle-resume-delivery.md | leaf | 01 | 45K | B |
| 03 | 03-gate-release-on-rejection.md | leaf | 01 | 40K | B |
| 04 | 04-park-bounds-and-budget.md | leaf | 01 | 45K | B |

## Dispatch Protocol

Phase 0 gate. Leaf 01 alone (keystone; everything depends on its owner
column), then 02+03+04 parallel within the 2-cap. In-session review per
leaf; orchestrator commits per leaf.

## Review Checklist

- [ ] Migration: parked_turns owner column, backfill for existing rows (default owner = quota watcher's parker), Load filters
- [ ] Restart re-arm test: throttle row loads ONLY into throttle parker; quota row ONLY into quota parker (this is the C1 killer test)
- [ ] Throttle resume: persisted exchange + pushed reply + terminal event; no duplicate user-message re-add
- [ ] Gate: rejection path releases the parked row; PendingGates does not grow after reject
- [ ] Quota re-park bounded (attempt cap -> give-up terminal)
- [ ] Budget parker: persists + emits park/resume events on agent.quota_wait
- [ ] No gate-test modification

Output: APPROVED or gaps.

## Coding Conventions

Repo conventions; `-p 2`; bus payloads: add fields, never reshape (TUI +
Flutter parse unblock_at etc.); cite finding IDs.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | C1 (double-execution) | 01 T1-T4 | TestLoadReturnsOnlyOwnedRows + restart re-arm test |
| R2 | M-13 (throttle result lost) | 02 T1-T3 | TestThrottleResumeDeliversResult |
| R3 | M-12 (gate strand) | 03 T1-T2 | TestRejectReleasesGatePark |
| R4 | M-4-park (unbounded quota re-park) | 04 T1-T2 | TestQuotaReparkAttemptCap |
| R5 | F5 (budget persistence + events) | 04 T3-T4 | TestBudgetParkPersists |
| R6 | F6 (state-machine-detection dependency) | 02 T4 | TestParkedDetectionWithoutStateMachine |
| R7 | F8 (stash cross-capture) | 01 T5 | TestThrottleStashPerTurn |
| R8 | F9 (duplicate gate record) | 03 T3 | TestReRaiseReplacesNotAppends |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-park-tests | TESTS_VALIDATED | 1 | committed 222578ac (--no-verify RED-by-design); parent-verified 35 missing-impl errors only |
| 01-parker-identity | REVIEWED | 1 | committed bb24843f; C1 killer tests PASS parent-verified; legacy backfill by stored class column; inert sibling stubs documented |
| 02-throttle-resume-delivery | REVIEWED | 1 | committed fa5720fa; M-13+F6 closed; delivery parity + nil-SM marker parent-verified; full agent pkg fails only 3 sibling gates at review time |
| 03-gate-release-on-rejection | REVIEWED | 1 | committed 407932d6; M-12+F9 closed; wiring DONE (plan.rejected/:449 + plan.cancelled/:570 pre-existing); parent-verified both gates + 6 pre-existing green |
| 04-park-bounds-and-budget | COMPLETE | 1 | committed 80659bb5; full internal/agent suite green (all 12 gates); cap=5 per frozen gate; branch 04 INTEGRATION DONE |

## Integration Test Plan

Daemon-wiring restart test: park one throttle + one quota turn, tear down
parkers, re-wire, assert each row resumed exactly once by its owner
(counting both execution and side effects). Extend into
`go test -p 2 ./internal/agent/... ./internal/daemon/...`.

## Test Gate

| # | Feature | Source | Unit tests | E2E | Status |
|---|---------|--------|-----------|-----|--------|
| F1 | Parker identity | 01 | TestLoadReturnsOnlyOwnedRows | restart re-arm (integration) | VALIDATED (bb24843f; merged e729937d) |
| F2 | Throttle delivery | 02 | TestThrottleResumeDeliversResult | n/a | VALIDATED (fa5720fa; merged e729937d) |
| F3 | Gate release | 03 | TestRejectReleasesGatePark | n/a | VALIDATED (407932d6; merged e729937d) |
| F4 | Bounds + budget | 04 | TestQuotaReparkAttemptCap / TestBudgetParkPersists | n/a | VALIDATED (80659bb5; merged e729937d) |

## Open Questions

- Q5 (owner: user): quota re-park cap value - 10 (default) or configurable?
- Q6 (owner: user): budget park persistence - persist like quota (default) or keep memory-only-by-design and only add events?

## Frozen Documents

Leaves freeze at dispatch.

## Notes

C1 is the CRITICAL of this tree: 01 is its only fix and blocks the branch.
The park store migration must be backward-compatible with existing parks.db
files (ADD COLUMN + backfill; no table rebuild). The quota watcher payload
decode tolerance that makes C1 dangerous (overlapping JSON keys) must be
tightened in 01: decode errors should DROP, not misroute.
