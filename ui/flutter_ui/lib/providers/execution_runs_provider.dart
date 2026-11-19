import 'dart:async';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/sdk_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart';

const _unset = Object();

/// Issue #160 follow-up: stateful execution-run surface.
///
/// Fully server-backed (not live-event-only): the daemon persists every
/// bot.result / employee.result into execution_runs.db, and this provider
/// POLLS GET /api/v1/executions on an interval. A client that disconnects
/// and reconnects - or a daemon that restarts - still shows the full recent
/// history. Live WS frames merge in immediately between polls so the list
/// feels instant; the next poll reconciles against the server truth.
///
/// Also owns:
///  - per-agent 8-cell token histograms (GET /api/v1/executions/histogram),
///    refreshed on the same poll tick, for the 8-bit activity bars;
///  - the roster active counts (GET /api/v1/agents/roster) used by the
///    per-card "other concurrent runs" counter.
class ExecutionRunsState {
  /// Newest-first persisted runs (server order), capped at 100.
  final List<ExecutionResultEvent> runs;

  /// Total matching rows server-side (before the page limit).
  final int total;

  /// Per-agent token cells: 8 ints, oldest first, last hour.
  final Map<String, List<int>> histograms;

  /// Roster active_count per agent id (live episodes/plans).
  final Map<String, int> activeCounts;

  /// Last poll/parse failure for the panel's error line; null = healthy.
  final String? error;

  /// True once the first server poll has answered (loading state).
  final bool loaded;

  const ExecutionRunsState({
    this.runs = const [],
    this.total = 0,
    this.histograms = const {},
    this.activeCounts = const {},
    this.error,
    this.loaded = false,
  });

  /// Sum of active counts across all OTHER agents (the per-card counter
  /// excludes the card's own agent).
  int otherActiveCount(String excludeAgentId) {
    var sum = 0;
    for (final entry in activeCounts.entries) {
      if (entry.key != excludeAgentId) sum += entry.value;
    }
    return sum;
  }

  ExecutionRunsState copyWith({
    List<ExecutionResultEvent>? runs,
    int? total,
    Map<String, List<int>>? histograms,
    Map<String, int>? activeCounts,
    Object? error = _unset,
    bool? loaded,
  }) {
    return ExecutionRunsState(
      runs: runs ?? this.runs,
      total: total ?? this.total,
      histograms: histograms ?? this.histograms,
      activeCounts: activeCounts ?? this.activeCounts,
      error: identical(error, _unset) ? this.error : error as String?,
      loaded: loaded ?? this.loaded,
    );
  }
}

class ExecutionRunsNotifier extends StateNotifier<ExecutionRunsState> {
  ExecutionRunsNotifier({required this.sdkClient, required this.websocket})
    : super(const ExecutionRunsState()) {
    _sub = websocket.subscribeToExecutionResults().listen(_handleLiveFrame);
    _pollTimer = Timer.periodic(const Duration(seconds: 5), (_) => refresh());
    // First fetch immediately; connectionStream replays the current state,
    // so a reconnect re-polls without waiting for the next tick.
    _connSub = websocket.connectionStream.listen((connected) {
      if (connected) refresh();
    });
    refresh();
  }

  final SdkApiClient sdkClient;
  final WebSocketService websocket;
  StreamSubscription<Map<String, dynamic>>? _sub;
  StreamSubscription<bool>? _connSub;
  Timer? _pollTimer;
  bool _disposed = false;
  bool _refreshInFlight = false;

  /// One poll cycle: runs page + roster counts + histograms for the agents
  /// present in the roster. Best-effort per leg: a failing histogram for
  /// one agent never blanks the runs list.
  Future<void> refresh() async {
    if (_disposed || _refreshInFlight) return;
    _refreshInFlight = true;
    try {
      final raw = await sdkClient.getExecutionRuns(limit: 100);
      if (_disposed) return;
      final runsRaw = (raw['runs'] as List?) ?? const [];
      final runs = runsRaw
          .whereType<Map>()
          .map(
            (m) => ExecutionResultEvent.fromJson(Map<String, dynamic>.from(m)),
          )
          .where((r) => r.agentId.isNotEmpty && r.status.isNotEmpty)
          .toList(growable: false);
      final total = (raw['total'] as num?)?.toInt() ?? runs.length;
      state = state.copyWith(
        runs: runs,
        total: total,
        error: null,
        loaded: true,
      );
    } catch (e) {
      if (_disposed) return;
      debugPrint('[exec-runs] poll failed: $e');
      state = state.copyWith(error: e.toString(), loaded: true);
    }

    // Roster active counts (per-card counter source).
    try {
      final roster = await sdkClient.getAgentsRoster();
      if (_disposed) return;
      final rows = (roster['agents'] as List?) ?? const [];
      final counts = <String, int>{};
      for (final row in rows) {
        if (row is Map) {
          final id = row['id'];
          final active = row['active_count'];
          if (id is String && id.isNotEmpty) {
            counts[id] = active is num ? active.toInt() : 0;
          }
        }
      }
      state = state.copyWith(activeCounts: counts);
      await _refreshHistograms(counts.keys);
    } catch (e) {
      if (_disposed) return;
      debugPrint('[exec-runs] roster poll failed: $e');
    } finally {
      _refreshInFlight = false;
    }
  }

  Future<void> _refreshHistograms(Iterable<String> agentIds) async {
    final next = Map<String, List<int>>.from(state.histograms);
    for (final id in agentIds) {
      if (_disposed) return;
      try {
        final raw = await sdkClient.getExecutionHistogram(id);
        final cellsRaw = (raw['cells'] as List?) ?? const [];
        final cells = cellsRaw
            .map((v) => v is num ? v.toInt() : 0)
            .toList(growable: false);
        if (cells.length == 8) next[id] = cells;
      } catch (e) {
        debugPrint('[exec-runs] histogram poll failed for $id: $e');
      }
    }
    if (!_disposed) state = state.copyWith(histograms: next);
  }

  /// Live WS frame: merge immediately (newest first) so the list updates
  /// between polls. The next server poll reconciles ordering/dedup.
  void _handleLiveFrame(Map<String, dynamic> frame) {
    try {
      final run = ExecutionResultEvent.fromJson(frame);
      if (run.agentId.isEmpty || run.status.isEmpty) return;
      final next = <ExecutionResultEvent>[run, ...state.runs];
      if (next.length > 100) next.removeRange(100, next.length);
      state = state.copyWith(runs: List.unmodifiable(next));
    } catch (e) {
      debugPrint('[exec-runs] live frame parse failure: $e');
    }
  }

  void clear() {
    state = state.copyWith(runs: const [], total: 0);
  }

  @override
  void dispose() {
    _disposed = true;
    _pollTimer?.cancel();
    _sub?.cancel();
    _connSub?.cancel();
    super.dispose();
  }
}

final executionRunsProvider =
    StateNotifierProvider<ExecutionRunsNotifier, ExecutionRunsState>((ref) {
      final client = ref.watch(sdkClientProvider);
      final ws = ref.watch(websocketProvider);
      return ExecutionRunsNotifier(sdkClient: client, websocket: ws);
    });
