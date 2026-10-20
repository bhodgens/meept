import 'dart:async';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/acp_permission.dart';
import '../services/sdk_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart';

const _unset = Object();

/// State for the ACP human-permission relay surface (leaf 05, Contract E).
///
/// `pending` is keyed by request id and kept in FIFO order (Dart maps
/// preserve insertion order, and re-upserting an existing id keeps its
/// queue position). The dialog shows the head; the status-bar badge shows
/// [count].
class AcpPermissionState {
  final Map<String, AcpPendingPermission> pending;

  /// Whether the relay is enabled daemon-side (from the GET envelope).
  final bool enabled;

  /// Last resolve failure message (surfaced in the dialog; cleared on the
  /// next successful fetch/resolve).
  final String? error;

  const AcpPermissionState({
    this.pending = const {},
    this.enabled = false,
    this.error,
  });

  List<AcpPendingPermission> get queue => pending.values.toList();

  int get count => pending.length;

  AcpPendingPermission? get head =>
      pending.values.isEmpty ? null : pending.values.first;

  AcpPermissionState copyWith({
    Map<String, AcpPendingPermission>? pending,
    bool? enabled,
    Object? error = _unset,
  }) {
    return AcpPermissionState(
      pending: pending ?? this.pending,
      enabled: enabled ?? this.enabled,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

/// Owns the pending-permission queue: initial + reconnect HTTP catch-up
/// (GET /api/v1/acp/permissions) merged with live WS frames
/// (acp.permission.requested / .resolved), and POSTs the user's answer to
/// /api/v1/acp/permissions/resolve.
///
/// Cleared entries: a resolved frame (external answer — TUI or CLI — or a
/// relay timeout) removes the entry by id; the dialog host watches the
/// head and closes/swaps when it changes. A successful local resolve
/// removes it optimistically; the daemon's echo resolved frame is then an
/// idempotent no-op.
class AcpPermissionNotifier extends StateNotifier<AcpPermissionState> {
  AcpPermissionNotifier({required this.sdkClient, required this.websocket})
    : super(const AcpPermissionState()) {
    _init();
  }

  final SdkApiClient sdkClient;
  final WebSocketService websocket;

  StreamSubscription<Map<String, dynamic>>? _permissionSub;
  StreamSubscription<bool>? _connectionSub;
  bool _disposed = false;
  final Stopwatch _clock = Stopwatch()..start();

  /// Wall-clock (ms) each id was locally added from a WS frame — lets
  /// [_refresh] keep only entries that RACED the GET (added after the
  /// fetch started) instead of resurrecting entries the snapshot dropped.
  final Map<String, int> _addedAtMs = {};

  void _init() {
    _permissionSub = websocket.subscribeToAcpPermissions().listen(_handleFrame);
    // Initial + reconnect catch-up: connectionStream is a BehaviorSubject,
    // so listening fires immediately with the current state — a GET runs on
    // first connect and on every reconnect afterwards (frames missed while
    // disconnected are recovered from the snapshot), and never runs while
    // offline. In hermetic widget tests the replayed value is `false`, so
    // no network touch happens unless a test drives the stream itself.
    _connectionSub = websocket.connectionStream.listen((connected) {
      if (connected) _refresh();
    });
  }

  /// GET /api/v1/acp/permissions and rebuild the queue from the snapshot.
  /// The snapshot is authoritative for everything known before the fetch
  /// started: an entry whose resolved frame was missed drops, a new request
  /// the WS never delivered appears. Entries added locally after the fetch
  /// started are kept — they raced a GET the server answered earlier.
  Future<void> _refresh() async {
    final fetchStartedAt = _clock.elapsedMilliseconds;
    try {
      final raw = await sdkClient.getAcpPermissions();
      if (_disposed) return;
      final enabled = raw['enabled'] == true;
      final rows = (raw['pending'] as List?) ?? const [];
      final fresh = <String, AcpPendingPermission>{};
      for (final row in rows) {
        if (row is Map) {
          final p = AcpPendingPermission.fromJson(
            Map<String, dynamic>.from(row),
          );
          if (p.id.isNotEmpty) fresh[p.id] = p;
        }
      }
      if (enabled) {
        for (final entry in state.pending.entries) {
          final added = _addedAtMs[entry.key] ?? 0;
          if (added > fetchStartedAt && !fresh.containsKey(entry.key)) {
            fresh[entry.key] = entry.value;
          }
        }
      } else if (state.pending.isNotEmpty) {
        // Disabled envelope while live WS state exists: the envelope
        // predates/disagrees with the frames — keep the WS-fed queue
        // rather than wiping a visible pending request.
        final merged = Map<String, AcpPendingPermission>.from(state.pending);
        state = state.copyWith(pending: merged, enabled: enabled);
        return;
      }
      _addedAtMs.removeWhere((id, _) => !fresh.containsKey(id));
      for (final id in fresh.keys) {
        _addedAtMs.putIfAbsent(id, () => fetchStartedAt);
      }
      state = state.copyWith(pending: fresh, enabled: enabled, error: null);
    } catch (e) {
      debugPrint('[acp-permission] snapshot fetch failed: $e');
    }
  }

  void _handleFrame(Map<String, dynamic> frame) {
    if (_disposed) return;
    final topic = frame['source_topic'] as String?;
    final id = frame['id'] as String? ?? '';
    if (id.isEmpty) return;
    if (topic == 'acp.permission.resolved') {
      _remove(id);
      return;
    }
    if (topic == 'acp.permission.requested') {
      _addedAtMs[id] = _clock.elapsedMilliseconds;
      final map = Map<String, AcpPendingPermission>.from(state.pending);
      map[id] = AcpPendingPermission.fromJson(frame);
      state = state.copyWith(pending: map, enabled: true);
    }
  }

  void _remove(String id) {
    _addedAtMs.remove(id);
    if (!state.pending.containsKey(id)) return;
    final map = Map<String, AcpPendingPermission>.from(state.pending)
      ..remove(id);
    state = state.copyWith(pending: map);
  }

  /// POST the user's decision ("allow" | "deny" | "always") for [id]. The
  /// decision string passes through untouched — the daemon validates the
  /// verb at the RPC/HTTP boundary ("always" accepted since
  /// acp-always-allow leaf 01). On success the
  /// entry is removed optimistically (the daemon's resolved echo frame is
  /// an idempotent no-op). On failure the entry STAYS — the request is
  /// still pending daemon-side until the relay times out — and
  /// [AcpPermissionState.error] records the reason so the dialog can
  /// retry.
  Future<void> resolve(String id, String decision) async {
    try {
      await sdkClient.resolveAcpPermission(id: id, decision: decision);
      if (_disposed) return;
      _remove(id);
      state = state.copyWith(error: null);
    } catch (e) {
      debugPrint('[acp-permission] resolve failed: $e');
      if (_disposed) return;
      state = state.copyWith(error: e.toString());
    }
  }

  /// Drop a locally-recorded error after the UI has surfaced it.
  void clearError() {
    if (state.error != null) state = state.copyWith(error: null);
  }

  @override
  void dispose() {
    _disposed = true;
    _permissionSub?.cancel();
    _connectionSub?.cancel();
    super.dispose();
  }
}

final acpPermissionProvider =
    StateNotifierProvider<AcpPermissionNotifier, AcpPermissionState>((ref) {
      final client = ref.watch(sdkClientProvider);
      final ws = ref.watch(websocketProvider);
      return AcpPermissionNotifier(sdkClient: client, websocket: ws);
    });
