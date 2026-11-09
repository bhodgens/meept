# Throttle Resume Delivery Parity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** M-13: resumed throttle turns deliver their result (persist + push + terminal) exactly like quota resumes; F6 parked-turn detection hardened.
- **Dependencies:** 01-parker-identity.md
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** M-13, F6

## Goal

A throttled interactive turn's answer reaches the client and history; the
park/detect path does not depend on an optional state machine.

## Context

- internal/agent/loop_park.go:32-52 throttledTurnPayload (no TurnID), :418-477 resumeThrottledTurn (logs only on success)
- Parity target: handler.go:2720-2746 + :3158-3185 (quota/budget resume: persistExchange + client push + turn.terminal via TurnID)
- F6: loop.go:3491-3492 parked-turn detection requires CurrentState()==StateQuotaWait; safeTransition failure falls through to success pipeline

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C-K2: throttledTurnPayload gains TurnID string (payload version-tolerant: absent = legacy row, resume still runs, delivery best-effort + warn).
// resumeThrottledTurn success path: persistExchange + client push + turn.terminal — byte-for-byte the same sequence resumeQuotaParkedTurn runs.
// Parked detection: parkThrottledTurn sets an explicit per-turn parked flag (context marker) — detection no longer requires the state machine read.
```

### What This Leaf Consumes

- C-K1 (owner semantics landed).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | TurnID in payload | Modify: loop_park.go payload + capture site | gate TestThrottleResumeDeliversResult GREEN (partially) | absent | GREEN: new parks carry TurnID | - |
| T2 | Delivery sequence | Modify: loop_park.go resumeThrottledTurn | gate test GREEN | resume only logs | GREEN: resumed turn persisted + pushed + terminal event with TurnID | Given parked throttle turn with reply R; When resumed; Then client receives R once, history contains it, terminal emitted |
| T3 | Legacy rows (no TurnID) | Verify via test | `-run TestThrottleResumeLegacyRow` (new) | - | GREEN: resumes without crash; delivery skipped with warn; no duplicate user-message re-add (H3 marker preserved) | - |
| T4 | Detection marker (F6) | Modify: loop_park.go park + loop.go detection site | gate TestParkedDetectionWithoutStateMachine GREEN | RED | GREEN: parked turn detected via marker even with nil stateMachine / rejected transition | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] H3 history-marker behavior preserved (no double user message)
- [ ] No client-side payload reshape (turn.terminal shape unchanged)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Delivery sequence matches quota resume exactly (read both, compare)
- [ ] Empty-message parked rows still drop safely

## Notes

handler.go owns turn.terminal emission for quota; loop-side emission must
use the same bus topic + payload shape — do NOT invent a new event.
