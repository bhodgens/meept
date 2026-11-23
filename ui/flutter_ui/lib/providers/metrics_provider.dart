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

  /// Review-verdict grades from GET /api/v1/metrics/reviews, keyed by
  /// group_by (the panel renders a table for `model` and one for `judge`).
  /// Null until the first fetch lands; an entry may hold null rows absent
  /// from a degraded payload.
  final Map<String, ReviewGrades?> reviewGrades;

  /// Model x category review counts from the same endpoint with
  /// `group_by=model_category`. Null until the first fetch lands; failures
  /// leave the previous value intact (best-effort, never blanks tiles).
  final List<ReviewBreakdownRow>? reviewBreakdown;

  const MetricsState({
    this.current,
    this.isLoading = false,
    this.error,
    this.modelUsage,
    this.agentUsage,
    this.totals,
    this.receivedAt,
    this.reviewGrades = const {},
    this.reviewBreakdown,
  });

  MetricsState copyWith({
    MetricsSnapshot? current,
    bool? isLoading,
    Object? error = _unset,
    Object? modelUsage = _unset,
    Object? agentUsage = _unset,
    Object? totals = _unset,
    Object? reviewGrades = _unset,
    Object? reviewBreakdown = _unset,
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
      reviewGrades: identical(reviewGrades, _unset)
          ? this.reviewGrades
          : (reviewGrades is Map<String, ReviewGrades?>
                ? reviewGrades
                : const {}),
      reviewBreakdown: identical(reviewBreakdown, _unset)
          ? this.reviewBreakdown
          : (reviewBreakdown is List<ReviewBreakdownRow>
                ? reviewBreakdown
                : null),
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
    unawaited(fetchReviewGrades(window: '30d', groupBy: 'model'));
    unawaited(fetchReviewGrades(window: '30d', groupBy: 'judge'));
    unawaited(fetchReviewBreakdown());

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
      // Preserve already-fetched review grades: the live payload does not
      // carry them (separate endpoint), so a fresh state must not reset
      // the grades sections back to 'fetching...'.
      reviewGrades: state.reviewGrades,
      reviewBreakdown: state.reviewBreakdown,
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
    await fetchReviewGrades(window: '30d', groupBy: 'model');
    await fetchReviewGrades(window: '30d', groupBy: 'judge');
    await fetchReviewBreakdown();
  }

  /// Fetch review-verdict grades for one group_by and merge the decoded
  /// payload into [MetricsState.reviewGrades] under that key. Errors
  /// degrade to an empty-rows entry so the grades section always renders;
  /// they never fail the live-metrics refresh.
  Future<void> fetchReviewGrades({
    String window = '30d',
    required String groupBy,
  }) async {
    try {
      _checkMounted();
      final data = await sdkClient.getReviewMetrics(
        window: window,
        groupBy: groupBy,
      );
      if (_disposed) return;
      final decoded = ReviewGrades.fromJson(Map<String, dynamic>.from(data));
      if (_disposed) return;
      state = state.copyWith(
        reviewGrades: {...state.reviewGrades, groupBy: decoded},
      );
    } catch (e) {
      if (_disposed) return;
      // Degraded: an empty-rows payload under the key keeps the panel's
      // grades section present (a lowercase note) instead of throwing.
      state = state.copyWith(
        reviewGrades: {
          ...state.reviewGrades,
          groupBy: ReviewGrades(window: window, groupBy: groupBy),
        },
      );
    }
  }

  /// Fetch the model x category breakdown (same endpoint, `group_by=
  /// model_category`) and store the rows. Best-effort: on failure the
  /// previous value (or null before the first success) is kept, so the
  /// breakdown section never blanks the existing tiles.
  Future<void> fetchReviewBreakdown({String window = '24h'}) async {
    try {
      _checkMounted();
      final rows = await sdkClient.getReviewBreakdown(window: window);
      if (_disposed) return;
      state = state.copyWith(reviewBreakdown: rows);
    } catch (_) {
      // Degraded: keep whatever rows we had; the panel shows the
      // placeholder until a later poll succeeds.
      if (_disposed) return;
    }
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
