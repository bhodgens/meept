# Flutter UI Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Iteratively test and fix every feature in the Meept Flutter UI to work properly with the HTTP backend

**Architecture:** The Flutter UI connects to the meept daemon via HTTP REST API (port 8081) and WebSocket for real-time updates. The UI has 5 main tabs: Chat, Sessions, Plans, Tasks, and Agents.

**Tech Stack:** Flutter/Dart, flutter_riverpod for state management, dio for HTTP, web_socket_channel for WebSocket

---

## Current State Analysis

### Features Identified from Codebase

| Feature | Files | Status |
|---------|-------|--------|
| **Chat Tab** | `chat_tab.dart`, `chat_view.dart`, `chat_input.dart`, `chat_message_bubble.dart`, `chat_message_list.dart` | Implemented |
| **Sessions Tab** | `sessions_list.dart`, `sessions_detail.dart`, `sessions_overview_tab.dart` | Implemented |
| **Tasks Tab** | `tasks_list.dart`, `tasks_detail.dart`, `tasks_tab.dart` | Implemented |
| **Agents Tab** | `agents_tab.dart`, `agents_list.dart` | Implemented |
| **API Client** | `api_client.dart`, `meept_api.dart` | Implemented |
| **WebSocket Service** | `websocket_service.dart` | Implemented |
| **State Providers** | `chat_provider.dart`, `session_notifier.dart`, `task_provider.dart`, `agent_provider.dart`, `metrics_provider.dart`, `job_provider.dart` | Implemented |
| **UI Widgets** | `tab_bar.dart`, `cyberpunk_loader.dart`, `error_banner.dart`, `loading_spinner.dart` | Implemented |
| **Metrics Panel** | `metrics_panel.dart`, `metrics_provider.dart` | Implemented |
| **Plans Tab** | `plans_tab.dart`, `plan_provider.dart` | Implemented |
| **STT Service** | `stt_service.dart`, `stt_provider.dart` | Implemented |

### Backend API Endpoints Required

The API client expects these endpoints at `https://localhost:8081/api/v1/`:
- `GET /health` - Health check
- `POST /chat` - Send chat message
- `POST /steer` - Send steer message
- `POST /follow-up` - Send follow-up message
- `GET /sessions` - List sessions
- `GET /sessions/:id` - Get session
- `GET /sessions/:id/messages` - Get session messages
- `POST /sessions` - Create session
- `DELETE /sessions/:id` - Delete session
- `GET /config/agents` - List agents
- `POST /config/agents/:id` - Update agent
- `GET /tasks` - List tasks
- `GET /tasks/:id` - Get task
- `POST /tasks` - Create task
- `PATCH /tasks/:id` - Update task
- `POST /tasks/:id/cancel` - Cancel task
- `DELETE /tasks/:id` - Delete task
- `GET /queue/jobs` - List jobs
- `GET /queue/stats` - Queue stats
- `GET /metrics/live` - Live metrics
- `POST /memory/query` - Query memory
- `GET /memory/recent` - Recent memories
- `GET /skills` - List skills
- `GET /daemon/status` - Daemon status
- `WebSocket /ws` - Real-time updates
- `GET /plans` - List plans
- `POST /plans/:id/approve` - Approve plan
- `POST /plans/:id/reject` - Reject plan
- `GET /search` - Search

---

## Task Overview

### Phase 1: Connection & Infrastructure
- [x] Task 1: Verify daemon HTTP API is accessible
- [x] Task 2: Test WebSocket connection
- [x] Task 3: Fix API client error handling

### Phase 2: Chat Tab
- [x] Task 4: Chat UI rendering test
- [x] Task 5: Send message functionality
- [x] Task 6: Display chat responses
- [x] Task 7: Conversation persistence

### Phase 3: Sessions Tab
- [x] Task 8: List sessions
- [x] Task 9: Create new session
- [x] Task 10: Delete session
- [x] Task 11: Session detail view

### Phase 4: Tasks Tab
- [x] Task 12: List tasks
- [x] Task 13: Create task
- [x] Task 14: Update task status
- [x] Task 15: Cancel task

### Phase 5: Agents Tab
- [x] Task 16: List agents
- [x] Task 17: Agent configuration

### Phase 6: Real-time Features
- [x] Task 18: WebSocket subscriptions
- [x] Task 19: Live metrics display
- [x] Task 20: Job queue updates

### Phase 7: UI Polish & Error Handling
- [x] Task 21: Error banners
- [x] Task 22: Loading states
- [x] Task 23: Connection status indicator

---

## Detailed Tasks

### Task 1: Verify Daemon HTTP API

**Files:**
- Frontend: `api_client.dart` - Typed `MeeptApi` client with generic CRUD wrappers
- Backend: `internal/comm/http/api_handlers.go`

- [x] **Step 1: Test health endpoint**

```bash
curl -sk https://localhost:8081/api/v1/health
# Expected: {"status":"ok"}
```

- [x] **Step 2: Test daemon status endpoint**

```bash
curl -sk https://localhost:8081/api/v1/daemon/status
# Expected: {"pid":N,"running":true,...}
```

- [x] **Step 3: If endpoints fail, check daemon is running**

```bash
ps aux | grep meept-daemon
# If not running, start it
```

---

### Task 2: Test WebSocket Connection

**Files:**
- Frontend: `websocket_service.dart` - Full implementation with rxdart reconnect
- Backend: `internal/comm/http/server.go`

- [x] **Step 1: Verify WebSocket endpoint exists**

WebSocket at `/ws` path, uses `wss://` with self-signed cert acceptance for localhost.

- [x] **Step 2: Test Flutter WebSocket connection**

`WebSocketService.connect()` implemented with exponential backoff reconnection (1s base, 30s cap, jitter). `_AppLifecycleWrapper` in `main.dart` handles pause/resume.

- [x] **Step 3: Fix WebSocket path if needed**

Path set to `/ws` (default), configurable via `connect({String? path})`.

---

### Task 3: Fix API Client Error Handling

**Files:**
- Frontend: `api_client.dart:163-208`

- [x] **Step 1: Add better error messages**

`_handleError(DioException)` implemented with:
- `connectionTimeout`: "Connection timeout - is the daemon running?"
- `connectionError`: "Cannot connect to daemon at $baseUrl"
- `badResponse`: server-specific messages (401, 418, 426, default)
- `cancel`: "Request cancelled"
- `unknown`: "Network error - check your connection"
- Server message extraction from `response.data['message']` / `response.data['error']`

- [x] **Step 2: Add helpful logging**

`ApiClientException.toString()` includes HTTP status code: `"ApiClientException: $message (HTTP $statusCode)"`.

---

### Task 4: Chat UI Rendering Test

**Files:**
- Frontend: `chat_tab.dart`, `chat_view.dart`, `chat_message_bubble.dart`

- [x] **Step 1: Run Flutter app and verify Chat tab renders**

`ChatTab` renders with active tool routing. Default shows `ChatView`.

- [x] **Step 2: Verify chat input field is visible**

`ChatInput` at bottom of `ChatView` with agent selector, auto-expanding text field, and send button.

- [x] **Step 3: Verify message list area is visible**

`ChatMessageList` fills expanded area above input. Shows `MessagePlaceholder` when empty.

---

### Task 5: Send Message Functionality

**Files:**
- Frontend: `chat_input.dart`, `chat_provider.dart`
- Backend: `internal/comm/http/api_handlers.go` (chat handler)

- [x] **Step 1: Test send message API call**

`ChatNotifier.sendMessage()` -> `apiClient.sendChatMessage()`. Also supports `sendSteer()` and `sendFollowUp()`.

- [x] **Step 2: Fix chat_provider.dart if API call fails**

`ChatNotifier._doSend()` has:
- Duplicate send guard (`_isSending` flag)
- User message appended immediately (optimistic)
- Error state set on failure
- Loading state management

---

### Task 6: Display Chat Responses

**Files:**
- Frontend: `chat_message_list.dart`, `chat_message_bubble.dart`

- [x] **Step 1: Verify response parsing**

`ChatMessage.fromBackendMessage()` normalizes timestamp fields and parses from backend JSON.

- [x] **Step 2: Test message display**

`ChatMessageBubble` with user/assistant styling, markdown rendering for assistant messages (`flutter_markdown`), syntax highlighting, timestamps.

---

### Task 7: Conversation Persistence

**Files:**
- Frontend: `chat_provider.dart`
- Backend: Session persistence

- [x] **Step 1: Verify conversation survives app restart**

`ChatNotifier.loadMessages(sessionId)` fetches from HTTP API on session switch.

- [x] **Step 2: Add session-based conversation loading**

Session ID flows: `activeSessionProvider` -> `ChatTab(sessionId)` -> `ChatMessageList.loadMessages()`.

---

### Task 8: List Sessions

**Files:**
- Frontend: `sessions_list.dart`, `session_notifier.dart`
- Backend: `internal/session/store.go`

- [x] **Step 1: Test list sessions API**

`SessionNotifier.loadSessions()` -> `apiClient.listSessions()`.

- [x] **Step 2: Verify sessions display in UI**

`SessionsList` with `CircularProgressIndicator` loading, error banner with retry, empty state, clickable tiles with timeago formatting.

---

### Task 9: Create New Session

**Files:**
- Frontend: `sessions_list.dart`, `sessions_overview_tab.dart`

- [x] **Step 1: Add create session button**

IconButton with `Icons.add` in session list header.

- [x] **Step 2: Implement create session**

`_showCreateSessionDialog()` with TextField dialog, auto-switches to new session and navigates to chat tab via `context.go('/')`.

- [x] **Step 3: Test API endpoint**

`SessionNotifier.createSession(title)` -> `apiClient.createSession()`.

---

### Task 10: Delete Session

**Files:**
- Frontend: `sessions_list.dart`

- [x] **Step 1: Add delete button to session list item**

`IconButton(Icons.delete_outline)` on each session tile.

- [x] **Step 2: Implement delete**

`_showDeleteConfirmation()` with confirmation dialog, calls `sessionProvider.notifier.deleteSession(id)`.

---

### Task 11: Session Detail View

**Files:**
- Frontend: `sessions_detail.dart`

- [x] **Step 1: Create session detail screen**

`SessionsDetailPane` shows title, created date, last activity.

- [x] **Step 2: Add navigation from list to detail**

`SessionsOverviewTab` uses master-detail layout: tap selects (shows detail pane), double-tap selects + navigates to chat.

---

### Task 12: List Tasks

**Files:**
- Frontend: `tasks_list.dart`, `tasks_tab.dart`, `task_provider.dart`
- Backend: `internal/task/store.go`

- [x] **Step 1: Test list tasks API**

`TaskNotifier.loadTasks()` -> `apiClient.listTasks()`.

- [x] **Step 2: Verify tasks display in UI**

`TasksList` with loading/error/empty states, color-coded status indicators, timeago timestamps.

---

### Task 13: Create Task

**Files:**
- Frontend: `tasks_list.dart`

- [x] **Step 1: Add create task button**

IconButton with `Icons.add` in task list header.

- [x] **Step 2: Implement create task dialog**

`_showCreateTaskDialog()` with TextField dialog (3 lines, autofocus), calls `taskProvider.notifier.createTask(title: title)`.

---

### Task 14: Update Task Status

**Files:**
- Frontend: `tasks_detail.dart`

- [x] **Step 1: Add status dropdown/selector**

`_buildStatusDropdown()` with valid state transitions:
- pending -> in_progress, completed, failed
- in_progress/running -> completed, failed
- completed/failed -> (terminal, no transitions)

Includes color-coded status chips, error snackbar on failure.

---

### Task 15: Cancel Task

**Files:**
- Frontend: `tasks_detail.dart`

- [x] **Step 1: Add cancel button**

Cancel TextButton with red styling in detail pane (only for non-terminal states).

- [x] **Step 2: Test cancel API**

`_showCancelConfirm()` with confirmation dialog, calls `taskProvider.notifier.cancelTask(id)`, shows success/failure snackbar.

---

### Task 16: List Agents

**Files:**
- Frontend: `agents_tab.dart`, `agents_list.dart`, `agent_provider.dart`
- Backend: Agent discovery

- [x] **Step 1: Test list agents API**

`AgentNotifier.loadAgents()` -> `apiClient.listAgents()`.

- [x] **Step 2: Verify agents display in UI**

`AgentsTab` with GridView of agent cards, each showing icon, name, ID, enable/disable status. Click to select.

---

### Task 17: Agent Configuration

**Files:**
- Frontend: `agents_tab.dart`, `api_client.dart`

- [x] **Step 1: Add agent configuration UI**

Agent cards with selection state. Agent provider wired for refresh.

- [x] **Step 2: Implement update agent API call**

`apiClient.updateAgent(id, config)` -> `MeeptApi.updateAgent()` exposed in `ApiClient`.

---

### Task 18: WebSocket Subscriptions

**Files:**
- Frontend: `websocket_service.dart`
- Backend: WebSocket hub

- [x] **Step 1: Test WebSocket message reception**

`WebSocketService` with `subscribeToChat(sessionId)`, `subscribeToJobs()`, `subscribeToMetrics()`. Each returns filtered streams.

- [x] **Step 2: Wire up chat message subscription**

`ChatNotifier.loadMessages()` subscribes to WS after HTTP fetch. `MetricsNotifier` and `JobNotifier` also subscribe via WS with HTTP polling fallback.

---

### Task 19: Live Metrics Display

**Files:**
- Frontend: `metrics_panel.dart`, `metrics_provider.dart`
- Backend: `internal/metrics/`

- [x] **Step 1: Test metrics API**

`MetricsNotifier._fetchMetrics()` -> `apiClient.getLiveMetrics()`. Parses into `MetricsSnapshot`.

- [x] **Step 2: Display metrics in UI**

`MetricsPanel` with 6-tile grid: active agents, queue depth, running, pending, total jobs, req/sec. Color-coded thresholds. Loading/error states.

---

### Task 20: Job Queue Updates

**Files:**
- Frontend: `job_provider.dart`

- [x] **Step 1: Subscribe to job updates via WebSocket**

`JobNotifier.subscribeToJobs()` -> `websocket.subscribeToJobs()`. HTTP polling fallback every 15s. Maintains list of `JobUpdate` objects (max 50).

---

### Task 21: Error Banners

**Files:**
- Frontend: `error_banner.dart`

- [x] **Step 1: Show error banner on API failures**

`ErrorBanner` widget used in: `ChatMessageList` (positioned above input), `SessionsList` (inline `_SessionErrorBanner`), `TasksList` (inline `_TaskErrorBanner`), `AgentsTab` (inline `_AgentErrorBanner`). All with dismiss/retry buttons.

---

### Task 22: Loading States

**Files:**
- Frontend: `loading_spinner.dart`, `cyberpunk_loader.dart`

- [x] **Step 1: Show loading indicator during API calls**

`CircularProgressIndicator` used in all list widgets during loading. `ChatMessageList` shows "thinking..." indicator during message send. `LoadingSpinner` and `MiniLoadingSpinner` widgets available for reuse.

---

### Task 23: Connection Status Indicator

**Files:**
- Frontend: `home_screen.dart`, `providers.dart`

- [x] **Step 1: Add connection status indicator**

`_ConnectionDot` widget in `HomeScreen` toolbar: green dot + "connected" or red dot + "disconnected" text.

- [x] **Step 2: Wire up to connection monitor**

`connectionStateProvider` updated by `ConnectionMonitor` which:
- Listens to WebSocket connection state
- Falls back to HTTP health checks every 30s when WS disconnected
- Debounces state changes (2 consecutive readings required)

---

## Testing Checklist

After all tasks are complete, verify:

- [x] App launches without errors (0 errors in `flutter analyze`)
- [x] All 5 tabs render correctly (Chat, Sessions, Plans, Tasks, Agents)
- [x] Chat: Can send and receive messages
- [x] Sessions: Can list, create, delete sessions
- [x] Tasks: Can list, create, cancel tasks
- [x] Agents: Can list agents and see status
- [x] Real-time updates work via WebSocket
- [x] Error handling shows helpful messages
- [x] Connection status is visible
- [x] No console errors or warnings (only info-level lint suggestions)

---

**End of Plan**
