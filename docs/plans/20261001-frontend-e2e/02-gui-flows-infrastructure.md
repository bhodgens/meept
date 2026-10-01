# GUI Flows Infrastructure (stub daemon server) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. After completing, report what
> you built, what files you touched, and any deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Build the in-process stub daemon server (HTTP + SSE) that the
  Flutter gui-flows tests run against, plus the test bootstrap helper.
  NO test flows in this leaf - only infrastructure + a smoke proof.
- **Dependencies:** none
- **Estimated Context:** ~85K
- **Concurrency Group:** A

## Goal

Flutter widget tests currently mock providers, so wire-shape bugs (the
dropped-terminal-event and pendingTurns-rebuild classes) ship undetected.
This leaf builds the inverse foundation: a real `ChatProvider` (and
friends) pumped inside `flutter test` against an in-process stub HTTP/SSE
server that speaks the daemon's actual wire shapes. The stub is Dart,
binds 127.0.0.1 on an ephemeral port, and serves ONLY the endpoints the
gui flows need, with response shapes copied from the Go handlers.

## Context

Repo: /Users/caimlas/git/meept. Flutter app lives in `ui/flutter_ui`.
Existing tests under `ui/flutter_ui/test/` mock the service layer; the
new world lives in `ui/flutter_ui/test/e2e/`.

Key files to understand before implementing:

- `ui/flutter_ui/lib/providers/chat_provider.dart` - the provider under
  test: `pendingTurns` map, ack handling, turn terminal delivery. Read
  how it constructs its service dependency (constructor injection point).
- `ui/flutter_ui/lib/services/` - the HTTP/SSE/WebSocket client layer the
  provider calls; the stub must answer the exact request paths these use.
- `internal/comm/http/` (Go, read-only reference) - the wire shapes to
  copy: chat submit response envelope, SSE event frame, session list
  shape. Field names MUST match; that is the point of the stub.
- `ui/flutter_ui/test/mocks/mock_websocket_service.dart` - existing mock
  idiom (what we are replacing for these tests, not deleting).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: ui/flutter_ui/test/e2e/stub_daemon.dart
class StubDaemon {
  static Future<StubDaemon> start({StubScript script});
  String get baseUrl;              // http://127.0.0.1:<ephemeral>
  void enqueueChatTurn({           // script one scripted turn
    required String turnId,
    required List<StubSseEvent> events,  // served on the stream endpoint
  });
  void enqueueSessionList(List<Map<String, dynamic>> sessions);
  int submitCount;                 // assertions on client->server calls
  Future<void> dispose();
}

class StubSseEvent { final String type; final Map<String, dynamic> payload; }

// Event payload shapes (Contract 1 in master.md — copy field names from
// the Go handlers, do not invent):
//   chat_message:  {"type":"chat_message","content":"...","turn_id":"..."}
//   agent_progress: {"type":"agent_progress","state":"...","turn_id":"..."}
//   terminal completion is a chat_message event carrying the final content.

// File: ui/flutter_ui/test/e2e/pump_app.dart
Future<(ProviderContainer, StubDaemon)> pumpRealApp(
  WidgetTester tester, {required StubDaemon daemon});
// Builds the real provider tree (real ChatProvider wired to the stub's
// baseUrl) inside tester.pumpWidget; returns the container for reads.
```

### What This Leaf Consumes

```
Production code (unmodified): ChatProvider and its service layer.
Go handlers (read-only): internal/comm/http for wire shapes.
```

## Tasks

### Task 1: StubDaemon HTTP core

**Objective:** Ephemeral-port HTTP server serving sessions list + chat
submit + SSE stream per script.

**Files:**
- Create: `ui/flutter_ui/test/e2e/stub_daemon.dart`
- Test: `ui/flutter_ui/test/e2e/stub_daemon_test.dart`

**Step 1: Write the test first** - start StubDaemon, `http.get` the
sessions endpoint, assert the scripted JSON comes back byte-shape-correct
(field names identical to the Go handler's).

**Step 2:** Implement with `HttpServer.bind(InternetAddress.loopbackIPv4, 0)`.
Route: sessions (GET list), chat/submit (POST, count it, return the
turn envelope), chat stream (GET, SSE frames from the script queue with
`data: <json>\n\n` framing and a terminal `[DONE]` sentinel if the Go
side sends one - check the handler).

**Step 3:** `cd ui/flutter_ui && flutter test test/e2e/stub_daemon_test.dart`
Expected: PASS

### Task 2: pumpRealApp bootstrap

**Objective:** Build the real provider tree against the stub.

**Files:**
- Create: `ui/flutter_ui/test/e2e/pump_app.dart`
- Test: extend `stub_daemon_test.dart`

**Step 1:** Read how ChatProvider receives its HTTP/SSE dependencies. If
it hardcodes a base URL, find the injection seam (config/provider
override). If NO injection seam exists, create one ADDITIVELY in the
service layer (a settable base-url parameter defaulting to current
behavior) - that is in-scope production code, flag it in the report.

**Step 2:** pumpRealApp pumps a minimal widget (or the real app shell if
cheap) inside a ProviderScope with the real ChatProvider pointed at the
stub. Must use `tester.runAsync` around any real socket I/O.

**Step 3:** Smoke proof test: submit a chat turn through the REAL provider
against the stub; assert `pendingTurns` gains an entry keyed by the
stub's turn_id and that submitCount == 1.

Run: `flutter test test/e2e` Expected: PASS

## Self-Verification Checklist

- [ ] Stub responses' field names verified against the Go handlers
- [ ] pumpRealApp returns a container with the REAL provider active
- [ ] Smoke test passes with -count=1 semantics (fresh run)
- [ ] No mock providers involved in the e2e path
- [ ] Any production-code change is the minimal base-url injection seam,
      flagged in the report

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Wire shapes match internal/comm/http handlers field-for-field
- [ ] Ephemeral port, loopback bind, clean dispose
- [ ] Real ChatProvider in the pump path; no service mocks
- [ ] Production changes limited to an additive injection seam

## Notes

- This leaf is the biggest in the tree; if you approach the time cap,
  land Tasks 1+2 and report - Task 3+ flows belong to leaf 03 anyway.
- SSE parsing differences between browser and VM matter: these tests run
  in the Flutter VM test environment; use the same client class the app
  uses so parse bugs are caught.
