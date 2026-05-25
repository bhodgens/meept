# Flutter UI Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Iteratively test and fix every feature in the Meept Flutter UI to work properly with the HTTP backend

**Architecture:** The Flutter UI connects to the meept daemon via HTTP REST API (port 8081) and WebSocket for real-time updates. The UI has 4 main tabs: Chat, Sessions, Tasks, and Agents.

**Tech Stack:** Flutter/Dart, flutter_riverpod for state management, dio for HTTP, web_socket_channel for WebSocket

---

## Current State Analysis

### Features Identified from Codebase

| Feature | Files | Status |
|---------|-------|--------|
| **Chat Tab** | `chat_tab.dart`, `chat_view.dart`, `chat_input.dart`, `chat_message_bubble.dart`, `chat_message_list.dart` | Needs testing |
| **Sessions Tab** | `sessions_list.dart`, `sessions_detail.dart`, `sessions_overview_tab.dart` | Needs testing |
| **Tasks Tab** | `tasks_list.dart`, `tasks_detail.dart`, `tasks_tab.dart` | Needs testing |
| **Agents Tab** | `agents_tab.dart`, `agents_list.dart` | Needs testing |
| **API Client** | `api_client.dart` | Needs testing |
| **WebSocket Service** | `websocket_service.dart` | Needs testing |
| **State Providers** | `chat_provider.dart`, `session_notifier.dart`, `task_provider.dart`, `agent_provider.dart` | Needs testing |
| **UI Widgets** | `tab_bar.dart`, `cyberpunk_loader.dart`, `error_banner.dart`, `loading_spinner.dart` | Needs testing |

### Backend API Endpoints Required

The API client expects these endpoints at `http://localhost:8081/api/v1/`:
- `GET /health` - Health check
- `POST /chat` - Send chat message
- `GET /sessions` - List sessions
- `GET /sessions/:id` - Get session
- `POST /sessions` - Create session
- `DELETE /sessions/:id` - Delete session
- `GET /config/agents` - List agents
- `POST /config/agents/:id` - Update agent
- `GET /tasks` - List tasks
- `GET /tasks/:id` - Get task
- `POST /tasks` - Create task
- `POST /tasks/:id/cancel` - Cancel task
- `DELETE /tasks/:id` - Delete task
- `GET /queue/jobs` - List jobs
- `GET /queue/stats` - Queue stats
- `GET /metrics/live` - Live metrics
- `POST /memory/query` - Query memory
- `GET /memory/recent` - Recent memories
- `GET /skills` - List skills
- `GET /daemon/status` - Daemon status
- `WebSocket /api/v1/ws` - Real-time updates

---

## Task Overview

### Phase 1: Connection & Infrastructure
- Task 1: Verify daemon HTTP API is accessible
- Task 2: Test WebSocket connection
- Task 3: Fix API client error handling

### Phase 2: Chat Tab
- Task 4: Chat UI rendering test
- Task 5: Send message functionality
- Task 6: Display chat responses
- Task 7: Conversation persistence

### Phase 3: Sessions Tab
- Task 8: List sessions
- Task 9: Create new session
- Task 10: Delete session
- Task 11: Session detail view

### Phase 4: Tasks Tab
- Task 12: List tasks
- Task 13: Create task
- Task 14: Update task status
- Task 15: Cancel task

### Phase 5: Agents Tab
- Task 16: List agents
- Task 17: Agent configuration

### Phase 6: Real-time Features
- Task 18: WebSocket subscriptions
- Task 19: Live metrics display
- Task 20: Job queue updates

### Phase 7: UI Polish & Error Handling
- Task 21: Error banners
- Task 22: Loading states
- Task 23: Connection status indicator

---

## Detailed Tasks

### Task 1: Verify Daemon HTTP API

**Files:**
- Test: Manual curl commands
- Backend: `internal/comm/http/api_handlers.go`

- [ ] **Step 1: Test health endpoint**

```bash
curl -s http://localhost:8081/api/v1/health
# Expected: {"status":"ok"}
```

- [ ] **Step 2: Test daemon status endpoint**

```bash
curl -s http://localhost:8081/api/v1/daemon/status
# Expected: {"pid":N,"running":true,...}
```

- [ ] **Step 3: If endpoints fail, check daemon is running**

```bash
ps aux | grep meept-daemon
# If not running, start it
```

---

### Task 2: Test WebSocket Connection

**Files:**
- Frontend: `websocket_service.dart:48-105`
- Backend: `internal/comm/http/server.go`

- [ ] **Step 1: Verify WebSocket endpoint exists**

```bash
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: SGVsbG8sIHdvcmxkIQ==" \
  -H "Sec-WebSocket-Version: 13" \
  http://localhost:8081/api/v1/ws
# Expected: HTTP 101 Switching Protocols
```

- [ ] **Step 2: Test Flutter WebSocket connection**

Run the Flutter app and check logs for connection success/failure.

- [ ] **Step 3: Fix WebSocket path if needed**

If connection fails, verify the path matches backend (`/ws` vs `/api/v1/ws`).

---

### Task 3: Fix API Client Error Handling

**Files:**
- Frontend: `api_client.dart:91-97`

- [ ] **Step 1: Add better error messages**

```dart
ApiClientException _handleError(DioException e) {
  String message;
  switch (e.type) {
    case DioExceptionType.connectionTimeout:
      message = 'Connection timeout - is the daemon running?';
      break;
    case DioExceptionType.connectionError:
      message = 'Cannot connect to daemon at $_baseUrl';
      break;
    case DioExceptionType.badResponse:
      message = 'Server error: ${e.response?.statusCode}';
      break;
    default:
      message = e.message ?? 'Unknown error';
  }
  return ApiClientException(
    message: message,
    statusCode: e.response?.statusCode ?? 0,
    response: e.response?.data,
  );
}
```

- [ ] **Step 2: Add helpful logging**

```dart
print('API Error: $message at ${_dio.options.baseUrl}$path');
```

---

### Task 4: Chat UI Rendering Test

**Files:**
- Frontend: `chat_tab.dart`, `chat_view.dart`, `chat_message_bubble.dart`

- [ ] **Step 1: Run Flutter app and verify Chat tab renders**

```bash
cd ui/flutter_ui
flutter run -d macos  # or your target platform
```

- [ ] **Step 2: Verify chat input field is visible**

Check that the message input field appears at the bottom.

- [ ] **Step 3: Verify message list area is visible**

Check that the message display area appears.

---

### Task 5: Send Message Functionality

**Files:**
- Frontend: `chat_input.dart`, `chat_provider.dart`
- Backend: `internal/comm/http/api_handlers.go` (chat handler)

- [ ] **Step 1: Test send message API call**

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"test"}'
# Expected: Response with agent reply
```

- [ ] **Step 2: Fix chat_provider.dart if API call fails**

```dart
Future<void> sendMessage(String message) async {
  try {
    state = state.copyWith(isLoading: true, error: null);
    final response = await _apiClient.sendChatMessage(
      message: message,
      conversationId: state.currentConversationId,
    );
    // Handle response...
  } catch (e) {
    state = state.copyWith(
      error: 'Failed to send: $e',
      isLoading: false,
    );
  }
}
```

---

### Task 6: Display Chat Responses

**Files:**
- Frontend: `chat_message_list.dart`, `chat_message_bubble.dart`

- [ ] **Step 1: Verify response parsing**

Check that `ChatMessage.fromJson()` correctly parses the API response.

- [ ] **Step 2: Test message display**

Send a message and verify the response appears in the UI.

---

### Task 7: Conversation Persistence

**Files:**
- Frontend: `chat_provider.dart`
- Backend: Session persistence

- [ ] **Step 1: Verify conversation survives app restart**

Send messages, restart app, check if conversation is restored.

- [ ] **Step 2: Add session-based conversation loading**

```dart
Future<void> loadConversation(String sessionId) async {
  final session = await _apiClient.getSession(sessionId);
  state = state.copyWith(
    currentConversationId: session.id,
    messages: session.messages,
  );
}
```

---

### Task 8: List Sessions

**Files:**
- Frontend: `sessions_list.dart`, `session_notifier.dart`
- Backend: `internal/session/store.go`

- [ ] **Step 1: Test list sessions API**

```bash
curl http://localhost:8081/api/v1/sessions
# Expected: {"sessions":[...]}
```

- [ ] **Step 2: Verify sessions display in UI**

Check the Sessions tab shows the list.

---

### Task 9: Create New Session

**Files:**
- Frontend: `sessions_list.dart` or `sessions_overview_tab.dart`

- [ ] **Step 1: Add create session button**

```dart
IconButton(
  icon: const Icon(Icons.add),
  onPressed: () => _createNewSession(),
)
```

- [ ] **Step 2: Implement create session**

```dart
Future<void> _createNewSession() async {
  await ref.read(sessionProvider.notifier).createSession(
    title: 'New Session ${DateTime.now()}',
  );
}
```

- [ ] **Step 3: Test API endpoint**

```bash
curl -X POST http://localhost:8081/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"title":"Test Session"}'
```

---

### Task 10: Delete Session

**Files:**
- Frontend: `sessions_list.dart`

- [ ] **Step 1: Add delete button to session list item**

```dart
IconButton(
  icon: const Icon(Icons.delete),
  onPressed: () => _deleteSession(session.id),
)
```

- [ ] **Step 2: Implement delete**

```dart
Future<void> _deleteSession(String id) async {
  await ref.read(sessionProvider.notifier).deleteSession(id);
}
```

---

### Task 11: Session Detail View

**Files:**
- Frontend: `sessions_detail.dart`

- [ ] **Step 1: Create session detail screen**

Show session metadata, messages, and actions.

- [ ] **Step 2: Add navigation from list to detail**

```dart
onTap: () => Navigator.push(
  context,
  MaterialPageRoute(
    builder: (_) => SessionDetailScreen(sessionId: session.id),
  ),
);
```

---

### Task 12: List Tasks

**Files:**
- Frontend: `tasks_list.dart`, `tasks_tab.dart`, `task_provider.dart`
- Backend: `internal/task/store.go`

- [ ] **Step 1: Test list tasks API**

```bash
curl http://localhost:8081/api/v1/tasks
# Expected: {"tasks":[...]}
```

- [ ] **Step 2: Verify tasks display in UI**

Check the Tasks tab shows the list with status indicators.

---

### Task 13: Create Task

**Files:**
- Frontend: `tasks_tab.dart`

- [ ] **Step 1: Add create task button**

```dart
FloatingActionButton(
  onPressed: () => _showCreateTaskDialog(),
  child: const Icon(Icons.add),
)
```

- [ ] **Step 2: Implement create task dialog**

```dart
Future<void> _showCreateTaskDialog() async {
  final title = await showDialog<String>(...);
  if (title != null) {
    await ref.read(taskProvider.notifier).createTask(title: title);
  }
}
```

---

### Task 14: Update Task Status

**Files:**
- Frontend: `tasks_list.dart`

- [ ] **Step 1: Add status dropdown/selector**

```dart
DropdownButton<TaskStatus>(
  value: task.status,
  items: TaskStatus.values.map((s) => ...).toList(),
  onChanged: (newStatus) => _updateTaskStatus(task.id, newStatus),
)
```

---

### Task 15: Cancel Task

**Files:**
- Frontend: `tasks_list.dart`

- [ ] **Step 1: Add cancel button**

```dart
IconButton(
  icon: const Icon(Icons.cancel),
  onPressed: () => _cancelTask(task.id),
)
```

- [ ] **Step 2: Test cancel API**

```bash
curl -X POST http://localhost:8081/api/v1/tasks/:id/cancel
```

---

### Task 16: List Agents

**Files:**
- Frontend: `agents_tab.dart`, `agents_list.dart`, `agent_provider.dart`
- Backend: Agent discovery

- [ ] **Step 1: Test list agents API**

```bash
curl http://localhost:8081/api/v1/config/agents
# Expected: {"agents":[...]}
```

- [ ] **Step 2: Verify agents display in UI**

Check the Agents tab shows agent cards with status.

---

### Task 17: Agent Configuration

**Files:**
- Frontend: `agents_tab.dart`

- [ ] **Step 1: Add agent configuration UI**

Show agent settings that can be modified.

- [ ] **Step 2: Implement update agent API call**

```dart
Future<void> updateAgentConfig(String id, Map<String, dynamic> config) async {
  await _apiClient.updateAgent(id, config);
}
```

---

### Task 18: WebSocket Subscriptions

**Files:**
- Frontend: `websocket_service.dart:166-185`
- Backend: WebSocket hub

- [ ] **Step 1: Test WebSocket message reception**

Subscribe to a channel and verify messages arrive.

- [ ] **Step 2: Wire up chat message subscription**

```dart
@override
void initState() {
  super.initState();
  _subscription = _websocket.subscribeToChat(sessionId).listen((msg) {
    // Add message to state
  });
}
```

---

### Task 19: Live Metrics Display

**Files:**
- Frontend: `metrics_panel.dart`
- Backend: `internal/metrics/`

- [ ] **Step 1: Test metrics API**

```bash
curl http://localhost:8081/api/v1/metrics/live
```

- [ ] **Step 2: Display metrics in UI**

Show queue depth, active agents, etc.

---

### Task 20: Job Queue Updates

**Files:**
- Frontend: `tasks_tab.dart` or dedicated jobs panel

- [ ] **Step 1: Subscribe to job updates via WebSocket**

```dart
_websocket.subscribeToJobs().listen((msg) {
  // Update job state
});
```

---

### Task 21: Error Banners

**Files:**
- Frontend: `error_banner.dart`

- [ ] **Step 1: Show error banner on API failures**

```dart
if (state.error != null)
  ErrorBanner(message: state.error!),
```

---

### Task 22: Loading States

**Files:**
- Frontend: `loading_spinner.dart`, `cyberpunk_loader.dart`

- [ ] **Step 1: Show loading indicator during API calls**

```dart
if (state.isLoading)
  const LoadingSpinner(),
```

---

### Task 23: Connection Status Indicator

**Files:**
- Frontend: New widget or add to tab bar

- [ ] **Step 1: Add connection status indicator**

```dart
Container(
  color: connected ? Colors.green : Colors.red,
  child: Text(connected ? 'Connected' : 'Disconnected'),
)
```

- [ ] **Step 2: Wire up to connection monitor**

```dart
final connected = ref.watch(connectionStateProvider);
```

---

## Testing Checklist

After all tasks are complete, verify:

- [ ] App launches without errors
- [ ] All 4 tabs render correctly
- [ ] Chat: Can send and receive messages
- [ ] Sessions: Can list, create, delete sessions
- [ ] Tasks: Can list, create, cancel tasks
- [ ] Agents: Can list agents and see status
- [ ] Real-time updates work via WebSocket
- [ ] Error handling shows helpful messages
- [ ] Connection status is visible
- [ ] No console errors or warnings

---

**End of Plan**
