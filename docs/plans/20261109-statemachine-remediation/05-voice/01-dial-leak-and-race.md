# Dial Leak + Answered Race - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Voice findings 2 (manager map leak on acquire failure) + 9 (answered-vs-noanswer race window).
- **Dependencies:** 00T-voice-tests.md
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** findings 2, 9

## Goal

Every dial-terminal path fires FailOutcome exactly once; the no-answer
timer cannot tear down a call that just answered.

## Context

- internal/voice/dial.go:326-343 - TrackOutbound succeeded, registry-race loss -> finishCall + carrier hangup, NO Manager.FailOutcome (m.outbound/m.done leak; WaitCall channel never closes)
- internal/voice/dial.go:503-525 - armNoAnswerTimer fire: re-checks d.answered, window between check and FailOutcome(no_answer)
- Correct patterns: abandonOutbound (leg.go:604-614), FailOutcome once-guard manager.go:141-166

## Verified Anchors

Re-grep at dispatch. Sibling-session dirty files: re-anchor against landed main at dispatch (branch is BLOCKED-START).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C-V1: acquire-failure path calls FailOutcome(ccid, OutcomeFailed) after finishCall — once-guarded by releaseOnce, no double-fire when carrier hangup later arrives.
// Finding 9: the answered re-check and the FailOutcome(no_answer) happen under the dialer's per-call lock (or a compare-and-swap on d.answered) so check→fire is atomic w.r.t. NotifyAnswered.
```

### What This Leaf Consumes

- Existing releaseOnce fan-out (unchanged).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | FailOutcome on acquire failure | Modify: dial.go (:326-343 region) | gate TestAcquireFailureFiresFailOutcome GREEN | absent | GREEN; outbound/done maps drain; WaitCall closes | Given registry-race loss; When dial aborts; Then outcome failed fired once |
| T2 | Race window closed | Modify: dial.go armNoAnswerTimer fire path | gate TestNoAnswerVsAnsweredRace GREEN | window remains | GREEN: interleaving check→webhook→fire no longer tears down answered call | Given answer lands mid-fire; Then no_answer NOT fired (or second outcome no-ops) |
| T3 | Once-guard audit | Verify: grep every finishCall/HangupCall path has paired FailOutcome | grep at review | - | zero unpaired paths (list in report) | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] T3 audit list exhaustive (6 known terminal initiators + new one)
- [ ] releaseOnce still the single funnel

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; T3 audit list; deviations.

## Review Checklist (For Review Agent)

- [ ] No double-outcome when carrier hangup follows acquire failure
- [ ] Map drains proven by test (no goroutine/channel leak)

## Notes

Keep the fix minimal: FailOutcome is already idempotent — the bug is purely
the missing call + the unlock-protected check window.
