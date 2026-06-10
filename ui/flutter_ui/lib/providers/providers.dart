export 'chat_provider.dart';
export 'task_provider.dart';
export 'agent_provider.dart';
export 'metrics_provider.dart';
export 'job_provider.dart';
export 'plan_provider.dart';
export 'stt_provider.dart';

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
  return WebSocketService.fromStorage(storage);
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

// Connection state
final connectionStateProvider = StateProvider<bool>((ref) => false);

// Active tool panel route (empty string = no tool active)
final activeToolProvider = StateProvider<String>((ref) => '');

// Drawer overlay visibility (true = open)
final drawerOpenProvider = StateProvider<bool>((ref) => false);

// Keyboard shortcut help dialog visibility
final shortcutHelpProvider = StateProvider<bool>((ref) => false);

// Focus input with slash prefix request (toggled true to trigger, ChatInput resets)
final focusInputRequestProvider = StateProvider<bool>((ref) => false);

/// Monitors WebSocket + HTTP health and updates connectionStateProvider.
///
/// Connection state changes are debounced: the provider is only updated
/// after two consecutive readings agree (within a short window).  This
/// prevents the UI indicator from flickering during reconnect cycles when
/// the WebSocket emits rapid connected/disconnected events.
class ConnectionMonitor {
  final WebSocketService _websocket;
  final ApiClient _apiClient;
  final ProviderContainer _container;
  Timer? _timer;
  Timer? _debounceTimer;
  StreamSubscription<bool>? _connectionSub;

  /// The last raw value received from WebSocket or health check.
  bool? _pendingState;

  /// Whether [_pendingState] has been confirmed by a second consecutive
  /// reading of the same value.
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
      _proposeState(connected);
    });
  }

  /// Propose a new connection state.  The provider is updated immediately
  /// when the value matches the previous confirmed state (e.g. still
  /// disconnected).  When the value differs, a second consecutive reading
  /// of the same value is required before the provider is updated.
  void _proposeState(bool connected) {
    _debounceTimer?.cancel();

    final prevState = _pendingState;

    if (prevState == null) {
      // First reading -- stash and wait for confirmation.
      _pendingState = connected;
      _confirmed = false;
      _debounceTimer = Timer(const Duration(seconds: 1), () {
        // Timeout without a second reading: accept the lone value.
        _applyState(connected);
      });
      return;
    }

    if (prevState == connected && _confirmed) {
      // Same value already confirmed -- no-op.
      return;
    }

    if (prevState == connected && !_confirmed) {
      // Second consecutive reading of the same new value -- confirm.
      _confirmed = true;
      _applyState(connected);
      return;
    }

    // Value changed -- reset and wait for confirmation of the new value.
    _pendingState = connected;
    _confirmed = false;
    _debounceTimer = Timer(const Duration(seconds: 1), () {
      // Timeout without confirmation: accept the new value.
      _applyState(connected);
    });
  }

  void _applyState(bool connected) {
    _pendingState = connected;
    _confirmed = true;
    _container.read(connectionStateProvider.notifier).state = connected;
  }

  void _startHealthChecks() {
    _timer = Timer.periodic(const Duration(seconds: 30), (_) async {
      // If WebSocket is not connected, try an HTTP health check to verify
      // the daemon is still reachable
      if (!_websocket.isConnected) {
        try {
          await _apiClient.get<Map<String, dynamic>>('/daemon/status');
          _proposeState(true);
        } catch (_) {
          _proposeState(false);
        }
      }
    });
  }

  void dispose() {
    _connectionSub?.cancel();
    _connectionSub = null;
    _timer?.cancel();
    _timer = null;
    _debounceTimer?.cancel();
    _debounceTimer = null;
  }
}

/// Single connection monitor instance, created once at app startup
final connectionMonitorProvider = Provider<ConnectionMonitor>((ref) {
  final websocket = ref.watch(websocketProvider);
  final client = ref.watch(apiClientProvider);
  final monitor = ConnectionMonitor(
    websocket: websocket,
    apiClient: client,
    container: ref.container,
  );
  ref.onDispose(() {
    monitor.dispose();
  });
  return monitor;
});
