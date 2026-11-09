# Webhook Decouple + Teardown Parity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Voice findings 1 (webhook goroutine blocks on goal mutex), 10 (no C1 end frame at release), 11 (redelivery storm - doc), 8 (restart-mid-ring residual - doc + WaitCall close).
- **Dependencies:** 00T-voice-tests.md
- **Estimated Context:** 40K
- **Concurrency Group:** C
- **Audit references:** findings 1, 8, 10, 11

## Goal

The carrier's webhook never stalls on internal locks; the child process
learns of release through the frame protocol; residuals are documented.

## Context

- internal/voice_wiring.go:294-299 spawn seam calls holdPhoneCall SYNCHRONOUSLY inside HandleCallEvent (webhook goroutine); internal/voice/voice_employee.go:388-405 AcquireCallHold mu.Lock can block a full goal round; comm/http/voice.go:264-272 returns 500 on error -> carrier redelivery
- Finding 10: releaseCall cancels loop/stream ctx; no EventEnd C1 frame sent (child rides socket EOF)
- Finding 8: dial.go:352-388 effects completed at POST; after restart WaitCall returns never-closing channel (documented "state unknown"); bounded ctx saves callers

## Verified Anchors

Re-grep at dispatch (BLOCKED-START re-anchor).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Finding 1: holdPhoneCall moves OFF the webhook goroutine: HandleCallEvent spawns the hold+release sequencing (or acquires with timeout); webhook returns promptly. Hold STILL completes before first audio turn (supervisor gate semantics unchanged).
// Finding 10: releaseCall sends EventEnd C1 frame best-effort (existing frame send helper) before ctx cancel; child-side handling already exists per AGENTS.md frame protocol.
// Finding 8: dialer keeps a ccid->done map across restart? NO - on restart, done channels are rebuilt closed-with-reason for unknown ccids: WaitCall returns a closed channel + logged "state unknown" instead of a never-closing one.
// Finding 11: voice AGENTS.md documents the redelivery-storm posture (deliberate) + finding-8 residual in the same section.
```

### What This Leaf Consumes

- Existing hold/release machinery; C1 frame send helpers.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Async/timeout hold | Modify: voice_wiring.go spawn seam + voice_employee.go (AcquireCallHold timeout variant) | gate TestHoldDoesNotBlockWebhook GREEN | synchronous block | GREEN: webhook returns within bound; hold still gates first turn; carrier not invited to redeliver by our 500 | Given held goal mutex; When webhook arrives; Then response < 2s, hold applied async before first say |
| T2 | EventEnd at release | Modify: releaseCall path (voice_wiring.go :830-861 region) | new test TestReleaseSendsEndFrame | absent | GREEN: frame sent best-effort; failure logged, teardown continues | Given live call; When release fan-out; Then child receives EventEnd before socket close |
| T3 | WaitCall restart semantics | Modify: dial.go doneFor/restart path (:371-388) | new test TestWaitCallAfterRestartCloses | never-closing | GREEN: unknown ccid post-restart -> closed channel + warn | Given restart; When WaitCall(unknown ccid); Then returns immediately, logged |
| T4 | Docs: storm + residual | Modify: internal/voice/AGENTS.md | grep | - | findings 8+11 documented (deliberate posture, operator guidance) | - |

## Self-Verification Checklist

- [ ] Gate test GREEN, untouched
- [ ] Supervisor gate semantics unchanged (say still gated)
- [ ] Frame failure cannot hang release (timeout/best-effort)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] T1 preserves hold-before-first-turn invariant (test proves ordering)
- [ ] No new endpoints/topics; docs section matches repo voice AGENTS.md style

## Notes

T1 is the riskiest change in the voice branch: the hold ordering is an
audited invariant (C-series). If async-hold cannot preserve ordering
without a new synchronization primitive, escalate BEFORE improvising.
