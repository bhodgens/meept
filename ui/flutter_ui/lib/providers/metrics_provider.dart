import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import 'providers.dart';
import '../services/websocket_service.dart';

const _unset = Object();

/// Metrics state for the live metrics panel
class MetricsState {
  final MetricsSnapshot? current;
  final bool isLoading;
  final String? error;

  const MetricsState({
    this.current,
    this.isLoading = false,
    this.error,
  });

  MetricsState copyWith({
    MetricsSnapshot? current,
    bool? isLoading,
    Object? error = _unset,
  }) {
    return MetricsState(
      current: current ?? this.current,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

/// StateNotifier that manages metrics from both HTTP polling and
/// WebSocket updates for live metrics display (Task 19).
class MetricsNotifier extends StateNotifier<MetricsState> {
  MetricsNotifier({
    required this.sdkClient,
    required this.websocket,
  }) : super(const MetricsState(isLoading: true)) {
    _init();
  }

  final SdkApiClient sdkClient;
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
      final data = await sdkClient.getLiveMetrics();
      if (_disposed) return;
      // Try to parse as MetricsSnapshot from the raw response
      // The backend /metrics/live returns a map with metric fields
      final snapshotData = Map<String, dynamic>.from(data);
      if (_disposed) return;
      state = state.copyWith(
        current: MetricsSnapshot.fromJson(snapshotData),
        isLoading: false,
        error: null,
      );
    } catch (e) {
      if (_disposed) return;
      state = state.copyWith(
        error: 'Failed to load metrics: ${e.toString()}',
        isLoading: false,
      );
    }
  }

  void _subscribeToMetrics() {
    if (_metricsSubscription != null) return;

    _metricsSubscription = websocket.subscribeToMetrics().listen((msg) {
      if (_disposed) return;
      // The websocket subscription filters for type == 'metrics_update'
      // and the message is already flattened by WebSocketService
      try {
        state = state.copyWith(
          current: MetricsSnapshot.fromJson(msg),
          error: null,
        );
      } catch (e) {
        state = state.copyWith(
          error: 'Failed to parse metrics: ${e.toString()}',
        );
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
    state = state.copyWith(isLoading: true);
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
    StateNotifierProvider<MetricsNotifier, MetricsState>((ref) {
  final client = ref.watch(sdkClientProvider);
  final websocket = ref.watch(websocketProvider);
  return MetricsNotifier(sdkClient: client, websocket: websocket);
});
