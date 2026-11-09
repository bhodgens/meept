# Ringing Watchdog + calls.db Sweep - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Voice findings 3 (signal ringing timeout), 7 (calls.db stranded 'live' rows), 4 (media-WS reconnect never re-splices).
- **Dependencies:** 00T-voice-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** findings 3, 7, 4, C-V3

## Goal

No ringing call holds a registry slot forever; persisted call rows reach a
terminal; a media reconnect restores audio.

## Context

- internal/signalleg/leg.go:375-480 Place: no local no-answer timer (phone has armNoAnswerTimer, dial.go:479-600); audio.go:360-374 holds on socket EOF awaiting RECONNECTING (compounds the hold)
- internal/voice/callstore.go:40/160: voice_calls.state 'live'->'ended' only via End; no boot sweep (grep daemon.go/voice_plain_boot.go)
- internal/voice/manager.go:311-321 OnMediaStart idempotent per ccid — a dead first splice blocks the reconnect splice

## Verified Anchors

Re-grep at dispatch (BLOCKED-START re-anchor).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C-V3:
// SignalLeg ringing watchdog: Place arms a no-answer timer (SAME duration source/config as phone armNoAnswerTimer); fire -> FailOutcome(no_answer) + registry Release (once-guarded with ENDED delivery).
// calls.db boot sweep: UPDATE voice_calls SET state='ended', outcome='failed' WHERE state='live' — run once at voice wiring boot, BEFORE any call can start.
// OnMediaStart: if a prior splice for ccid errored (non-cancellation), allow re-splice (track last error/closed state, not just "started once").
```

### What This Leaf Consumes

- Timer duration source: reuse the dialer's config value (single source; do not add a new knob).
- releaseOnce fan-out (unchanged).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Ringing watchdog | Modify: signalleg/leg.go Place + timer arm | gate TestRingingTimeout GREEN | no timer | GREEN: unanswered call -> no_answer outcome, slot released, employee budget freed; late ENDED no-ops | Given Place with no CONNECTED within timeout; Then terminal once |
| T2 | Boot sweep | Modify: voice wiring boot path (grep for callstore open/init) | gate TestBootSweepEndsStaleLive GREEN | absent | GREEN: stale live rows ended/failed at boot; single-instance note: sweep runs at wiring init before listeners accept calls | - |
| T3 | Re-splice on reconnect | Modify: manager.go OnMediaStart (:311-321) | gate TestMediaReconnectResplices GREEN | blocked by idempotence | GREEN: after prior splice error, new media WS re-arms splice | Given splice died from read error; When media restarts; Then splice runs again |
| T4 | audio.go EOF-hold bound | Verify/modify: audio.go:360-374 hold has the watchdog as backstop | `go test -p 2 ./internal/signalleg/` | - | GREEN: EOF hold cannot outlive T1's timer | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] Watchdog once-guarded vs ENDED (no double outcome)
- [ ] Sweep idempotent + ordered before call acceptance

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Timer duration shared with dialer config (no new knob)
- [ ] Re-splice condition is error-aware, not count-aware (legitimate reconnect after clean end must NOT splice)

## Notes

Finding 8 (restart mid-ring, effects-completed) stays documented residual —
covered by branch leaf 04's doc task, not this leaf.
