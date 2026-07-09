# Session Title Generation and Updates

This document describes the end-to-end flow for session title generation and mid-session updates in Meept.

## Overview

Session titles are generated in two ways:
1. **Initial generation**: When a session starts, the title is generated from the first user message
2. **Mid-session refresh**: Every 5 turns, the title is updated based on conversation progress

## Architecture

```
User Message → Agent Loop → Session Handler → Summarizer/Refresher → Session Store → Event Bus → Flutter UI
```

## Components

### Backend (Go)

| File | Component | Purpose |
|------|-----------|---------|
| `internal/session/summarizer.go` | `Summarizer`, `extractSimpleResult` | Initial title generation from first message |
| `internal/session/refresher.go` | `SessionRefresher` | Mid-session title updates |
| `internal/session/session.go` | `Handler.handleGenerateDescription`, `Handler.handleRefreshTitle` | RPC handlers for title operations |
| `internal/agent/loop.go` | `turnCounter`, `maybeRefreshTitle` | Tracks conversation turns, triggers periodic refresh |
| `internal/daemon/components.go` | Component wiring | Wires refresher to agent loop |

### Frontend (Flutter)

| File | Component | Purpose |
|------|-----------|---------|
| `ui/flutter_ui/lib/models/api_models.dart` | `_normaliseSessionJson` | Prefers LLM-generated names over generic fallbacks |
| `ui/flutter_ui/lib/services/websocket_service.dart` | `subscribeToSessionTitles()` | Subscribes to title update events |
| `ui/flutter_ui/lib/services/session_notifier.dart` | `_initWebSocket`, `_updateSessionTitle` | Updates session state on title events |
| `ui/flutter_ui/lib/features/chat/chat_view.dart` | `ChatView` | Displays session title in header |

## Flow 1: Initial Title Generation

1. User sends first message in new session
2. Agent loop calls `session.generate_description` RPC
3. `handleGenerateDescription` receives request with `first_message`
4. `Summarizer.GenerateDescription` is called:
   - If LLM client available: Sends prompt to LLM, expects JSON `{name, description}`
   - If LLM unavailable: Falls back to `extractSimpleResult` (extracts first non-filler word)
5. Result saved to session store via `UpdateName` and `UpdateDescription`
6. Flutter receives updated session via existing session fetch

**Example LLM prompt:**
```
You are a session summarizer. Generate a JSON object with:
1. "name": A single lowercase word that captures the topic
2. "description": A brief 3-8 word description in "category: detail" format

Output ONLY valid JSON. Example:
{"name": "debugging", "description": "coding: fixed null pointer in auth"}
```

**Fallback behavior:**
- Input: `"what are your capabilities"`
- Output: `name="capabilities"`, `description="what are your capabilities"`

## Flow 2: Mid-Session Title Refresh

1. Agent loop increments `turnCounter` after each response
2. Every 5 turns (`turnCounter % 5 == 0`), `maybeRefreshTitle` is called
3. Agent loop calls `session.refresh_title` RPC with:
   - `session_id`
   - `topic` (dominant intent from session tracker)
   - `turn_count`
   - `keywords` (extracted from recent messages)
4. `handleRefreshTitle` receives request
5. `SessionRefresher.Refresh` is called:
   - If LLM client available: Sends prompt with conversation context
   - If LLM unavailable: Returns fallback `session-{turnCount}`
6. Result saved via `UpdateName` and `UpdateDescription`
7. Event published: `session.title_updated` with `{session_id, name, description}`
8. Flutter receives event via WebSocket subscription
9. `SessionNotifier._updateSessionTitle` updates session state
10. UI rebuilds with new title

**Example LLM prompt for refresh:**
```
You are updating a session title based on conversation progress.
Generate a JSON object with:
1. "name": A single lowercase word capturing the dominant topic
2. "description": A brief 3-6 word description in "category: detail" format

Categories: coding, research, task, personal, creative, system

Session has 10 turns. Topic: debugging. Keywords: [null, pointer, fix]
Provide updated title.
```

**Expected response:**
```json
{"name": "debugging", "description": "coding: fixed null pointer"}
```

## Event Format

### `session.title_updated`

```json
{
  "type": "event",
  "topic": "session.title_updated",
  "payload": {
    "session_id": "session-abc123",
    "name": "debugging",
    "description": "coding: fixed null pointer"
  }
}
```

## RPC Methods

### `session.generate_description`

**Request:**
```json
{
  "session_id": "session-abc123",
  "first_message": "How do I fix a null pointer?",
  "project_name": "meept"
}
```

**Response:**
```json
{
  "name": "debugging",
  "description": "coding: fixed null pointer"
}
```

### `session.refresh_title`

**Request:**
```json
{
  "session_id": "session-abc123",
  "topic": "debugging",
  "turn_count": 10,
  "keywords": ["null", "pointer", "fix"],
  "first_message": "How do I fix a null pointer?"
}
```

**Response:**
```json
{
  "name": "debugging",
  "description": "coding: fixed null pointer"
}
```

## Flutter Display Logic

The Flutter session normalizer (`_normaliseSessionJson`) decides which title to display:

```dart
final isGenericName = name == 'default' ||
    name == 'Untitled' ||
    name == 'chat' ||
    name.isEmpty;
final displayTitle = (name.isNotEmpty && !isGenericName)
    ? name  // Use LLM-generated name
    : (description ?? name);  // Fall back to description
```

**Examples:**
- `name="debugging"`, `description=...` → Display: "debugging"
- `name="default"`, `description="fixing null pointer"` → Display: "fixing null pointer"
- `name="chat"`, `description=""` → Display: "chat"

## Testing

### Backend Tests

```bash
# Summarizer unit tests (extractSimpleResult)
go test ./internal/session/... -v -run TestExtractSimpleResult

# Refresher unit tests
go test ./internal/session/... -v -run TestSessionRefresher

# Handler integration tests
go test ./internal/session/... -v -run TestHandler_RefreshTitle
```

### Frontend Tests

```bash
# Flutter widget tests (TODO: implement)
cd ui/flutter_ui && flutter test test/models/api_models_test.dart
```

## Related Files

- `docs/superpowers/plans/2026-07-07-session-title-improvements.md` - Original implementation plan
- `internal/session/summarizer_test.go` - Unit tests for summarization
- `internal/session/refresher_test.go` - Unit tests for refresher
- `internal/session/refresh_title_handler_test.go` - Handler integration tests
- `ui/flutter_ui/lib/models/api_models.dart` - Flutter session model with normalizer
