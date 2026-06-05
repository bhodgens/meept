import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'async_state.dart';
import 'providers.dart';
import '../services/websocket_service.dart';

/// StateNotifier that manages metrics from both HTTP polling and
/// WebSocket updates for live metrics display (Task 19).
class MetricsNotifier extends StateNotifier<AsyncState<MetricsSnapshot>> {
  MetricsNotifier({
    required this.apiClient,
    required this.websocket,
  }) : super(const AsyncState.loading()) {
    _init();
  }

  final ApiClient apiClient;
  final WebSocketService websocket;
  StreamSubscription<Map<String, dynamic>>? _metricsSubscription;
  StreamSubscription<bool>? _connectionSubscription;
  Timer? _pollTimer;
  bool _disposed = false;

  void _checkMounted() {
    // Guard against using StateNotifier after dispose
    assert(() {
      if (_disposed) {
        throw StateError('MetricsNotifier was used after dispose');
      }
      return true;
    }());
  }

  void _init() {
    // Initial fetch from HTTP
    _fetchMetrics();

    // Subscribe to WebSocket metrics updates
    if (websocket.isConnected) {
      _subscribeToMetrics();
    } else {
      // Start polling as fallback if WS not connected yet
      _startPolling();
    }

    // Listen for WS connection state changes
    _connectionSubscription = websocket.connectionStream.listen((connected) {
      if (connected) {
        _pollTimer?.cancel();
        _pollTimer = null;
        _subscribeToMetrics();
      } else {
        _metricsSubscription?.cancel();
        _metricsSubscription = null;
        _startPolling();
      }
    });
  }

  Future<void> _fetchMetrics() async {
    try {
      _checkMounted();
      final data = await apiClient.getLiveMetrics();
      if (_disposed) return;
      final snapshotData = Map<String, dynamic>.from(data);
      if (_disposed) return;
      state = AsyncState.data(MetricsSnapshot.fromJson(snapshotData));
    } catch (e, st) {
      if (_disposed) return;
      state = AsyncState.error(e, st);
    }
  }

  void _subscribeToMetrics() {
    if (_metricsSubscription != null) return;

    _metricsSubscription = websocket.subscribeToMetrics().listen((msg) {
      try {
        state = AsyncState.data(MetricsSnapshot.fromJson(msg));
      } catch (e, st) {
        state = AsyncState.error(e, st);
      }
    });
  }

  void _startPolling() {
    _pollTimer?.cancel();
    _pollTimer = Timer.periodic(const Duration(seconds: 10), (_) {
      _fetchMetrics();
    });
  }

  /// Refresh metrics manually
  Future<void> refresh() async {
    state = const AsyncState.loading();
    await _fetchMetrics();
  }

  @override
  void dispose() {
    _disposed = true;
    _connectionSubscription?.cancel();
    _connectionSubscription = null;
    _metricsSubscription?.cancel();
    _metricsSubscription = null;
    _pollTimer?.cancel();
    _pollTimer = null;
    super.dispose();
  }
}

/// Metrics provider
final metricsProvider =
    StateNotifierProvider<MetricsNotifier, AsyncState<MetricsSnapshot>>((ref) {
  final client = ref.watch(apiClientProvider);
  final websocket = ref.watch(websocketProvider);
  return MetricsNotifier(apiClient: client, websocket: websocket);
});
