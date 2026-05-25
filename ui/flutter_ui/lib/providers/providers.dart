export 'chat_provider.dart';
export 'task_provider.dart';
export 'agent_provider.dart';
export 'metrics_provider.dart';
export 'job_provider.dart';

import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import '../services/session_notifier.dart';
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

/// Monitors WebSocket + HTTP health and updates connectionStateProvider
class ConnectionMonitor {
  final WebSocketService _websocket;
  final ApiClient _apiClient;
  final ProviderContainer _container;
  Timer? _timer;

  ConnectionMonitor({
    required WebSocketService websocket,
    required ApiClient apiClient,
    required ProviderContainer container,
  })  : _websocket = websocket,
        _apiClient = apiClient,
        _container = container {
    _listenToWebSocket();
    _startHealthChecks();
  }

  void _listenToWebSocket() {
    // Wire WebSocket connection events to the connection state provider
    _websocket.connectionStream.listen((connected) {
      _container.read(connectionStateProvider.notifier).state = connected;
    });
  }

  void _startHealthChecks() {
    _timer = Timer.periodic(const Duration(seconds: 30), (_) async {
      // If WebSocket is not connected, try an HTTP health check to verify
      // the daemon is still reachable
      if (!_websocket.isConnected) {
        try {
          await _apiClient.get<Map<String, dynamic>>('/daemon/status');
          _container.read(connectionStateProvider.notifier).state = true;
        } catch (_) {
          _container.read(connectionStateProvider.notifier).state = false;
        }
      }
    });
  }

  void dispose() {
    _timer?.cancel();
    _timer = null;
  }
}

/// Single connection monitor instance, created once at app startup
final connectionMonitorProvider = Provider<ConnectionMonitor>((ref) {
  final websocket = ref.watch(websocketProvider);
  final client = ref.watch(apiClientProvider);
  return ConnectionMonitor(
    websocket: websocket,
    apiClient: client,
    container: ref.container,
  );
});
