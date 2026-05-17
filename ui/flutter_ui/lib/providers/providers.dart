import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import '../models/api_models.dart';

// API Client provider
final apiClientProvider = Provider<ApiClient>((ref) => ApiClient());

// WebSocket Service provider
final websocketProvider = Provider<WebSocketService>((ref) => WebSocketService());

// Sessions provider
final sessionsProvider = FutureProvider<List<Session>>((ref) async {
  final client = ref.watch(apiClientProvider);
  return client.listSessions();
});

// Agents provider
final agentsProvider = FutureProvider<List<Agent>>((ref) async {
  final client = ref.watch(apiClientProvider);
  return client.listAgents();
});

// Tasks provider
final tasksProvider = FutureProvider<List<Task>>((ref) async {
  final client = ref.watch(apiClientProvider);
  return client.listTasks();
});

// Active session state
final activeSessionProvider = StateProvider<Session?>((ref) => null);

// Active agent state
final activeAgentProvider = StateProvider<Agent?>((ref) => null);

// Connection state
final connectionStateProvider = StateProvider<bool>((ref) => false);
