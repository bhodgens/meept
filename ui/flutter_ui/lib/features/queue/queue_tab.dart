import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../providers/providers.dart' show sdkClientProvider;
import '../../services/sdk_client.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'queue_detail.dart';
import 'queue_list.dart';

/// A single queue job rendered in the queue view.
///
/// Parsed from the raw `*queue.Job` JSON returned by `GET /api/v1/queue/jobs`
/// (the OpenAPI spec leaves the Job entity untyped, so the SDK client hands
/// back raw maps — same shape the TUI's `types.QueueJob` mirrors).
class QueueJobView {
  final String id;
  final String type;
  final String state;
  final int priority;
  final String taskId;
  final int retryCount;
  final int maxRetries;
  final String error;
  final String claimedBy;
  final DateTime? createdAt;
  final Map<String, dynamic>? payload;

  const QueueJobView({
    required this.id,
    required this.type,
    required this.state,
    required this.priority,
    required this.taskId,
    required this.retryCount,
    required this.maxRetries,
    required this.error,
    required this.claimedBy,
    required this.createdAt,
    required this.payload,
  });

  factory QueueJobView.fromJson(Map<String, dynamic> json) {
    final created = json['created_at'];
    final payload = json['payload'];
    return QueueJobView(
      id: json['id'] as String? ?? '',
      type: json['type'] as String? ?? '',
      state: json['state'] as String? ?? json['status'] as String? ?? 'pending',
      priority: (json['priority'] as num?)?.toInt() ?? 0,
      taskId: json['task_id'] as String? ?? '',
      retryCount: (json['retry_count'] as num?)?.toInt() ?? 0,
      maxRetries: (json['max_retries'] as num?)?.toInt() ?? 0,
      error: json['error'] as String? ?? '',
      claimedBy: json['claimed_by'] as String? ?? '',
      createdAt: created is String ? DateTime.tryParse(created) : null,
      payload: payload is Map<String, dynamic> ? payload : null,
    );
  }

  /// TUI priority names (internal/tui/models/queue.go updateTable).
  String get priorityName {
    switch (priority) {
      case 1:
        return 'low';
      case 2:
        return 'normal';
      case 3:
        return 'high';
      case 4:
        return 'urgent';
      default:
        return '$priority';
    }
  }

  /// Short display id, mirroring the TUI's 20-column truncation.
  String get shortId {
    if (id.length > 20) return '${id.substring(0, 17)}...';
    return id;
  }
}

/// Queue statistics parsed from `GET /api/v1/queue/stats`.
///
/// The endpoint serializes `*queue.QueueStats` (internal/queue/store.go)
/// which has NO json tags, so the wire keys are the Go field names
/// (`ByState`, `DeadCount`). Both casings are accepted so a future tagged
/// response keeps parsing.
class QueueStatsView {
  final Map<String, int> byState;
  final int deadCount;

  const QueueStatsView({required this.byState, required this.deadCount});

  factory QueueStatsView.fromJson(Map<String, dynamic> json) {
    final raw = json['by_state'] ?? json['ByState'];
    final byState = <String, int>{};
    if (raw is Map) {
      raw.forEach((k, v) {
        if (v is num) byState[k.toString()] = v.toInt();
      });
    }
    final dead = (json['dead_count'] ?? json['DeadCount']) as num?;
    return QueueStatsView(byState: byState, deadCount: dead?.toInt() ?? 0);
  }

  int get pending => byState['pending'] ?? 0;
  int get claimed => byState['claimed'] ?? 0;
  int get processing => byState['processing'] ?? 0;
  int get completed => byState['completed'] ?? 0;
  int get failed => byState['failed'] ?? 0;

  /// In-flight depth: everything not yet completed/failed/dead.
  int get depth => pending + claimed + processing;
}

/// State filter, mirroring the TUI's p/f/c/a filter keys.
enum QueueFilter { all, pending, processing, completed, failed }

/// Immutable view state for the queue tab.
class QueueViewState {
  final List<QueueJobView> jobs;
  final QueueStatsView? stats;
  final bool isLoading;
  final String? error;

  const QueueViewState({
    this.jobs = const [],
    this.stats,
    this.isLoading = false,
    this.error,
  });

  QueueViewState copyWith({
    List<QueueJobView>? jobs,
    QueueStatsView? stats,
    bool? isLoading,
    String? error,
  }) {
    return QueueViewState(
      jobs: jobs ?? this.jobs,
      stats: stats ?? this.stats,
      isLoading: isLoading ?? this.isLoading,
      error: error,
    );
  }
}

/// Fetches queue jobs + stats over the same REST endpoints the jobProvider
/// polls (GET /api/v1/queue/jobs, GET /api/v1/queue/stats), but keeps the
/// full-fidelity job fields (payload, error, retry counts, priority) that
/// the shared JobUpdate projection drops.
class QueueViewController extends StateNotifier<QueueViewState> {
  QueueViewController(this._client)
    : super(const QueueViewState(isLoading: true));

  final SdkApiClient _client;
  int _generation = 0;

  Future<void> load() async {
    final gen = ++_generation;
    try {
      final rawJobs = await _client.listJobs();
      final jobs = rawJobs.map(QueueJobView.fromJson).toList(growable: false);

      // Stats fetch is non-fatal; the header keeps its last known values.
      QueueStatsView? stats;
      try {
        stats = QueueStatsView.fromJson(await _client.getQueueStats());
      } catch (_) {
        stats = state.stats;
      }

      if (!mounted || gen != _generation) return;
      state = state.copyWith(
        jobs: jobs,
        stats: stats,
        isLoading: false,
        error: null,
      );
    } catch (e) {
      if (!mounted || gen != _generation) return;
      state = state.copyWith(
        isLoading: false,
        error: 'Failed to load queue: ${e.toString()}',
      );
    }
  }

  Future<void> refresh() async {
    state = state.copyWith(isLoading: true);
    await load();
  }
}

/// Queue data controller.
final queueViewProvider =
    StateNotifierProvider<QueueViewController, QueueViewState>((ref) {
      return QueueViewController(ref.watch(sdkClientProvider));
    });

/// Current state filter (all/pending/processing/completed/failed).
final queueFilterProvider = StateProvider<QueueFilter>(
  (ref) => QueueFilter.all,
);

/// Currently selected job shown in the detail pane.
final activeQueueJobProvider = StateProvider<QueueJobView?>((ref) => null);

/// Pretty-printed payload for the detail pane.
String formatQueuePayload(Map<String, dynamic> payload) {
  try {
    const encoder = JsonEncoder.withIndent('  ');
    return encoder.convert(payload);
  } catch (_) {
    return payload.toString();
  }
}

/// Queue tab - stats header, filter chips, and a master-detail jobs view
/// mirroring the TUI queue view (internal/tui/models/queue.go).
class QueueTab extends ConsumerStatefulWidget {
  const QueueTab({super.key});

  @override
  ConsumerState<QueueTab> createState() => _QueueTabState();
}

class _QueueTabState extends ConsumerState<QueueTab> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(queueViewProvider.notifier).load();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          const _QueueStatsHeader(),
          Divider(height: 1, color: CyberpunkColors.midGray),
          const Expanded(
            child: Row(
              children: [
                SizedBox(width: 340, child: QueueList()),
                VerticalDivider(width: 1),
                Expanded(child: QueueDetail()),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Stats band: depth + per-state counts, mirroring the TUI stats panel.
class _QueueStatsHeader extends ConsumerWidget {
  const _QueueStatsHeader();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final view = ref.watch(queueViewProvider);
    final stats = view.stats;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      child: Row(
        children: [
          Text(
            'job queue',
            style: CyberpunkTypography.headlineMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(width: 24),
          Expanded(
            child: stats == null
                ? Text(
                    'loading statistics...',
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.midGray,
                    ),
                  )
                : Wrap(
                    spacing: 16,
                    runSpacing: 4,
                    children: [
                      _stat('depth: ${stats.depth}'),
                      _stat(
                        'pending: ${stats.pending}',
                        color: CyberpunkColors.yellowWarning,
                      ),
                      _stat(
                        'processing: ${stats.processing}',
                        color: CyberpunkColors.blueInfo,
                      ),
                      _stat(
                        'completed: ${stats.completed}',
                        color: CyberpunkColors.greenSuccess,
                      ),
                      _stat(
                        'failed: ${stats.failed}',
                        color: CyberpunkColors.redAlert,
                      ),
                      // TUI only surfaces the dead count when non-zero.
                      if (stats.deadCount > 0)
                        _stat(
                          'dead: ${stats.deadCount}',
                          color: CyberpunkColors.redAlert,
                        ),
                    ],
                  ),
          ),
        ],
      ),
    );
  }

  Widget _stat(String text, {Color? color}) {
    return Text(
      text,
      style: CyberpunkTypography.bodySmall.copyWith(
        color: color ?? CyberpunkColors.midGray,
        fontFamily: 'SourceCodePro',
      ),
    );
  }
}
