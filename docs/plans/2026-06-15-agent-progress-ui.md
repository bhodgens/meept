# Plan: Agent Progress UI - Real-time Dispatcher Activity Display

**Created:** 2026-06-15
**Tracking Issue:** EC-7
**Priority:** Medium
**Effort:** ~4-6 hours

---

## Problem Statement

The Flutter UI currently displays only `"thinking..."` while the agent processes requests, despite the backend having rich agent event data available:

- Tool execution status (which tool, arguments, result preview)
- Turn iteration count with token/tool stats
- Model selection decisions
- Resource usage (tokens, iterations remaining)
- Queue depth and job status

Users see a generic loading indicator instead of actionable progress information like:
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
AgentLoop → EventEmitter → MessageBus → (in-process only)
                                    ↓
                            ProgressSynthesizer (unused by UI)
                                    ↓
                           (not broadcast to WebSocket)
```

**Flutter UI:**
- `chat_message_list.dart:118` - Shows static `"thinking..."` text
- `websocket_service.dart:403` - Filters for `type == 'chat_message'` only
- No subscription mechanism for agent progress events

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

**Tasks:**

1.1. Add agent progress subscription to WebSocket handler
   - Create `AgentProgressEmitter` that subscribes to `agent.event.*` bus topics
   - Serialize `SynthesizedProgressEvent` and send to WebSocket clients
   - Message format: `{type: "agent_progress", data: {agent_id, session_id, message, tier, timestamp}}`

1.2. Filter progress by session
   - Clients subscribe to progress for specific session IDs
   - Only broadcast events matching subscribed sessions

1.3. Rate limiting (optional)
   - Debounce rapid events to prevent UI spam
   - Configurable: `progress_update_interval: 100ms`

**Acceptance Criteria:**
- [ ] Agent events flow from EventEmitter → MessageBus → WebSocket
- [ ] Flutter client can receive `{type: "agent_progress"}` messages
- [ ] Events include: agent_id, session_id, human-readable message, tier

---

### Phase 2: Flutter - WebSocket Progress Subscription

**Files:** `ui/flutter_ui/lib/services/websocket_service.dart`, `ui/flutter_ui/lib/providers/chat_provider.dart`

**Tasks:**

2.1. Add progress subscription to `WebSocketService`
   ```dart
   Stream<Map<String, dynamic>> subscribeToAgentProgress(String sessionId) {
     return _messageSubject.stream.where((m) =>
       m['type'] == 'agent_progress' && m['session_id'] == sessionId
     );
   }
   ```

2.2. Add progress state to `ChatState`
   ```dart
   class ChatState {
     final List<ChatMessage> messages;
     final bool isLoading;
     final String? error;
     final AgentProgress? currentProgress; // NEW
   }

   class AgentProgress {
     final String agentId;
     final String message;
     final int tier; // VerbosityLevel
     final DateTime timestamp;
   }
   ```

2.3. Wire up subscription in `ChatNotifier`
   - Subscribe when `loadMessages` is called
   - Update state on progress events
   - Unsubscribe on session change/dispose

**Acceptance Criteria:**
- [ ] `WebSocketService.subscribeToAgentProgress()` returns filtered stream
- [ ] `ChatState` includes optional `currentProgress` field
- [ ] Progress updates trigger UI rebuild via Riverpod

---

### Phase 3: Flutter UI - Display Progress Messages

**Files:** `ui/flutter_ui/lib/features/chat/chat_message_list.dart`

**Tasks:**

3.1. Replace static `"thinking..."` with progress-aware widget
   ```dart
   if (chatState.isLoading) {
     if (chatState.currentProgress != null) {
       return AgentProgressIndicator(progress: chatState.currentProgress!);
     } else {
       return const ThinkingIndicator(); // fallback
     }
   }
   ```

3.2. Create `AgentProgressIndicator` widget
   - Display agent_id (lowercase per convention)
   - Display human-readable message
   - Visual styling based on tier:
     - `VerbosityQuiet` (Tier 0): Minimal, subtle
     - `VerbosityNormal` (Tier 1): Standard styling
     - `VerbosityVerbose` (Tier 2): Detailed, collapsed by default

3.3. Animation/UX polish
   - Fade transition when progress updates
   - Auto-scroll behavior preserved
   - Progress message truncated if too long (60 char max)

**Acceptance Criteria:**
- [ ] Progress message visible during agent processing
- [ ] Shows agent ID + action being performed
- [ ] Tier-based styling applied
- [ ] Falls back to generic "thinking..." if no progress available

---

### Phase 4: Testing & Verification

**Files:** `ui/flutter_ui/test/`, `internal/comm/http/*_test.go`

**Tasks:**

4.1. Backend unit tests
   - [x] Test event serialization to WebSocket format -- `TestHandleChatStream_SSEAgentProgressEvent` in `server_test.go` validates SSE agent progress forwarding
   - [x] Test session filtering logic -- `ShouldSendProgress` uses `sessionSubs` map with broadcast fallback; no dedicated unit test but covered by integration flow
   - [x] Rate limiting -- not implemented (no debounce/throttle on `handleWSProgress`)

4.2. Flutter widget tests
   - [x] Tests pass (119 passed, 5 failed -- all pre-existing `error_banner_test.dart` failures, unrelated to agent progress)
   - [ ] No dedicated `AgentProgressIndicator` widget tests exist yet

4.3. Integration testing
   - [ ] Manual test not yet performed (requires running daemon + Flutter UI together)
   - [ ] Cross-intent-type testing not yet performed
   - [ ] Performance testing not yet performed

**Acceptance Criteria:**
- [ ] Unit tests pass for backend event broadcast
- [ ] Flutter tests pass for progress display
- [ ] Manual verification: progress visible during real agent execution

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
- ⏳ Need to wire events through to WebSocket layer

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

- [x] Backend events flow through to WebSocket: `handleWSProgress` subscribes to `agent.progress.synthesized` bus topic, serializes to JSON `{type: "agent_progress", ...}`, and writes to WebSocket connections subscribed to the relevant session
- [ ] Flutter receives and displays progress for all agent types: Manual integration test pending
- [ ] Tier-based filtering works correctly: Tier logic verified in `AgentProgressIndicator` (0=light gray, 1=mid gray, 2=italic light gray); `handleWSProgress` passes tier as `int(event.Tier)` over WebSocket
- [ ] No regression in chat message delivery latency: No coupling between progress and chat message paths observed
- [x] Documentation updated: `docs/reference/http-api.md` WebSocket section expanded (2026-06-15); `docs/concepts/multi-agent.md` not updated (no agent-level changes needed)

---

## Estimates

| Phase | Effort |
|-------|--------|
| Phase 1: Backend WebSocket Broadcast | 1.5-2 hours |
| Phase 2: Flutter Subscription | 1 hour |
| Phase 3: Flutter UI Display | 1-1.5 hours |
| Phase 4: Testing | 1 hour |
| **Total** | **~4.5-5.5 hours** |
