# Voice CallLeg Interface — Design + Gating (follow-on to 20261109 design amendment 5)

Status: DESIGN ONLY. Implementation is BLOCKED until the sibling tree
`plan/voice-deadair-link` merges (its link.go/localsplice.go/voice_wiring.go
changes reshape the phone/local legs this interface will absorb).

## Problem (audit DUPLICATED verdict)

Three legs (phone/Telnyx, local, Signal) each keep their own call-state
bookkeeping over ~8 shared-but-scattered maps (Manager.calls/stream/
outbound/done, CallRegistry.live, Dialer.timers/answered, Binding.calls,
childproc live set, calls.db rows, per-leg callState). The 20261109 tree
fixed the drift symptoms (fresh-session, stable key, terminal completeness,
watchdogs) but the STRUCTURE still permits new drift: a fourth channel
added today copies whichever leg it likes, not a contract.

## Target shape

```
// internal/voice (new callleg.go) — carrier-neutral contract
type CallLeg interface {
    Place(ctx, req PlaceRequest) (CallHandle, error)   // reserve+ring; req carries EmployeeID, Target, Leg, RequestID (idempotency), FreshSession, DayBrief
    Accept(ctx, id string) (CallHandle, error)         // inbound adoption (phone webhook / signal event)
    Hangup(ctx, handle, reason string) error           // idempotent
    Events() <-chan CallEvent                          // unified: ringing/connected/reconnecting/ended/errored
}
type CallHandle struct { CCID, Leg, EmployeeID, Target string }
```

Manager holds ONE per-call state struct keyed by ccid (absorbs
calls/stream/outbound/done); release fan-out (releaseOnce) unchanged as
the single terminal seam (verified-clean 20261109 — do not restructure it).
Registry cap/one-live rules stay shared (they already are, dial.go
AcquireLeg). Each leg becomes an adapter implementing CallLeg:
- phone: wraps Telnyx controller + Dialer timers
- local: wraps LocalLeg (already FailOutcome-parity)
- signal: wraps signalleg.Leg (parity fixed 20261109 leaf 02)

## Why blocked now

voice-deadair-link (in flight, sibling) rewrites exactly the phone-leg
turn-taking/localsplice paths an adapter must wrap. Re-basing an interface
extraction on moving ground is how the fresh_session drift started in the
first place.

## First implementation leaf after unblock (pre-written for a future tree)

01-callleg-interface.md: define CallLeg + CallHandle + CallEvent in
internal/voice; NO behavior change; compile-adapter stubs per leg marked
`// ADAPTER STUB (tree 20261111-voice-callleg)`. Gate tests pin the three
adapters' Events() shape equivalence against today's observable surfaces
(bus events + calls.db + registry release) before any caller moves.

## Consequences to verify at kickoff (survey tasks)

1. Diff voice-deadair-link against this design's assumptions (link
   coordinator inside phone leg = adapter-internal, must not leak).
2. voicechild C1 frame protocol unaffected (adapter speaks frames via the
   existing TextServer sink — releaseCall end frame 20261109 leaf 04 stays).
3. calls.db schema unchanged; state values stay live/ended.

## Implementation status (updated by tree 20261110-voice-callleg leaves as they land)

- FIRST PRODUCTION CALLER MIGRATED — DONE (leaf 03). The contact-chain
  signal_voice attempt now Places through the CallLeg contract:
  `voice.NewSignalAdapter(leg)` constructed at the single daemon site
  internal/daemon/contact_wiring.go:128, consumed at
  internal/signalleg/voiceattempt.go:62 (`a.caller.Place(...)`; the
  seam-taking constructor is voiceattempt.go:29). Observable behavior
  unchanged (same PlaceRequest fields, same C9 effect keys — pinned by
  internal/signalleg/callleg_chain_pin_test.go); the adapter's
  Events() lane is added-but-unconsumed by the chain. Survey item 3
  (calls.db schema untouched) holds: the adapter calls through the
  same leg paths. builtin voice_call tool stays on the Dialer per
  Q-B1 default.
