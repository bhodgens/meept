# CallLeg Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY, RED for missing-implementation reasons (undefined CallLeg symbols = accepted compile-RED). No prod changes, no commits. No read_file on existing sources; grep/cat. NEVER edit frozen gates (terminal_gate_test.go, leg_gate_test.go, voice_webhook_gate_test.go — verify zero-diff after).

## Meta

- **Parent:** ../master.md
- **Scope:** internal/voice new test file(s) only (+ signalleg if a per-leg pin is cleaner there).
- **Dependencies:** none
- **Estimated Context:** 35K
- **Audit references:** 20261109 design amendment 5; DESIGN.md here.

## Goal

Pin C-B1: one CallLeg interface (Place/Accept/Hangup/Events), three
adapters, and event-shape parity: each leg's observable signals map to the
same CallEvent enum values.

## Verified Anchors (re-grep at dispatch)

../20261110-voice-callleg-design.md contract block; fake rigs: internal/voice test fakes (fakeController/telnyx rig in dial_test/manager tests; newTestLeg fake-cli in signalleg audio_test; local rig in local_test/localsplice tests).

## Interface Contracts (From Parent)

### What This Leaf Exposes
```
internal/voice/callleg_gate_test.go:
TestCallLegAdaptersExist      // var _ CallLeg = phoneAdapter{} / localAdapter{} / signalAdapter{} (compile-RED undefined)
TestCallLegPlacePin           // per-adapter Place over its own fake carrier: handle carries CCID/Leg/EmployeeID/Target; idempotency via RequestID where the leg supports it (signal/local: DESIGN says ledger keys differ — pin per-leg actual today's semantics passthrough)
TestCallLegHangupIdempotentPin// double Hangup no-ops per today's per-leg behavior (all three already idempotent — pins lock it IN)
TestCallLegEventsParityAcrossLegs // feed each fake rig its natural terminal signal (telnyx call.hangup / signal ENDED / local End) -> Events() yields CallEvent{Kind: Ended, Reason:...} on all three, same enum, no leg-specific strings leaking
```
(If adapter names in DESIGN.md differ from what leaf 01 will choose, keep these names — they ARE the contract.)

## Tasks

| Task ID | Objective | Files | Verify | RED reason |
|---------|-----------|-------|--------|------------|
| T1 | the four tests above | Test: internal/voice/callleg_gate_test.go | `go test -p 2 -count=1 ./internal/voice/ -run 'CallLeg'` | undefined CallLeg/adapters |

## Self-Verification Checklist

- [ ] RED for implementation reasons; frozen gates zero-diff; pre-existing suites otherwise green
- [ ] gofmt clean

**DO NOT COMMIT.**

## Review Checklist (For Review Agent)

- [ ] parity test asserts enum equality across legs, not message text

## Notes

Events() channel ownership (who closes it) left to leaf 01; pin only the
value shape here.
