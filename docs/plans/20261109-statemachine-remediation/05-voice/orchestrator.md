# Voice Call Lifecycle Remediation - Implementation Orchestrator

> **For the executing agent:** orchestrator for this branch. Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test-gate leaf + 4 implementation leaves
- **Scope:** Close voice findings 1-11: map leaks (2), ringing timeout (3), re-splice (4), signal-leg parity (5, 6), calls.db stranded rows (7), restart-mid-ring (8), answered race (9), C1 end frame (10), webhook mutex block (1), redelivery storm documentation (11).

## Goal

Every call reaches a terminal exactly once on every leg; every leg honors
the same session/brief/idempotency contracts; restart reconciles persisted
call state.

## Architecture

Fix in place: the releaseOnce fan-out stays the single terminal seam
(verified clean); the fixes add the MISSING terminal paths (finding 2, 3)
and align the signal leg with the phone/local legs (5, 6). Branch is
BLOCKED-START: the sibling session holds internal/voice dirty; re-anchor all
against landed main before dispatch.

## Interface Contracts

### C-V1: Dialer terminal on acquire failure

```
// dial.go acquire-failure path calls Manager.FailOutcome(ccid, OutcomeFailed) after finishCall — mirrors abandonOutbound (leg.go:604).
// Owner: 01-dial-leak-and-race.md
```

### C-V2: Signal leg parity

```
// bindCall passes FreshSessionFor + DayBrief; effectKey uses (employee, target, requestID) like the phone dialer (dial.go:231-260).
// Owner: 02-signal-leg-parity.md
```

### C-V3: Ringing watchdog

```
// SignalLeg rings timer: Place arms no-answer timer (same duration source as armNoAnswerTimer); ENDED or timeout -> FailOutcome + registry Release.
// calls.db startup sweep: UPDATE voice_calls SET state='ended', outcome='failed' WHERE state='live' at boot.
// Owner: 03-ringing-watchdog-and-db-sweep.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-voice-tests.md | leaf (gate) | none | 30K | A |
| 01 | 01-dial-leak-and-race.md | leaf | 00T | 40K | A |
| 02 | 02-signal-leg-parity.md | leaf | 00T | 45K | B |
| 03 | 03-ringing-watchdog-and-db-sweep.md | leaf | 00T | 45K | B |
| 04 | 04-webhook-and-teardown.md | leaf | 00T | 40K | C |

## Dispatch Protocol

BLOCKED-START: wait for the sibling voice wave to land on main. Then Phase 0
gate; groups A, B, C within the 2-cap. In-session review per leaf.

## Review Checklist

- [ ] Finding 2: every finishCall/HangupCall path paired with FailOutcome (grep at review)
- [ ] Finding 3: ringing timeout bounded; registry slot released on timeout
- [ ] Finding 5/6: signal leg fresh-session + day brief + stable effect key; one call one session
- [ ] Finding 7: boot sweep idempotent; does not clobber rows of calls genuinely live on another instance (single-instance guard)
- [ ] Finding 9: race window closed or shrunk with re-check under the dialer lock
- [ ] Finding 1: holdPhoneCall moved off the webhook goroutine (async or timeout-bounded)
- [ ] internal/voice/AGENTS.md updated in the same commit for any changed invariant (repo rule)
- [ ] No gate-test modification

Output: APPROVED or gaps.

## Coding Conventions

Repo conventions; `-p 2`; every fix comment cites the audit finding number
and this tree. Voice AGENTS.md is the contract source - read it before
touching frames/sockets/release.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | Finding 2 (map leak) | 01 T1-T2 | TestAcquireFailureFiresFailOutcome |
| R2 | Finding 9 (answered race) | 01 T3 | TestNoAnswerVsAnsweredRace |
| R3 | Finding 5/6 (signal parity) | 02 T1-T3 | TestSignalLegFreshSession / TestSignalLegEffectKeyStable |
| R4 | Finding 3/7 (timeout + db sweep) | 03 T1-T3 | TestRingingTimeout / TestBootSweepEndsStaleLive |
| R5 | Finding 4 (re-splice) | 03 T4 | TestMediaReconnectResplices |
| R6 | Finding 1 (webhook mutex) + 10 (C1 end frame) + 11 | 04 T1-T3 | TestHoldDoesNotBlockWebhook |
| R7 | Finding 8 (restart mid-ring, documented residual) | 04 T4 (doc + WaitCall close) | doc review |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-voice-tests | TESTS_VALIDATED | 2 | part 1 (terminal_gate_test.go) + part 2 (leg_gate + webhook_gate) both committed; 8 pins, all parent-verified RED for right reasons |
| 01-dial-leak-and-race | COMPLETE | 1 | committed f9304084 (dial.go) + 47d03b60 (manager tombstone, parent-landed residual) + gofmt fix; finding 2 FULLY closed, finding 9 closed; T3 audit: 0 unpaired paths |
| 02-signal-leg-parity | COMPLETE | 1 | committed 97c67a59; findings 5/6 closed; nil-seam TRUE posture gate-forced + documented; daemon one-liner flagged & parent-reviewed (sole New site) |
| 03-ringing-watchdog-and-db-sweep | IN_PROGRESS | 0 | dispatched (leg timer + callstore sweep + OnMediaStart; voice_wiring.go off-limits w/ deferral rule) |
| 04-webhook-and-teardown | IN_PROGRESS | 0 | dispatched (wiring/employee/docs; T3 dial.go DEFERRED to orchestrator — leaf 01 owns dial.go) |

## Integration Test Plan

`go test -p 2 ./internal/voice/... ./internal/signalleg/... ./internal/daemon/...`;
full release-fan-out test: hangup arriving from each of the 6 terminal
initiators - outcome fires exactly once (extends the existing verified-clean
matrix with the new paths).

## Test Gate

| # | Feature | Source | Unit tests | E2E | Status |
|---|---------|--------|-----------|-----|--------|
| F1 | Terminal completeness | 01 | TestAcquireFailureFiresFailOutcome | fan-out matrix | PENDING |
| F2 | Signal parity | 02 | per-task | n/a | PENDING |
| F3 | Watchdog + sweep | 03 | per-task | n/a | PENDING |
| F4 | Webhook decouple | 04 | TestHoldDoesNotBlockWebhook | n/a | PENDING |

## Open Questions

- Finding 11 (redelivery storm) is deliberate-and-commented upstream;
  default: document in voice AGENTS.md + keep. No code change unless the
  user opts into webhook-level dedup.

## Frozen Documents

Leaves freeze at dispatch.

## Notes

The sibling session's in-flight voice work may already fix part of finding
5 (fresh_session_per_call docs leaf landed at HEAD). Re-anchor everything at
dispatch time; drop SUPERSEDED tasks with a dated reconciliation note
rather than re-fixing.
