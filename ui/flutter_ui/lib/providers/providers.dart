export 'chat_provider.dart';
export 'task_provider.dart';
export 'agent_provider.dart';
export 'metrics_provider.dart';
export 'job_provider.dart';
export 'plan_provider.dart';
export 'stt_provider.dart';
export 'tts_provider.dart';

import 'dart:async';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/sdk_client.dart';
import '../services/websocket_service.dart';
import '../services/storage_service.dart';
import '../services/session_notifier.dart';
import '../services/daemon_cert_pinner.dart';
import '../models/api_models.dart';

// Storage service — initialized in main() before runApp
final storageProvider = Provider<StorageService>((ref) => StorageService.instance);

// SDK Client provider — wraps the OpenAPI-generated Dart SDK.
//
// Loads persisted host/port/API key from storage. All feature panels and
// provider classes route through this provider.
final sdkClientProvider = Provider<SdkApiClient>((ref) {
  final storage = ref.watch(storageProvider);
  final client = SdkApiClient.storage(storage: storage);
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
  final client = ref.watch(sdkClientProvider);
  return SessionNotifier(sdkClient: client);
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
  final client = ref.watch(sdkClientProvider);
  try {
    final rawProjects = await client.listProjects();
    final projects = rawProjects.map(Project.fromJson).toList();
    final active = projects.where((p) => p.status == 'active').toList();
    return active.isEmpty ? null : active.first;
  } catch (e) {
    debugPrint('[warn] resolveActiveProjectProvider: $e');
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

// Keyboard shortcut help dialog visibility
final shortcutHelpProvider = StateProvider<bool>((ref) => false);

// Focus input with slash prefix request
final focusInputRequestProvider = StateProvider<bool>((ref) => false);

/// Connection detail row data.
class ConnDataRow {
  final String label;
  final String value;
  const ConnDataRow(this.label, this.value);
}

/// Connection details — fetched from the API on successful connect.
class ConnectionDetails {
  final String? version;
  final int? pid;
  final String? uptime;
  final String state;
  final String host;
  final int port;
  final bool tls;
  final DateTime? connectedAt;
  final String? certFingerprint;

  const ConnectionDetails({
    this.version,
    this.pid,
    this.uptime,
    this.state = 'unknown',
    required this.host,
    required this.port,
    required this.tls,
    this.connectedAt,
    this.certFingerprint,
  });

  /// Duration since connection was established.
  String get duration {
    final since = connectedAt;
    if (since == null) return '—';
    final diff = DateTime.now().difference(since);
    final hh = diff.inHours;
    final mm = diff.inMinutes % 60;
    final ss = diff.inSeconds % 60;
    if (hh > 0) return '$hh h $mm m';
    if (mm > 0) return '$mm m $ss s';
    return '$ss s';
  }

  /// Build a short summary line for the popup menu.
  String get summary {
    final parts = <String>[];
    if (state != 'unknown') parts.add(state);
    if (pid != null) parts.add('pid $pid');
    return parts.join(' • ');
  }

  /// Build the rows for the connection details dialog.
  List<ConnDataRow> get dialogRows => <ConnDataRow>[
        const ConnDataRow('host', ''),
        const ConnDataRow('port', ''),
        ConnDataRow('tls', tls ? 'yes (self-signed)' : 'no'),
        if (tls)
          ConnDataRow('certificate', certFingerprint ?? 'pinned (no file)'),
        ConnDataRow('connection', connectedAt != null ? 'alive' : '—'),
        if (connectedAt != null) ConnDataRow('duration', duration),
        if (pid != null) ConnDataRow('pid', pid.toString()),
        if (state != 'unknown') ConnDataRow('state', state),
        if (uptime != null) ConnDataRow('uptime', uptime!),
        if (version != null) ConnDataRow('version', version!),
      ];

  /// Get the row value for a given label.
  String rowValue(String label) {
    return switch (label) {
      'host' => host,
      'port' => port.toString(),
      _ => '',
    };
  }
}

final connectionDetailsProvider = StateNotifierProvider<ConnectionDetailsNotifier, ConnectionDetails?>((ref) {
  return ConnectionDetailsNotifier(ref);
});

/// Provides daemon info (version, pid, uptime, state) and connection metadata.
/// Updated on each successful connection.
class ConnectionDetailsNotifier extends StateNotifier<ConnectionDetails?> {
  final Ref _ref;
  Timer? _fetchTimer;
  bool _isFetching = false;

  ConnectionDetailsNotifier(this._ref) : super(null) {
    // Periodically refresh daemon state while connected
    _fetchTimer = Timer.periodic(const Duration(seconds: 60), (_) async {
      final connected = _ref.read(connectionStateProvider);
      if (connected) await _fetch();
    });
  }

  Future<void> _fetch() async {
    // Prevent concurrent fetches (e.g., on connect + periodic timer race)
    if (_isFetching) {
      debugPrint('[warn] ConnectionDetailsNotifier._fetch() skipped - already fetching');
      return;
    }
    _isFetching = true;
    try {
      ConnectionDetails result;

      final host = _ref.read(websocketProvider).host;
      final port = _ref.read(websocketProvider).port;
      final connectedAt = _ref.read(websocketProvider).connectedAt;
      final fp = DaemonCertPinner.currentFingerprint;

      try {
        final client = _ref.read(sdkClientProvider);
        final status = await client.getDaemonStatusRaw();
        final dState = status['state'] as String? ?? 'unknown';
        final pid = status['pid'] as int?;
        final uptime = status['uptime'] as String?;

        // Try to extract version from status
        String? version;
        if (status['version'] != null) {
          version = status['version'] as String;
        } else if (status['build'] != null) {
          version = status['build'] as String;
        }

        result = ConnectionDetails(
          version: version,
          pid: pid,
          uptime: uptime,
          state: dState,
          host: host,
          port: port,
          tls: true,
          connectedAt: connectedAt,
          certFingerprint: fp,
        );
      } catch (e) {
        debugPrint('[warn] ConnectionDetails fetch daemon status: $e');
        // Still show connection metadata even if daemon is unreachable
        result = ConnectionDetails(
          host: host,
          port: port,
          tls: true,
          connectedAt: connectedAt,
          certFingerprint: fp,
        );
      }

      state = result;
    } catch (e) {
      debugPrint('[warn] ConnectionDetails fetch metadata: $e');
      // Best-effort — only connection metadata available
    } finally {
      _isFetching = false;
    }
  }

  @override
  void dispose() {
    _fetchTimer?.cancel();
    super.dispose();
  }
}

// ConnectionMonitor provider - manages WebSocket health monitoring
final connectionMonitorProvider = Provider<ConnectionMonitor>((ref) {
  final websocket = ref.watch(websocketProvider);
  final sdkClient = ref.watch(sdkClientProvider);
  final container = ref.container;

  final monitor = ConnectionMonitor(
    websocket: websocket,
    sdkClient: sdkClient,
    container: container,
  );

  ref.onDispose(() => monitor.dispose());
  return monitor;
});

/// Monitors WebSocket + HTTP health and updates connectionStateProvider.
class ConnectionMonitor {
  final WebSocketService _websocket;
  final SdkApiClient _sdkClient;
  final ProviderContainer _container;
  Timer? _timer;
  Timer? _debounceTimer;
  StreamSubscription<bool>? _connectionSub;
  Timer? _connectingTimer;

  bool? _pendingState;
  bool _confirmed = false;

  ConnectionMonitor({
    required WebSocketService websocket,
    required SdkApiClient sdkClient,
    required ProviderContainer container,
  })  : _websocket = websocket,
        _sdkClient = sdkClient,
        _container = container {
    _listenToWebSocket();
    _startHealthChecks();
  }

  void _listenToWebSocket() {
    _connectionSub = _websocket.connectionStream.listen((connected) {
      _proposeState(connected);
    });

    // Poll WebSocket's isConnecting flag to show "connecting..." state
    // This fires while the WebSocket is attempting to connect/reconnect.
    // Only writes to the provider when the value changes to avoid
    // excessive provider notifications.
    _connectingTimer = Timer.periodic(const Duration(milliseconds: 500), (_) {
      final newValue = _websocket.isConnecting;
      final currentValue = _container.read(isConnectingProvider.notifier).state;
      if (newValue != currentValue) {
        _container.read(isConnectingProvider.notifier).state = newValue;
      }
    });
  }

  void _proposeState(bool connected) {
    _debounceTimer?.cancel();
    final prevState = _pendingState;

    if (prevState == null) {
      _pendingState = connected;
      _confirmed = false;
      _debounceTimer = Timer(const Duration(milliseconds: 500), () {
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
    _debounceTimer = Timer(const Duration(milliseconds: 500), () {
      _applyState(connected);
    });
  }

  void _applyState(bool connected) {
    _pendingState = connected;
    _confirmed = true;
    _container.read(connectionStateProvider.notifier).state = connected;
    // Don't clear isConnecting here - let the polling timer manage it
    // Fetch daemon details on successful connect
    if (connected) {
      _fetchConnectionDetails();
    }
  }

  Future<void> _fetchConnectionDetails() async {
    try {
      final details = _container.read(connectionDetailsProvider.notifier);
      await details._fetch();
    } catch (e) {
      debugPrint('[warn] fetchConnectionDetails: $e');
      // Best-effort only; don't disrupt connection
    }
  }

  void _startHealthChecks() {
    _timer = Timer.periodic(const Duration(seconds: 30), (_) async {
      if (!_websocket.isConnected && !_websocket.isConnecting) {
        try {
          await _sdkClient.getDaemonStatus();
          _proposeState(true);
        } catch (e) {
          debugPrint('[warn] health check daemon status: $e');
          _proposeState(false);
        }
      }
    });
  }

  void dispose() {
    _timer?.cancel();
    _debounceTimer?.cancel();
    _connectionSub?.cancel();
    _connectingTimer?.cancel();
  }
}
