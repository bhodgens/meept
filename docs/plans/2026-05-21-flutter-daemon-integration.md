# Meept Flutter Daemon Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Wire the Flutter UI to the Meept daemon for real-time chat, sessions, tasks, and agents management.

**Architecture:** The Flutter app uses Riverpod for state management, connecting to the daemon's HTTP API at `http://localhost:8081/api/v1`. The API client (`api_client.dart`) already has all required methods - this plan connects them to the UI via Riverpod providers and WebSocket listeners for real-time updates.

**Tech Stack:** Flutter 3.x, Riverpod 2.x, Dio 5.x, web_socket_channel, Meept HTTP API

---

## File Structure

**Files to create:**
- `ui/flutter_ui/lib/providers/providers.dart` - Riverpod providers for all data
- `ui/flutter_ui/lib/providers/chat_provider.dart` - Chat state and actions
- `ui/flutter_ui/lib/providers/session_provider.dart` - Session management
- `ui/flutter_ui/lib/providers/task_provider.dart` - Task management  
- `ui/flutter_ui/lib/providers/agent_provider.dart` - Agent management
- `ui/flutter_ui/lib/services/websocket_service.dart` - Real-time event listener

**Files to modify:**
- `ui/flutter_ui/lib/features/chat/chat_input.dart` - Wire up send message
- `ui/flutter_ui/lib/features/chat/chat_message_list.dart` - Connect to live messages
- `ui/flutter_ui/lib/features/sessions/sessions_list.dart` - Live session list
- `ui/flutter_ui/lib/features/tasks/tasks_list.dart` - Live task list
- `ui/flutter_ui/lib/features/agents/agents_tab.dart` - Live agent list
- `ui/flutter_ui/lib/features/home/home_screen.dart` - Initialize providers

---

## Task 1: WebSocket Service for Real-Time Updates

**Files:**
- Create: `ui/flutter_ui/lib/services/websocket_service.dart`

- [ ] **Step 1: Create WebSocket service skeleton**

```dart
import 'dart:async';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// WebSocketService provides real-time updates from the daemon.
class WebSocketService {
  final String wsUrl;
  WebSocketChannel? _channel;
  final _messageController = StreamController<String>.broadcast();
  
  WebSocketService({required this.wsUrl});
  
  Stream<String> get messages => _messageController.stream;
  
  bool get isConnected => _channel != null;
  
  void connect() {
    if (_channel != null) return;
    
    _channel = WebSocketChannel.connect(Uri.parse(wsUrl));
    _channel!.stream.listen(
      (data) => _messageController.add(data),
      onError: (error) {
        print('WebSocket error: $error');
        _channel = null;
      },
      onDone: () {
        print('WebSocket closed');
        _channel = null;
      },
    );
  }
  
  void disconnect() {
    _channel?.sink.close();
    _channel = null;
  }
  
  void send(String message) {
    _channel?.sink.add(message);
  }
}

final websocketServiceProvider = Provider<WebSocketService>((ref) {
  return WebSocketService(wsUrl: 'ws://localhost:8081/ws');
});
```

- [ ] **Step 2: Commit WebSocket service**

```bash
cd ui/flutter_ui
git add lib/services/websocket_service.dart
git commit -m "feat(flutter): add WebSocket service for real-time daemon updates"
```

---

## Task 2: Riverpod Providers for Chat

**Files:**
- Create: `ui/flutter_ui/lib/providers/chat_provider.dart`
- Modify: `ui/flutter_ui/lib/features/chat/chat_input.dart`
- Modify: `ui/flutter_ui/lib/features/chat/chat_message_list.dart`

- [ ] **Step 1: Create chat provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';

/// ChatState holds the current chat messages and loading state.
class ChatState {
  final List<ChatMessage> messages;
  final bool isLoading;
  final String? error;
  
  ChatState({
    this.messages = const [],
    this.isLoading = false,
    this.error,
  });
  
  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? isLoading,
    String? error,
  }) {
    return ChatState(
      messages: messages ?? this.messages,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

/// Notifier for chat operations.
class ChatNotifier extends StateNotifier<ChatState> {
  final ApiClient _client;
  
  ChatNotifier(this._client) : super(ChatState());
  
  Future<void> sendMessage({
    required String message,
    String? sessionId,
    String? agentId,
  }) async {
    state = state.copyWith(isLoading: true, error: null);
    
    try {
      final response = await _client.sendChatMessage(
        message: message,
        sessionId: sessionId,
        agentId: agentId,
      );
      
      // Add user message to local state
      final userMsg = ChatMessage(
        id: DateTime.now().millisecondsSinceEpoch.toString(),
        role: 'user',
        content: message,
        timestamp: DateTime.now(),
      );
      state = state.copyWith(
        messages: [...state.messages, userMsg],
        isLoading: false,
      );
      
      // TODO: Listen for streaming response
    } catch (e) {
      state = state.copyWith(
        error: e.toString(),
        isLoading: false,
      );
    }
  }
  
  void addMessage(ChatMessage message) {
    state = state.copyWith(
      messages: [...state.messages, message],
    );
  }
  
  void clear() {
    state = ChatState();
  }
  
  Future<void> loadMessages(String sessionId) async {
    // TODO: Add getMessages endpoint to API
    state = state.copyWith(messages: []);
  }
}

final chatProvider = StateNotifierProvider<ChatNotifier, ChatState>((ref) {
  final client = ref.watch(apiClientProvider);
  return ChatNotifier(client);
});

/// Provider for the current session ID.
final currentSessionIdProvider = StateProvider<String?>((ref) => null);
```

- [ ] **Step 2: Wire up chat input send button**

Modify `ui/flutter_ui/lib/features/chat/chat_input.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/chat_provider.dart';

class ChatInput extends ConsumerStatefulWidget {
  final String sessionId;
  
  const ChatInput({super.key, required this.sessionId});
  
  @override
  State<ChatInput> createState() => _ChatInputState();
}

class _ChatInputState extends ConsumerState<ChatInput> {
  final _controller = TextEditingController();
  
  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }
  
  void _handleSend() {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    
    final chatNotifier = ref.read(chatProvider.notifier);
    chatNotifier.sendMessage(
      message: text,
      sessionId: widget.sessionId,
      agentId: 'coder', // TODO: Make selectable
    );
    
    _controller.clear();
  }
  
  @override
  Widget build(BuildContext context) {
    final chatState = ref.watch(chatProvider);
    
    return Container(
      height: 80,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          top: BorderSide(color: CyberpunkColors.orangePrimary, width: 1),
        ),
      ),
      child: Row(
        children: [
          _buildAgentSelector(),
          const SizedBox(width: 8),
          Expanded(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              decoration: BoxDecoration(
                color: CyberpunkColors.black,
                border: Border.all(color: CyberpunkColors.midGray, width: 1),
                borderRadius: BorderRadius.circular(4),
              ),
              child: TextField(
                controller: _controller,
                enabled: !chatState.isLoading,
                style: CyberpunkTypography.bodyMedium.copyWith(
                  color: CyberpunkColors.greenSuccess,
                  fontFamily: 'SourceCodePro',
                ),
                cursorColor: CyberpunkColors.orangePrimary,
                decoration: InputDecoration(
                  hintText: chatState.isLoading ? 'sending...' : 'enter command...',
                  hintStyle: CyberpunkTypography.bodySmall,
                  border: InputBorder.none,
                  contentPadding: EdgeInsets.zero,
                ),
                maxLines: 3,
                minLines: 1,
                textCapitalization: TextCapitalization.sentences,
                onSubmitted: (value) => _handleSend(),
              ),
            ),
          ),
          const SizedBox(width: 8),
          _buildSendButton(),
        ],
      ),
    );
  }
  
  // ... rest of widget methods unchanged
}
```

- [ ] **Step 3: Wire up chat message list**

Modify `ui/flutter_ui/lib/features/chat/chat_message_list.dart` to use `ref.watch(chatProvider)` for messages.

- [ ] **Step 4: Commit chat integration**

```bash
cd ui/flutter_ui
git add lib/providers/chat_provider.dart lib/features/chat/chat_input.dart lib/features/chat/chat_message_list.dart
git commit -m "feat(flutter): wire up chat message sending via Riverpod"
```

---

## Task 3: Session Management Providers

**Files:**
- Create: `ui/flutter_ui/lib/providers/session_provider.dart`
- Modify: `ui/flutter_ui/lib/features/sessions/sessions_list.dart`

- [ ] **Step 1: Create session provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';

/// SessionState holds sessions list and loading state.
class SessionState {
  final List<Session> sessions;
  final bool isLoading;
  final String? error;
  
  SessionState({
    this.sessions = const [],
    this.isLoading = false,
    this.error,
  });
  
  SessionState copyWith({
    List<Session>? sessions,
    bool? isLoading,
    String? error,
  }) {
    return SessionState(
      sessions: sessions ?? this.sessions,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

class SessionNotifier extends StateNotifier<SessionState> {
  final ApiClient _client;
  
  SessionNotifier(this._client) : super(SessionState());
  
  Future<void> loadSessions() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final sessions = await _client.listSessions();
      state = state.copyWith(sessions: sessions, isLoading: false);
    } catch (e) {
      state = state.copyWith(error: e.toString(), isLoading: false);
    }
  }
  
  Future<void> createSession(String title) async {
    try {
      await _client.createSession(title: title);
      await loadSessions(); // Refresh list
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }
  
  Future<void> deleteSession(String id) async {
    try {
      await _client.deleteSession(id);
      await loadSessions(); // Refresh list
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }
  
  Future<void> selectSession(String id) async {
    // Handled by selectedSessionProvider
  }
}

final sessionProvider = StateNotifierProvider<SessionNotifier, SessionState>((ref) {
  final client = ref.watch(apiClientProvider);
  return SessionNotifier(client);
});

final selectedSessionProvider = StateProvider<Session?>((ref) => null);
```

- [ ] **Step 2: Wire up sessions list UI**

Modify `sessions_list.dart` to use `ref.watch(sessionProvider)` and call `notifier.loadSessions()` on mount.

- [ ] **Step 3: Commit session integration**

```bash
cd ui/flutter_ui
git add lib/providers/session_provider.dart lib/features/sessions/sessions_list.dart
git commit -m "feat(flutter): wire up session management via Riverpod"
```

---

## Task 4: Task Management Providers

**Files:**
- Create: `ui/flutter_ui/lib/providers/task_provider.dart`
- Modify: `ui/flutter_ui/lib/features/tasks/tasks_list.dart`

- [ ] **Step 1: Create task provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';

class TaskState {
  final List<Task> tasks;
  final bool isLoading;
  final String? error;
  
  TaskState({this.tasks = const [], this.isLoading = false, this.error});
  
  TaskState copyWith({List<Task>? tasks, bool? isLoading, String? error}) {
    return TaskState(
      tasks: tasks ?? this.tasks,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

class TaskNotifier extends StateNotifier<TaskState> {
  final ApiClient _client;
  
  TaskNotifier(this._client) : super(TaskState());
  
  Future<void> loadTasks({String? sessionId}) async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final tasks = await _client.listTasks(sessionId: sessionId);
      state = state.copyWith(tasks: tasks, isLoading: false);
    } catch (e) {
      state = state.copyWith(error: e.toString(), isLoading: false);
    }
  }
}

final taskProvider = StateNotifierProvider<TaskNotifier, TaskState>((ref) {
  return TaskNotifier(ref.watch(apiClientProvider));
});
```

- [ ] **Step 2: Wire up tasks list UI**

- [ ] **Step 3: Commit task integration**

```bash
cd ui/flutter_ui
git add lib/providers/task_provider.dart lib/features/tasks/tasks_list.dart
git commit -m "feat(flutter): wire up task management via Riverpod"
```

---

## Task 5: Agent Management Providers

**Files:**
- Create: `ui/flutter_ui/lib/providers/agent_provider.dart`
- Modify: `ui/flutter_ui/lib/features/agents/agents_tab.dart`

- [ ] **Step 1: Create agent provider**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';

class AgentState {
  final List<Agent> agents;
  final bool isLoading;
  final String? error;
  
  AgentState({this.agents = const [], this.isLoading = false, this.error});
  
  AgentState copyWith({List<Agent>? agents, bool? isLoading, String? error}) {
    return AgentState(
      agents: agents ?? this.agents,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

class AgentNotifier extends StateNotifier<AgentState> {
  final ApiClient _client;
  
  AgentNotifier(this._client) : super(AgentState());
  
  Future<void> loadAgents() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final agents = await _client.listAgents();
      state = state.copyWith(agents: agents, isLoading: false);
    } catch (e) {
      state = state.copyWith(error: e.toString(), isLoading: false);
    }
  }
}

final agentProvider = StateNotifierProvider<AgentNotifier, AgentState>((ref) {
  return AgentNotifier(ref.watch(apiClientProvider));
});
```

- [ ] **Step 2: Wire up agents list UI**

- [ ] **Step 3: Commit agent integration**

```bash
cd ui/flutter_ui
git add lib/providers/agent_provider.dart lib/features/agents/agents_tab.dart
git commit -m "feat(flutter): wire up agent management via Riverpod"
```

---

## Task 6: Initialize Providers in Home Screen

**Files:**
- Modify: `ui/flutter_ui/lib/features/home/home_screen.dart`

- [ ] **Step 1: Initialize providers on app start**

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../providers/session_provider.dart';
import '../../providers/task_provider.dart';
import '../../providers/agent_provider.dart';

class _HomeScreenState extends ConsumerState<HomeScreen> {
  @override
  void initState() {
    super.initState();
    // Load initial data
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(sessionProvider.notifier).loadSessions();
      ref.read(taskProvider.notifier).loadTasks();
      ref.read(agentProvider.notifier).loadAgents();
    });
  }
  
  // ... rest of implementation
}
```

- [ ] **Step 2: Commit home screen initialization**

```bash
cd ui/flutter_ui
git add lib/features/home/home_screen.dart
git commit -m "feat(flutter): initialize data providers on app start"
```

---

## Task 7: Error Handling and Loading States

**Files:**
- Create: `ui/flutter_ui/lib/widgets/error_banner.dart`
- Create: `ui/flutter_ui/lib/widgets/loading_spinner.dart`

- [ ] **Step 1: Create error banner widget**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class ErrorBanner extends StatelessWidget {
  final String message;
  final VoidCallback? onDismiss;
  
  const ErrorBanner({super.key, required this.message, this.onDismiss});
  
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.redAlert.withOpacity(0.2),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
          ),
          if (onDismiss != null)
            IconButton(
              icon: const Icon(Icons.close, size: 16),
              onPressed: onDismiss,
            ),
        ],
      ),
    );
  }
}
```

- [ ] **Step 2: Create loading spinner widget**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';

class LoadingSpinner extends StatelessWidget {
  final double size;
  
  const LoadingSpinner({super.key, this.size = 24.0});
  
  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size,
      child: CircularProgressIndicator(
        strokeWidth: 2,
        valueColor: AlwaysStoppedAnimation<Color>(
          CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }
}
```

- [ ] **Step 3: Commit error/loading widgets**

```bash
cd ui/flutter_ui
git add lib/widgets/error_banner.dart lib/widgets/loading_spinner.dart
git commit -m "feat(flutter): add error banner and loading spinner widgets"
```

---

## Summary

**Total Tasks:** 7
**Estimated Time:** 2-3 hours with hot reload

**Files to Create:** 8
- `lib/services/websocket_service.dart`
- `lib/providers/chat_provider.dart`
- `lib/providers/session_provider.dart`
- `lib/providers/task_provider.dart`
- `lib/providers/agent_provider.dart`
- `lib/widgets/error_banner.dart`
- `lib/widgets/loading_spinner.dart`

**Files to Modify:** 6
- `lib/features/chat/chat_input.dart`
- `lib/features/chat/chat_message_list.dart`
- `lib/features/sessions/sessions_list.dart`
- `lib/features/tasks/tasks_list.dart`
- `lib/features/agents/agents_tab.dart`
- `lib/features/home/home_screen.dart`

---

**Next Steps:** Run `make gui-web-run` to test the integrated UI against the running daemon.
