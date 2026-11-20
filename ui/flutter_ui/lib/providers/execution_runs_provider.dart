import 'dart:async';

import 'package:flutter/foundation.dart' show debugPrint, visibleForTesting;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/sdk_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart';

const _unset = Object();

/// Newest-first list cap shared by the live merge and the poll merge.
const _maxRuns = 100;

/// Issue #160 follow-up: stateful execution-run surface.
///
/// Fully server-backed (not live-event-only): the daemon persists every
/// bot.result / employee.result into execution_runs.db, and this provider
/// POLLS GET /api/v1/executions on an interval WHILE THE AGENTS TAB IS
/// VISIBLE (plan contract: master.md "GUI polls ... while the agents tab is
/// visible"). Visibility is driven by the tab's mount/unmount seam
/// ([ExecutionRunsNotifier.setPollingActive]): the constructor starts no
/// timer, so a hidden tab never polls runs + roster + one histogram GET per
/// roster agent. A client that disconnects and reconnects - or a daemon
/// that restarts - still shows the full recent history. Live WS frames
/// merge in immediately between polls so the list feels instant; the next
/// poll reconciles against the server truth without dropping a
/// projector-lagged live row (mergeLiveRuns, TUI parity).
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
    // connectionStream replays the current state, so a reconnect re-polls
    // without waiting for the next tick - but only while polling is active.
    _connSub = websocket.connectionStream.listen((connected) {
      if (connected && _pollingActive) refresh();
    });
    // No polling until the agents tab declares itself visible (M3): the
    // constructor must not arm the 5s runs + roster + per-agent histogram
    // cadence for the app lifetime.
  }

  final SdkApiClient sdkClient;
  final WebSocketService websocket;
  StreamSubscription<Map<String, dynamic>>? _sub;
  StreamSubscription<bool>? _connSub;
  Timer? _pollTimer;
  bool _disposed = false;
  bool _refreshInFlight = false;

  /// Visibility gate (M3): true while the agents tab is mounted/visible.
  bool _pollingActive = false;

  /// True while the visibility-gated poll cadence is armed (test pin).
  @visibleForTesting
  bool get isPollingActive => _pollingActive;

  /// Tab-driven pause/resume seam (M3). Activation refreshes immediately,
  /// then resumes the 5s cadence; deactivation cancels the timer but keeps
  /// the live WS subscription and cached state so re-entry is instant.
  /// Idempotent: repeated calls with the same value are no-ops.
  void setPollingActive(bool active) {
    if (_disposed || _pollingActive == active) return;
    _pollingActive = active;
    if (active) {
      refresh();
      _pollTimer?.cancel();
      _pollTimer = Timer.periodic(const Duration(seconds: 5), (_) => refresh());
    } else {
      _pollTimer?.cancel();
      _pollTimer = null;
    }
  }

  /// One poll cycle: runs page + roster counts + histograms for the agents
  /// present in the roster. Best-effort per leg: a failing histogram for
  /// one agent never blanks the runs list. Poll ticks are visibility-gated;
  /// an explicit refresh (panel button, reconnect while active) always runs.
  Future<void> refresh() async {
    if (_disposed || _refreshInFlight) return;
    _refreshInFlight = true;
    try {
      final raw = await sdkClient.getExecutionRuns(limit: _maxRuns);
      if (_disposed) return;
      final runsRaw = (raw['runs'] as List?) ?? const [];
      final serverRuns = runsRaw
          .whereType<Map>()
          .map(
            (m) => ExecutionResultEvent.fromJson(Map<String, dynamic>.from(m)),
          )
          .where((r) => r.agentId.isNotEmpty && r.status.isNotEmpty)
          .toList(growable: false);
      final total = (raw['total'] as num?)?.toInt() ?? serverRuns.length;
      state = state.copyWith(
        runs: mergeLiveRuns(serverRuns, state.runs),
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
  /// between polls, deduped by run identity so a redelivery (or a frame
  /// already reconciled by a poll) never shows the same execution twice.
  /// The next server poll reconciles ordering/dedup against durable truth.
  void _handleLiveFrame(Map<String, dynamic> frame) {
    try {
      final run = ExecutionResultEvent.fromJson(frame);
      if (run.agentId.isEmpty || run.status.isEmpty) return;
      final composite = _runCompositeKey(run);
      final idKey = run.id != 0 ? _runIdKey(run) : null;
      final next = <ExecutionResultEvent>[
        run,
        ...state.runs.where(
          (r) =>
              _runCompositeKey(r) != composite &&
              (idKey == null || r.id == 0 || _runIdKey(r) != idKey),
        ),
      ];
      if (next.length > _maxRuns) next.removeRange(_maxRuns, next.length);
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

/// Identity of one execution row (L3). Persisted server rows carry the
/// store id (activity.ExecutionRun.ID); live WS frames carry id 0. The
/// composite key is tier+agent+status+second-truncated event time (the
/// store truncates created_at to whole seconds - internal/activity/
/// execution_store.go Append), and bridges a live frame to the server row
/// of the SAME execution once the projector has persisted it. Two distinct
/// runs of one agent within the same second share a composite key; that is
/// the documented precision limit, the same trade-off the TUI merge made
/// before ids existed.
String _runIdKey(ExecutionResultEvent r) => 'id:${r.id}';

String _runCompositeKey(ExecutionResultEvent r) {
  final t = r.effectiveTime;
  final sec = t == null ? 0 : t.toUtc().millisecondsSinceEpoch ~/ 1000;
  return '${r.tier}|${r.agentId}|${r.status}|$sec';
}

/// Reconciles a freshly-fetched server page with the live rows cached since
/// the last poll (L3, mirrors internal/tui/agents_panel.go mergeLiveRuns).
/// The execution projector appends the durable row asynchronously off the
/// bus (internal/activity/execution_projector.go), so a result that reached
/// this client just before a poll can be newer than the snapshot the poll
/// returns. Replacing the list wholesale dropped that live row for one poll
/// and brought it back on the next - a visible flicker.
///
/// Live rows STRICTLY newer than the newest server row and not already in
/// the page are prepended in their prior newest-first order. Identity:
/// id equality when BOTH sides carry an id; otherwise the composite key
/// (tier+agent+status+second) - which also matches an id-less live frame
/// against the server row of the same execution. An empty server page has
/// no max, so every timestamped live row survives. No newer live row =>
/// the page is returned unchanged. A live row equal to the newest server
/// second is treated as already persisted (the store stamps whole
/// seconds); the next page carries it with its durable id.
List<ExecutionResultEvent> mergeLiveRuns(
  List<ExecutionResultEvent> server,
  List<ExecutionResultEvent> live,
) {
  DateTime? maxServer;
  final seenIds = <String>{};
  final seenComposite = <String>{};
  for (final r in server) {
    if (r.id != 0) seenIds.add(_runIdKey(r));
    seenComposite.add(_runCompositeKey(r));
    final t = r.effectiveTime;
    if (t != null && (maxServer == null || t.isAfter(maxServer))) {
      maxServer = t;
    }
  }

  final preserved = <ExecutionResultEvent>[];
  for (final r in live) {
    final t = r.effectiveTime;
    if (t == null) continue;
    if (maxServer != null && !t.isAfter(maxServer)) continue;
    if (r.id != 0 && seenIds.contains(_runIdKey(r))) continue;
    if (seenComposite.contains(_runCompositeKey(r))) continue;
    preserved.add(r);
  }

  final merged = [...preserved, ...server];
  if (merged.length > _maxRuns) merged.removeRange(_maxRuns, merged.length);
  return List.unmodifiable(merged);
}

final executionRunsProvider =
    StateNotifierProvider<ExecutionRunsNotifier, ExecutionRunsState>((ref) {
      final client = ref.watch(sdkClientProvider);
      final ws = ref.watch(websocketProvider);
      return ExecutionRunsNotifier(sdkClient: client, websocket: ws);
    });
