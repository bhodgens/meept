export 'task_provider.dart';
export 'agent_provider.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/api_client.dart';
import '../services/session_notifier.dart';
import '../services/websocket_service.dart';
import '../models/api_models.dart';

// API Client provider
final apiClientProvider = Provider<ApiClient>((ref) => ApiClient());

// WebSocket Service provider
final websocketProvider = Provider<WebSocketService>((ref) => WebSocketService());

// Session state provider (StateNotifier for CRUD + selection)
final sessionProvider =
    StateNotifierProvider<SessionNotifier, SessionState>((ref) {
  final client = ref.watch(apiClientProvider);
  return SessionNotifier(apiClient: client);
});


// Active session state
final activeSessionProvider = StateProvider<Session?>((ref) => null);

// Active agent state
final activeAgentProvider = StateProvider<Agent?>((ref) => null);

// Connection state
final connectionStateProvider = StateProvider<bool>((ref) => false);
