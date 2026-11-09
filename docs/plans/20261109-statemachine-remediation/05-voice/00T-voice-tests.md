# Voice Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY, fail RED for missing-implementation reasons. Do NOT implement production code. Do NOT commit.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Gate tests for voice branch (findings 1-11).
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** voice findings 1-11 (audit 2026-11-09)

## Goal

Pin: terminal completeness on every release path, signal-leg parity, ringing watchdog, calls.db sweep, webhook decoupling.

## Context

- internal/voice (manager.go, dial.go, binding.go), internal/signalleg/leg.go, internal/daemon/voice_wiring.go, voice_employee.go
- The release fan-out (releaseOnce) is verified-clean — tests must not weaken it.

## Verified Anchors

Re-grep at dispatch: dial.go:326-343 (acquire-failure path, no FailOutcome), leg.go:375-480 (Place, no timer), leg.go:302-312 (bindCall no freshSession/DayBrief), leg.go:412 (random-ccid effectKey), manager.go:311-321 (OnMediaStart idempotent), callstore.go:40/160 (no boot sweep), voice_wiring.go:294-299 (synchronous hold in spawn seam).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test functions:
TestAcquireFailureFiresFailOutcome
TestNoAnswerVsAnsweredRace
TestSignalLegFreshSession
TestSignalLegEffectKeyStable
TestRingingTimeout
TestBootSweepEndsStaleLive
TestMediaReconnectResplices
TestHoldDoesNotBlockWebhook
```

### What This Leaf Consumes

Parent contracts C-V1..C-V3 (compile/behavior targets).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Terminal completeness tests | Test: internal/voice/dial_gate_test.go | `go test -p 2 ./internal/voice/ -run 'TestAcquireFailureFiresFailOutcome|TestNoAnswerVsAnsweredRace'` | acquire-failure never fires FailOutcome | GREEN | Given registry-race loss after TrackOutbound; When dial fails; Then FailOutcome(failed) fires exactly once, maps drain. Given answered lands between timer check and fire; Then no teardown of answered call |
| T2 | Signal parity tests | Test: internal/signalleg/leg_gate_test.go | `go test -p 2 ./internal/signalleg/ -run 'TestSignalLegFreshSession|TestSignalLegEffectKeyStable'` | bindCall lacks freshSession; effectKey random | GREEN | Given fresh_session_per_call=true; When signal call starts; Then fresh session resolved like phone leg. Same request_id retried -> same effect key |
| T3 | Watchdog + sweep tests | Test: internal/signalleg + internal/voice | `-run 'TestRingingTimeout|TestBootSweepEndsStaleLive'` | no timer; no sweep | GREEN | Given unanswered signal call past timeout; Then FailOutcome + registry release. Given calls.db row state='live' from dead boot; When boot sweep; Then ended/failed |
| T4 | Webhook/resplice tests | Test: internal/daemon or voice per package layout | `-run 'TestHoldDoesNotBlockWebhook|TestMediaReconnectResplices'` | hold synchronous; no re-splice | GREEN | Given goal mutex held; When webhook arrives; Then handler not blocked beyond bound. Given media WS reconnect; Then splice re-armed |

## Self-Verification Checklist

- [ ] RED for implementation reasons; no prod changes

**DO NOT COMMIT.**

## Completion Report Format

Files; RED tails; deviations.

## Review Checklist (For Review Agent)

- [ ] Matrix F1-F4 covered; releaseOnce invariant untouched

## Notes

Voice test rig exists (C1-C8 audit history) — extend it. Tests must run hermetic (no carrier).
