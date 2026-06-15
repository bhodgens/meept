export 'chat_provider.dart';
export 'task_provider.dart';
export 'agent_provider.dart';
export 'metrics_provider.dart';
export 'job_provider.dart';
export 'plan_provider.dart';
export 'stt_provider.dart';
export 'tts_provider.dart';

import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import '../services/storage_service.dart';
import '../services/session_notifier.dart';
import '../models/api_models.dart';

// Storage service — initialized in main() before runApp
final storageProvider = Provider<StorageService>((ref) => StorageService.instance);

// API Client provider — loads persisted host/port and API key from storage
final apiClientProvider = Provider<ApiClient>((ref) {
  final storage = ref.watch(storageProvider);
  final client = ApiClient.storage(storage: storage);
  ref.onDispose(() => client.dispose());
  return client;
});

// WebSocket Service provider — reads persisted settings from storage
final websocketProvider = Provider<WebSocketService>((ref) {
  final storage = ref.watch(storageProvider);
  final ws = WebSocketService.fromStorage(storage);
  ref.onDispose(() => ws.disconnect());
  return ws;
});

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

// Active project state.
final activeProjectProvider = StateProvider<Project?>((ref) => null);

// Resolves the active project
final resolveActiveProjectProvider = FutureProvider<Project?>((ref) async {
  final explicit = ref.watch(activeProjectProvider);
  if (explicit != null) return explicit;
  final client = ref.watch(apiClientProvider);
  try {
    final projects = await client.listProjects();
    final active = projects.where((p) => p.status == 'active').toList();
    return active.isEmpty ? null : active.first;
  } catch (_) {
    return null;
  }
});

// Connection state - using simple boolean for compatibility
final connectionStateProvider = StateProvider<bool>((ref) => false);

// Connection status text - derived from connection state with "connecting..." support
final connectionStatusProvider = StateProvider<String>((ref) {
  final connected = ref.watch(connectionStateProvider);
  final isConnecting = ref.watch(isConnectingProvider);
  if (isConnecting) return 'connecting...';
  return connected ? 'connected' : 'disconnected';
});

// Connection status color provider
final connectionColorProvider = StateProvider<String>((ref) {
  final connected = ref.watch(connectionStateProvider);
  final isConnecting = ref.watch(isConnectingProvider);
  if (isConnecting) return 'orange';
  return connected ? 'green' : 'red';
});

// Whether the app is currently attempting to connect to the daemon
final isConnectingProvider = StateProvider<bool>((ref) => false);

// Active tool panel route
final activeToolProvider = StateProvider<String>((ref) => '');

// Drawer overlay visibility
final drawerOpenProvider = StateProvider<bool>((ref) => false);

// Keyboard shortcut help dialog visibility
final shortcutHelpProvider = StateProvider<bool>((ref) => false);

// Focus input with slash prefix request
final focusInputRequestProvider = StateProvider<bool>((ref) => false);

// ConnectionMonitor provider - manages WebSocket health monitoring
final connectionMonitorProvider = Provider<ConnectionMonitor>((ref) {
  final websocket = ref.watch(websocketProvider);
  final apiClient = ref.watch(apiClientProvider);
  final container = ref.container;

  final monitor = ConnectionMonitor(
    websocket: websocket,
    apiClient: apiClient,
    container: container,
  );

  ref.onDispose(() => monitor.dispose());
  return monitor;
});

/// Monitors WebSocket + HTTP health and updates connectionStateProvider.
class ConnectionMonitor {
  final WebSocketService _websocket;
  final ApiClient _apiClient;
  final ProviderContainer _container;
  Timer? _timer;
  Timer? _debounceTimer;
  StreamSubscription<bool>? _connectionSub;

  bool? _pendingState;
  bool _confirmed = false;

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
    _connectionSub = _websocket.connectionStream.listen((connected) {
      // When WebSocket disconnects after being connected, show "connecting..." briefly
      if (_pendingState == true && !connected) {
        _container.read(isConnectingProvider.notifier).state = true;
      }
      _proposeState(connected);
    });
  }

  void _proposeState(bool connected) {
    _debounceTimer?.cancel();
    final prevState = _pendingState;

    if (prevState == null) {
      _pendingState = connected;
      _confirmed = false;
      _debounceTimer = Timer(const Duration(seconds: 1), () {
        _applyState(connected);
      });
      return;
    }

    if (prevState == connected && _confirmed) {
      return;
    }

    if (prevState == connected && !_confirmed) {
      _confirmed = true;
      _applyState(connected);
      return;
    }

    _pendingState = connected;
    _confirmed = false;
    _debounceTimer = Timer(const Duration(seconds: 1), () {
      _applyState(connected);
    });
  }

  void _applyState(bool connected) {
    _pendingState = connected;
    _confirmed = true;
    _container.read(connectionStateProvider.notifier).state = connected;
    // Clear connecting state once we have a definitive state
    _container.read(isConnectingProvider.notifier).state = false;
  }

  void _startHealthChecks() {
    _timer = Timer.periodic(const Duration(seconds: 30), (_) async {
      if (!_websocket.isConnected) {
        try {
          await _apiClient.getDaemonStatus();
          _proposeState(true);
        } catch (_) {
          _proposeState(false);
        }
      }
    });
  }

  void dispose() {
    _timer?.cancel();
    _debounceTimer?.cancel();
    _connectionSub?.cancel();
  }
}
