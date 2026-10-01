# GUI Flows Tests - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. After completing, report what
> you built, what files you touched, and any deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Write the four gui flows against the StubDaemon from leaf 02:
  terminal-event delivery, session switch transcript reload, quota-wait
  surfacing, and streaming accumulation.
- **Dependencies:** 02-gui-flows-infrastructure.md (StubDaemon + pumpRealApp)
- **Estimated Context:** ~75K
- **Concurrency Group:** B

## Goal

These are the actual regression tests for the bug class that shipped in
September: terminal events dropped or misdelivered, pending-turn tracking
wiped on rebuild, quota parks never resolving. Each flow scripts a daemon
conversation through StubDaemon and asserts what the REAL ChatProvider +
widgets do with it.

## Context

Repo: /Users/caimlas/git/meept. Everything from leaf 02 exists:
`ui/flutter_ui/test/e2e/stub_daemon.dart` (StubDaemon, StubSseEvent) and
`pump_app.dart` (pumpRealApp). The prior audit findings these tests pin
(2026-09-17 audit items 19-21): terminal events arriving before the ack
registers the pending turn were dropped (chat_provider.dart ~441); the
send/steer path rebuilt ChatState without pendingTurns (~828); TUI-side
analogues in chat_turn.go.

Key files:

- `ui/flutter_ui/lib/providers/chat_provider.dart` - pendingTurns map,
  ack race window, terminal delivery path
- `ui/flutter_ui/test/e2e/stub_daemon.dart` - the scripting API
- Prior audit: `.hermes/audits/2026-09-17-week-bughunt.md` findings 19-21
  (read for the exact regression shapes being pinned)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: ui/flutter_ui/test/e2e/gui_flows_test.dart
// Scenario ids (registered by leaf 04):
//   gui-terminal-01  TestTerminalEventDeliversToPendingTurn
//   gui-session-01   TestSessionSwitchReloadsTranscript
//   gui-quota-01     TestQuotaParkSurfacesAndWaitResolves
//   gui-stream-01    TestStreamingTurnAccumulatesAndFinalizes
```

### What This Leaf Consumes

```
Leaf 02: StubDaemon (enqueueChatTurn, enqueueSessionList, submitCount),
         pumpRealApp (real ChatProvider in a ProviderScope)
```

## Tasks

### Task 1: Terminal event delivery (gui-terminal-01)

**Objective:** A terminal chat_message event for a pending turn resolves
the pendingTurns entry and surfaces the content - including the ACK-RACE
shape: terminal event arrives before the submit ack is processed.

**Files:**
- Test: `ui/flutter_ui/test/e2e/gui_flows_test.dart`

**Step 1:** Script StubDaemon so the SSE terminal event fires BEFORE the
submit-ack response (order-controlled script; if StubDaemon lacks
order control, extend it additively). Pump, call the real submit path,
run async, assert: pendingTurns is empty (resolved, not leaked), the
rendered transcript contains the terminal content, and no error state.

**Step 2:** `flutter test test/e2e -r expanded` -> PASS

### Task 2: Session switch reload (gui-session-01)

**Objective:** Switching sessions reloads the transcript from the
(newly-fetched) session history and does not leak the prior session's
pending turns.

**Files:**
- Test: same file, Task 2

**Step 1:** Two sessions scripted; start on A with a pending turn
in-flight; switch to B; assert B's transcript renders B's scripted
history, A's pending turn is no longer rendered in B's view, and the
provider's pendingTurns still tracks A's turn (correctness: it belongs
to A, not dropped - this pins the audit's pendingTurns-wipe finding).

**Step 2:** Verify PASS.

### Task 3: Quota park surfacing (gui-quota-01)

**Objective:** A quota-wait progress event surfaces as a waiting state,
and a later terminal event resolves it.

**Files:**
- Test: same file, Task 3

**Step 1:** Script agent_progress(quota_wait) then (after a scripted
delay/second poll) the terminal chat_message. Assert intermediate waiting
state rendered, then final content delivered, pendingTurns resolved.

**Step 2:** Verify PASS.

### Task 4: Streaming accumulation (gui-stream-01)

**Objective:** Multiple streamed assistant content events accumulate in
order into one rendered message, finalized exactly once.

**Files:**
- Test: same file, Task 4

**Step 1:** Script 5 ordered chat_message deltas + 1 terminal; assert the
final transcript contains the concatenation in order, the message count
is 1 (not 5), and pendingTurns resolved.

**Step 2:** `flutter test test/e2e` -> all green.

## Self-Verification Checklist

- [ ] Four flows pass: `flutter test test/e2e -r expanded`
- [ ] Each asserts provider state AND rendered output, not one alone
- [ ] The ack-race shape (Task 1) is genuinely ordered terminal-before-ack
- [ ] No mocks in the client path; only the stub server is scripted
- [ ] No sleeps; `tester.pump`/`runAsync` discipline throughout

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] All four scenario names exact (gui-terminal-01 etc. in comments)
- [ ] Task 2 asserts pendingTurns RETAINED for the departed session
      (the anti-wipe pin), not just B's view correctness
- [ ] Full `flutter test test/e2e` green

## Notes

- These are the regression pins for audit findings 19-21; if a flow fails
  because the PRODUCTION code has the bug (e.g. the ack race still
  drops), that is a FINDING to report, not something to paper over with
  looser assertions. Report it and mark the test with a skip + reason
  rather than deleting.
