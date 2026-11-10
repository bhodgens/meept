# Event Unification - Implementation Leaf

> **For the implementing agent:** TDD. Gate: callleg_gate_test.go test 4 (frozen). Do NOT commit. No read_file. NEVER edit gates.

## Meta

- **Parent:** ../master.md
- **Scope:** the three adapter files from leaf 01 (callleg_phone.go, callleg_local.go, callleg_signal.go) — internal/voice only.
- **Dependencies:** 01-interface-adapters.md
- **Estimated Context:** 55K

## Goal

TestCallLegEventsParityAcrossLegs GREEN: each leg's terminal/transition
signals map onto the SAME CallEvent enum + reason vocabulary (no
carrier-specific strings in Kind; free-text belongs in Reason).

## Contracts (From Parent)

Consumes C-B1 event enum; mapping table (leaf 01 report + these defaults):
telnyx call.answered->Connected; call.hangup->Ended(hangup); no-answer
timer->Ended(no_answer); spawn fail->Errored; signal CONNECTED->Connected;
ENDED->Ended(reason passthrough); RECONNECTING->Reconnecting; local End->
Ended(hangup); local failures->Errored. Events() closed by adapter on leg
teardown (document ownership).

## Tasks

| Task ID | Objective | Files | Verify | PASS |
|---------|-----------|-------|--------|------|
| T1 | mapping table as named consts | callleg_*.go | vet | single source per leg |
| T2 | wire each adapter's signal sources to Events() | callleg_*.go | gate 4 GREEN + full voice/signalleg suites | parity |
| T3 | channel close-once semantics + test | callleg*.go, +new internal/voice/callleg_events_test.go (non-gate) | -race | no double close |

## Self-Verification

- [ ] all four CallLeg gates GREEN
- [ ] frozen 20261109 voice gates zero-diff green; releaseOnce untouched
- [ ] build green; -race voice sample

**DO NOT COMMIT.**

## Review Checklist

- [ ] no behavior moved from fan-out into events (observability only)

## Notes

If a leg cannot emit some kind (local Reconnecting), omit — Kind set is
per-leg documented in callleg.go comment.
