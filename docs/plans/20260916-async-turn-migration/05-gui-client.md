# GUI Client Async Migration (Flutter) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The Flutter GUI submits turns via POST /api/v1/chat/submit and renders results from the WS turn.terminal relay (agent_progress), with per-turn progress and honest stalled/late-error states. Web + desktop parity.
- **Dependencies:** 01-async-rpc-mode.md (HTTP endpoint, Contract 5)
- **Estimated Context:** ~60K
- **Concurrency Group:** B
- **Repo:** meept (ui/flutter_ui — same repo)

## Goal

The GUI posts to `/api/v1/chat` (sdk_client.dart:392,414) and blocks that
HTTP call until the daemon's reply — long tasks hit the same stub problem.
This leaf: submit returns instantly; the result arrives over the existing
WebSocket as the Plan-1 `turn.terminal` event (WS-classified
`agent_progress`); the chat view renders per-turn progress, the final
reply, failed turns, and stalled turns (liveness timeout), for both web and
desktop.

## Context

Flutter app at ui/flutter_ui. Networking: lib/services/sdk_client.dart
(`_post('/api/v1/chat')` at :392/:414, steer :474, followup :490). WS:
lib/services/websocket_service.dart — already filters `agent_progress`
events by session (:646-663) and has reconnection. Flutter rules from
AGENTS.md: no top-level dart:io imports in shared code; `kIsWeb` guards;
PlatformService for platform abstraction; UI text lowercase; TUI/GUI parity
(matching semantics with the TUI leaf: per-turn progress line, reply bubble,
error bubble, stalled text "no progress for Ns — task may still complete").

Key files:
- lib/services/sdk_client.dart — chat submit/steer/followup calls
- lib/services/websocket_service.dart — WS connect, agent_progress filter
- lib/providers/ (chat/session state — find the provider the chat screen uses)
- lib/features/ (chat screen widgets)
- lib/core/platform/platform_service.dart — platform abstraction pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// lib/services/sdk_client.dart:
//   class ChatSubmitAck {
//     final String turnId; final String conversationId;
//     final String sessionId; final bool accepted; final String note;
//     // json factory fromParse(Map<String,dynamic>)
//   }
//   Future<ChatSubmitAck> submitChat(ChatRequest req)  // POST /api/v1/chat/submit
//
// lib/services/websocket_service.dart (or a new turn_events.dart service):
//   Stream<TurnTerminalEvent> turnTerminalStream(String sessionId)
//   // parses WS agent_progress events whose payload came from topic
//   // turn.terminal — identify by payload["handler_case"] presence AND
//   // payload["turn_id"]; expose status/reply/error/durationMs/turnId.
//
// lib/providers/<chat provider>:
//   submitTurn(message) → ack; tracks pending turns:
//   Map<turnId, PendingTurn{conversationId, startedAt, lastProgressAt, status}>
//   states: pending → progress(text) → terminal(status)
//   liveness: Timer per pending turn; on LivenessTimeout (120s, config)
//   mark stalled (UI shows stalled text; a later terminal event still
//   renders the reply — stalled is NOT terminal).
```

### What This Leaf Consumes

```
// Daemon: POST /api/v1/chat/submit (Contract 5 ack JSON); WS agent_progress
// events carrying the turn.terminal payload (Plan 1 Contract 1 frozen keys:
// turn_id, conversation_id, session_id, status, reply, error, duration_ms,
// handler_case).
```

## Tasks

### Task 1: ChatSubmitAck + submitChat

**Objective:** The submit call and its ack model.

**Files:**
- Modify: `lib/services/sdk_client.dart` (add near the existing chat posts)
- Test: `test/services/sdk_client_submit_test.dart` (create; follow existing
  sdk_client tests' http-mock pattern — find it)

**Step 1: Failing tests** — POSTs to /api/v1/chat/submit with the chat body;
parses the ack; accepted=false surfaces note; error propagation matches the
existing error style.

**Steps 2-4:** standard cycle. Web-safe: pure HTTP via the existing client
 plumbing — no dart:io.

### Task 2: TurnTerminalEvent stream

**Objective:** Parse and expose terminal events from the WS.

**Files:**
- Modify: `lib/services/websocket_service.dart` (or new
  `lib/services/turn_events.dart` wired off the existing WS message stream —
  prefer extending websocket_service to keep one WS owner)
- Test: `test/services/turn_events_test.dart`

**Step 1: Failing tests** — feed a fabricated WS frame (the test harness
the existing websocket tests use) with an agent_progress event whose payload
matches turn.terminal (handler_case present + turn_id present) → stream
emits a parsed TurnTerminalEvent; an agent_progress event WITHOUT turn_id
(ordinary progress) is NOT emitted on the terminal stream; event for another
session filtered by subscription session filter (mirror :646-663 logic).

**Steps 2-4:** standard cycle. kIsWeb-safe (WS is already cross-platform).

### Task 3: Chat provider + UI states

**Objective:** submitTurn flow with pending/progress/terminal/stalled
states rendered in the chat screen.

**Files:**
- Modify: the chat state provider (locate via the chat screen's provider
  import) and the chat screen widget(s) in lib/features/
- Test: `test/providers/chat_submit_flow_test.dart` + a widget test for the
  chat view states

**Step 1: Failing tests**

- submitTurn: ack → pending turn appears (progress indicator, lowercase
  label "running turn …"); terminal completed → reply bubble replaces
  indicator; failed → error bubble with the error text; stalled (fake timer)
  → stalled text "no progress for 120s — task may still complete"; a late
  terminal event after stalled STILL renders the reply and clears the
  stalled state.
- Two concurrent submits track independently (turn-keyed).
- Web/desktop: no platform-conditional behavior in the flow (kIsWeb only
  where the existing code already branches).

**Steps 2-4:** standard cycle. Reuse existing bubble widgets/styles; do not
introduce a new design language.

### Task 4: Late-error and park-state UX

**Objective:** Errors and parked turns arriving minutes later still land on
the right conversation view.

**Files:**
- Modify: the chat provider + chat screen (Task 3's files)
- Test: extend Task 3's tests

**Step 1: Failing tests**

- status=failed terminal for a conversation the user has navigated away
  from → the conversation's unread/error indicator increments (follow the
  existing unread/session-badge pattern — locate it) so the user discvers
  the failure contextually.
- status=parked (quota) → indicator text "waiting for provider quota —
  will resume automatically" (lowercase), NOT an error styling; terminal
  event on resume clears it.

**Steps 2-4:** standard cycle.

## Self-Verification Checklist

- [ ] All tasks implemented; `flutter analyze` clean; `flutter test` green
- [ ] Web parity: no dart:io top-level; kIsWeb only in existing branch points
- [ ] turn_id-keyed pending turns; concurrent turns independent
- [ ] Stalled ≠ terminal: late completion still renders
- [ ] Late errors land on the right conversation with a discoverable indicator
- [ ] Parked state renders quota-wait honestly, not as an error
- [ ] Lowercase UI text; existing bubble/widget styles reused
- [ ] No new dependencies without orchestrator approval

**DO NOT COMMIT.** Orchestrator commits after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks + tests present/passing (flutter test; -race N/A for Dart)
- [ ] Contract 5 (submit endpoint) + Contract 4 (loop shape, Dart idiom)
- [ ] Terminal stream parses ONLY turn.terminal-shaped events (turn_id required)
- [ ] Session filtering mirrors websocket_service.dart:646-663
- [ ] No sync /api/v1/chat call remains on the migrated chat path
- [ ] Stalled/parked/late-error UX per spec, lowercase, non-blocking
- [ ] No scope creep: steer/followup endpoints untouched; no theme changes

Output: APPROVED or specific gaps with file+line.

## Notes

- The GUI's steer (':474') and followup (':490') endpoints stay synchronous
  — they are short control-plane calls, out of scope.
- If the chat screen uses a ChangeNotifier/riverpod/bloc pattern, follow it
  exactly — do not introduce a second state pattern.
- LivenessTimeout: 120s constant in the provider (a settings knob is NOT
  required for parity; the TUI got one because it has a config file — match
  each platform's existing configurability level).
- The daemon's 110s stub is gone from the async path, but during migration
  the OLD /api/v1/chat still exists: the new path must NOT accidentally call
  it (test asserts the URL).
