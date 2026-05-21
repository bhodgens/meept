import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import '../models/api_models.dart';
import 'chat_provider.dart';

// API Client provider
final apiClientProvider = Provider<ApiClient>((ref) => ApiClient());

// WebSocket Service provider
final websocketProvider = Provider<WebSocketService>((ref) => WebSocketService());

// Chat provider (from chat_provider.dart)
// Exported via chat_provider.dart - chatProvider, currentSessionIdProvider

// Sessions provider with state management
class SessionState {
  final List<Session> sessions;
  final bool isLoading;
  final String? error;

  SessionState({this.sessions = const [], this.isLoading = false, this.error});

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
      await loadSessions();
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> deleteSession(String id) async {
    try {
      await _client.deleteSession(id);
      await loadSessions();
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }
}

final sessionProvider = StateNotifierProvider<SessionNotifier, SessionState>((ref) {
  return SessionNotifier(ref.watch(apiClientProvider));
});

// Agents provider with state management
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

// Tasks provider with state management
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

// Active session state
final activeSessionProvider = StateProvider<Session?>((ref) => null);

// Active agent state
final activeAgentProvider = StateProvider<Agent?>((ref) => null);

// Connection state
final connectionStateProvider = StateProvider<bool>((ref) => false);
