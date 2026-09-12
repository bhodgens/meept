import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import 'providers.dart';
import '../services/websocket_service.dart';

const _unset = Object();

/// Metrics state for the live metrics panel.
///
/// The `models`, `agents`, and `totals` sections are nullable so the panel
/// can tell "the daemon did not report this" (null -> short lowercase note)
/// apart from "the daemon reported nothing" (empty list -> empty message).
class MetricsState {
  final MetricsSnapshot? current;
  final bool isLoading;
  final String? error;
  final List<ModelUsage>? modelUsage;
  final List<AgentUsage>? agentUsage;
  final MetricsTotals? totals;

  /// Wall-clock time of the last successful payload, used for the panel's
  /// 'updated <time>' stamp. Null until the first successful fetch.
  final DateTime? receivedAt;

  const MetricsState({
    this.current,
    this.isLoading = false,
    this.error,
    this.modelUsage,
    this.agentUsage,
    this.totals,
    this.receivedAt,
  });

  MetricsState copyWith({
    MetricsSnapshot? current,
    bool? isLoading,
    Object? error = _unset,
    Object? modelUsage = _unset,
    Object? agentUsage = _unset,
    Object? totals = _unset,
    DateTime? receivedAt,
  }) {
    return MetricsState(
      current: current ?? this.current,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset)
          ? this.error
          : (error is String ? error : null),
      modelUsage: identical(modelUsage, _unset)
          ? this.modelUsage
          : (modelUsage is List<ModelUsage> ? modelUsage : null),
      agentUsage: identical(agentUsage, _unset)
          ? this.agentUsage
          : (agentUsage is List<AgentUsage> ? agentUsage : null),
      totals: identical(totals, _unset)
          ? this.totals
          : (totals is MetricsTotals ? totals : null),
      receivedAt: receivedAt ?? this.receivedAt,
    );
  }
}

/// StateNotifier that manages metrics from both HTTP polling and
/// WebSocket updates for live metrics display (Task 19).
class MetricsNotifier extends StateNotifier<MetricsState> {
  MetricsNotifier({required this.sdkClient, required this.websocket})
    : super(const MetricsState(isLoading: true)) {
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

  /// Build a fresh state from one payload.
  ///
  /// Every optional section is parsed with absent-aware helpers; a missing
  /// or malformed `models` / `agents` / `totals` leaves that state field
  /// null rather than throwing, so the existing tiles keep rendering.
  MetricsState _stateFromPayload(Map<String, dynamic> data) {
    return MetricsState(
      current: MetricsSnapshot.fromJson(data),
      modelUsage: ModelUsage.parseList(data['models']),
      agentUsage: AgentUsage.parseList(data['agents']),
      totals: MetricsTotals.parse(data['totals']),
      receivedAt: DateTime.now(),
      isLoading: false,
      error: null,
    );
  }

  Future<void> _fetchMetrics() async {
    try {
      _checkMounted();
      final data = await sdkClient.getLiveMetrics();
      if (_disposed) return;
      final snapshotData = Map<String, dynamic>.from(data);
      if (_disposed) return;
      state = _stateFromPayload(snapshotData);
    } catch (e) {
      if (_disposed) return;
      state = state.copyWith(
        error: 'failed to load metrics: ${e.toString()}',
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
        state = _stateFromPayload(msg);
      } catch (e) {
        state = state.copyWith(
          error: 'failed to parse metrics: ${e.toString()}',
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
final metricsProvider = StateNotifierProvider<MetricsNotifier, MetricsState>((
  ref,
) {
  final client = ref.watch(sdkClientProvider);
  final websocket = ref.watch(websocketProvider);
  return MetricsNotifier(sdkClient: client, websocket: websocket);
});
