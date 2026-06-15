# Plan: Agent Progress UI - Real-time Dispatcher Activity Display

**Created:** 2026-06-15
**Tracking Issue:** EC-7
**Priority:** Medium
**Effort:** ~4-6 hours

---

## Problem Statement (Resolved)

The Flutter UI displayed only `"thinking..."` while the agent processed requests, despite the backend having rich agent event data available:

- Tool execution status (which tool, arguments, result preview)
- Turn iteration count with token/tool stats
- Model selection decisions
- Resource usage (tokens, iterations remaining)
- Queue depth and job status

Users now see actionable progress information instead:
- `"coder: executing ReadFile..."`
- `"dispatcher: classifying intent (code)...`
- `"analyst: turn 2 done (3 tool calls, 245 tokens)"`

---

## Background

### Current Architecture

**Backend (Go):**
- `internal/agent/events.go` - 18 typed agent event types with structured data
- `internal/agent/progress_synthesizer.go` - Converts raw events to human-readable messages with verbosity tiers
- `internal/agent/emitter.go` - EventEmitter bridges typed events to message bus
- `internal/comm/http/server.go` - Unified HTTP server with WebSocket support at `/ws`
- `ui/flutter_ui/lib/services/websocket_service.dart` - Flutter WebSocket client

**Current Data Flow:**
```
AgentLoop → EventEmitter → bus.Publish("agent.event.*")
                         → ProgressSynthesizer → bus.Publish("agent.progress.synthesized")
                                                    ↓
                                           handleWSProgress → WebSocket Hub
                                                        ↓
                                          ShouldSendProgress → client connections
```

SSE path (via `handleChatStream`):
```
EventEmitter → bus.Publish("agent.progress")
               ↓
         SSE event stream → client
```

**Flutter UI (existing code, before this plan):**
- `chat_message_list.dart:118` - Shows fallback `ThinkingIndicator` when no progress available
- `websocket_service.dart:403` - Filters for `type == 'chat_message'` only (chat subscription)
- No agent progress subscription or display widget existed yet

---

## Solution Overview

Expose real-time agent progress to Flutter UI by:

1. **Backend:** Bridge agent progress events from message bus to WebSocket channel
2. **Flutter:** Add subscription for agent progress events
3. **Flutter UI:** Replace `"thinking..."` with live progress messages

---

## Implementation Phases

### Phase 1: Backend - WebSocket Agent Progress Broadcast

**Files:** `internal/comm/http/server.go`, `internal/agent/handler.go`

> **Status: COMPLETE** -- implemented and merged.
>
> - SSE: `handleChatStream` subscribes to `agent.progress` and forwards as SSE events (`api_handlers.go:115-175`)
> - WebSocket: `handleWSProgress` subscribes to `agent.progress.synthesized` (via ProgressSynthesizer), validates fields, serializes to `{type: "agent_progress", ...}` and broadcasts session-scoped (`server.go:355-448`)
> - Session filtering via `wsHub.ShouldSendProgress(conn, sessionID)`
> - Rate limiting: not implemented (no debounce/throttle)

**Acceptance Criteria:**
- [x] Agent events flow from EventEmitter → MessageBus → WebSocket: SSE path via `agent.progress` + WS path via `agent.progress.synthesized` → ProgressSynthesizer → `handleWSProgress` → WebSocket Hub
- [x] Flutter client receives `{type: "agent_progress"}` messages
- [x] Events include: agent_id, session_id, human-readable message, tier, source_event, timestamp

---

### Phase 2: Flutter - WebSocket Progress Subscription

**Files:** `ui/flutter_ui/lib/services/websocket_service.dart`, `ui/flutter_ui/lib/providers/chat_provider.dart`

> **Status: COMPLETE** -- implemented and merged.
>
> - `subscribeToAgentProgress(sessionId)` in `websocket_service.dart:451-483`: sends `subscribe` with `channel: 'progress'`, filters on `type == 'agent_progress' && session_id == sessionId`
> - `ChatState.currentProgress` field present (`chat_provider.dart:25`): optional `AgentProgress?`
> - `AgentProgress` model in `api_models.dart` with `fromJson` supporting flat and nested formats
> - `ChatNotifier._progressSubscription` subscribed in `loadMessages` (line 138-141), cancelled in `dispose` (line 360-361)

**Acceptance Criteria:**
- [x] `WebSocketService.subscribeToAgentProgress()` returns filtered stream
- [x] `ChatState` includes optional `currentProgress` field
- [x] Progress updates trigger UI rebuild via Riverpod

---

### Phase 3: Flutter UI - Display Progress Messages

**Files:** `ui/flutter_ui/lib/features/chat/chat_message_list.dart`

> **Status: COMPLETE** -- implemented and merged.
>
> - `AgentProgressIndicator` widget in `agent_progress_indicator.dart`: shows `agent_id` (lowercase), human-readable message, tier-based colors (0=lightGray, 1=midGray, 2=italic+lightGray), truncation at 60 chars
> - `AnimatedSwitcher` with 150ms fade in `chat_message_list.dart:104-114`: uses `ValueKey` based on message+timestamp for smooth transitions
> - Fallback `ThinkingIndicator` shown when `currentProgress == null` (line 115-138)
> - Auto-scroll preserved (existing `_scrollToBottom` logic untouched)

**Acceptance Criteria:**
- [x] Progress message visible during agent processing
- [x] Shows agent ID + action being performed
- [x] Tier-based styling applied (tier 0: light gray normal, tier 1: mid gray normal, tier 2: light gray italic)
- [x] Falls back to generic "thinking..." if no progress available

---

### Phase 4: Testing & Verification

**Files:** `ui/flutter_ui/test/`, `internal/comm/http/*_test.go`

**Tasks:**

4.1. Backend unit tests
   - [-] SSE event serialization: `TestHandleChatStream_SSEAgentProgressEvent` in `server_test.go` validates SSE agent progress forwarding; no dedicated Go test for `handleWSProgress` WebSocket path yet
   - [-] Session filtering: `ShouldSendProgress` uses `sessionSubs` map with broadcast fallback; no dedicated unit test but covered by integration flow
   - [ ] Rate limiting: not implemented (no debounce/throttle on `handleWSProgress`)

4.2. Flutter widget tests
   - [-] General Flutter tests pass (119 passed, 5 failed -- all pre-existing `error_banner_test.dart` failures, unrelated to agent progress)
   - [ ] No dedicated `AgentProgressIndicator` widget tests exist yet

4.3. Integration testing
   - [ ] Manual end-to-end test not yet performed (requires running daemon + Flutter UI together)
   - [ ] Cross-intent-type testing not yet performed
   - [ ] Performance/load testing not yet performed

**Acceptance Criteria:**
- [ ] Dedicated Go unit test for `handleWSProgress` WebSocket serialization
- [ ] Flutter widget tests for `AgentProgressIndicator`
- [ ] Manual integration verification: progress visible during real agent execution

---

## Message Format Specification

### Backend → Flutter WebSocket Message

The server sends a flat message (not wrapped in `data`):

```json
{
  "type": "agent_progress",
  "session_id": "abc-123",
  "agent_id": "coder",
  "message": "coder: executing ReadFile (internal/file/read)",
  "tier": 1,
  "source_event": "tool_execution_start",
  "timestamp": "2026-06-15T10:30:00Z"
}
```

**Note:** The `AgentProgress.fromJson` Flutter parser accepts both flat and nested (`data`-wrapped) formats for forward compatibility.

### Tier Mapping (VerbosityLevel)

| Tier | Name | Example Messages | Display Policy |
|------|------|------------------|----------------|
| 0 | Quiet | `"coder: completed (tool_use)"` | Always show |
| 1 | Normal | `"coder: ok ReadFile: package main..."` | Always show |
| 2 | Verbose | `"coder: turn 2 done (3 tool calls, 245 tokens)"` | Collapse if spam |

---

## Dependencies

- ✅ Agent events already defined (`internal/agent/events.go`)
- ✅ ProgressSynthesizer already implemented (`internal/agent/progress_synthesizer.go`)
- ✅ WebSocket infrastructure in place
- ✅ Events wired through to WebSocket layer (`handleWSProgress` subscribes to `agent.progress.synthesized`)

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Event spam floods UI/WS | High | Rate limiting, tier-based filtering |
| Progress out of sync with messages | Medium | Session-scoped subscription, timestamp ordering |
| WebSocket payload bloat | Low | Synthesized messages are small (~200 bytes) |
| Flutter performance impact | Low | Only latest progress kept in state |

---

## Future Enhancements (Out of Scope)

- Progressive disclosure: Expand verbose messages on tap
- Progress history: Show timeline of completed steps
- Tool result previews inline
- Per-agent color coding
- Estimated time remaining (requires metrics analysis)
- Skeleton screens during cold start

---

## Success Metrics

- **Qualitative:** Users can tell what agent is doing without checking logs
- **Quantitative:** Support tickets about "agent stuck" reduce by 50%
- **UX:** Time-to-understand-agent-state < 2 seconds

---

## Review Checklist

Before marking complete:

- [x] Backend events flow through to WebSocket: `handleWSProgress` subscribes to `agent.progress.synthesized` bus topic, serializes to JSON `{type: "agent_progress", ...}`, and writes to WebSocket connections via `ShouldSendProgress` session-scoped filtering
- [ ] Flutter receives and displays progress for all agent types: Manual integration test pending
- [x] Tier-based filtering works correctly: Tier logic verified in `AgentProgressIndicator` (0=light gray normal, 1=mid gray normal, 2=light gray italic); `handleWSProgress` passes tier as `int(event.Tier)` over WebSocket
- [ ] No regression in chat message delivery latency: Progress path decoupled from chat message delivery (separate bus topics and subscriptions), but no formal latency regression test performed
- [ ] Rate limiting implemented: Currently not present -- rapid agent events may arrive at full frequency without debounce/throttle on `handleWSProgress`
- [x] Documentation updated: `docs/reference/http-api.md` WebSocket section expanded (2026-06-15) with SSE vs WebSocket schema difference; `docs/concepts/multi-agent.md` not updated (no agent-level changes needed)

---

## Estimates

Phases 1-3 are complete. Remaining work:

| Phase | Effort | Status |
|-------|--------|--------|
| Phase 1: Backend WebSocket Broadcast | 0 hours | ✅ Done |
| Phase 2: Flutter Subscription | 0 hours | ✅ Done |
| Phase 3: Flutter UI Display | 0 hours | ✅ Done |
| Phase 4.1: Backend unit test for `handleWSProgress` | 0.5-1 hour | Remaining |
| Phase 4.2: Flutter widget tests for `AgentProgressIndicator` | 0.5-1 hour | Remaining |
| Phase 4.3: Manual integration testing | 0.5 hour | Remaining |
| **Total remaining** | **~1.5-2.5 hours** | |
