import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'providers.dart';
import '../services/websocket_service.dart';

const _unset = Object();

/// Job update state - contains the latest job updates from the queue
/// (Task 20: Job queue updates)
class JobState {
  final List<JobUpdate> updates;
  final bool isLoading;
  final String? error;
  final int? queueDepth;

  const JobState({
    this.updates = const [],
    this.isLoading = false,
    this.error,
    this.queueDepth,
  });

  JobState copyWith({
    List<JobUpdate>? updates,
    bool? isLoading,
    Object? error = _unset,
    int? queueDepth,
  }) {
    return JobState(
      updates: updates ?? this.updates,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
      queueDepth: queueDepth ?? this.queueDepth,
    );
  }
}

/// Represents a real-time job update from the WebSocket stream
class JobUpdate {
  final String jobId;
  final String type;
  final String status;
  final String? agentId;
  final DateTime timestamp;

  const JobUpdate({
    required this.jobId,
    required this.type,
    required this.status,
    this.agentId,
    required this.timestamp,
  });

  factory JobUpdate.fromJson(Map<String, dynamic> json) {
    final ts = json['timestamp'];
    return JobUpdate(
      jobId: json['job_id'] as String? ?? json['id'] as String? ?? '',
      type: json['type'] as String? ?? '',
      status: json['status'] as String? ?? '',
      agentId: json['agent_id'] as String?,
      timestamp: ts != null
          ? (ts is String
              ? DateTime.parse(ts)
              : DateTime.fromMillisecondsSinceEpoch((ts as num).toInt()))
          : DateTime.now(),
    );
  }
}

/// StateNotifier that manages job queue state via HTTP polling and
/// WebSocket real-time updates (Task 20).
class JobNotifier extends StateNotifier<JobState> {
  JobNotifier({
    required this.apiClient,
    required this.websocket,
  }) : super(const JobState(isLoading: true)) {
    _init();
  }

  final ApiClient apiClient;
  final WebSocketService websocket;
  StreamSubscription<Map<String, dynamic>>? _jobsSubscription;
  StreamSubscription<bool>? _connectionSubscription;
  Timer? _pollTimer;

  void _init() {
    _fetchJobs();

    if (websocket.isConnected) {
      _subscribeToJobs();
    } else {
      _startPolling();
    }

    _connectionSubscription = websocket.connectionStream.listen((connected) {
      if (connected) {
        _subscribeToJobs();
      } else {
        _jobsSubscription?.cancel();
        _jobsSubscription = null;
        _startPolling();
      }
    });
  }

  Future<void> _fetchJobs() async {
    try {
      final jobs = await apiClient.listJobs();
      final stats = await apiClient.getQueueStats();
      final depth = stats['queue_depth'] as int? ?? stats['depth'] as int? ?? 0;

      state = state.copyWith(
        updates: jobs
            .map((j) => JobUpdate(
                  jobId: j.id,
                  type: j.type,
                  status: j.status,
                  agentId: j.agentId,
                  timestamp: j.createdAt,
                ))
            .toList(),
        queueDepth: depth,
        isLoading: false,
      );
    } catch (e) {
      state = state.copyWith(
        error: 'Failed to load jobs: ${e.toString()}',
        isLoading: false,
      );
    }
  }

  void _subscribeToJobs() {
    if (_jobsSubscription != null) return;

    _jobsSubscription = websocket.subscribeToJobs().listen((msg) {
      try {
        final update = JobUpdate.fromJson(msg);

        // Prepend new update to the front of the list, keeping max 50
        final newUpdates = [update, ...state.updates];
        if (newUpdates.length > 50) {
          state = state.copyWith(
            updates: newUpdates.sublist(0, 50),
            error: null,
          );
        } else {
          state = state.copyWith(
            updates: newUpdates,
            error: null,
          );
        }

        // Update queue depth if present
        if (msg['queue_depth'] != null) {
          final depth = msg['queue_depth'] as int;
          state = state.copyWith(queueDepth: depth);
        }
      } catch (e) {
        state = state.copyWith(
          error: 'Failed to parse job update: ${e.toString()}',
        );
      }
    });
  }

  void _startPolling() {
    _pollTimer?.cancel();
    _pollTimer = Timer.periodic(const Duration(seconds: 15), (_) {
      _fetchJobs();
    });
  }

  Future<void> refresh() async {
    state = state.copyWith(isLoading: true);
    await _fetchJobs();
  }

  @override
  void dispose() {
    _connectionSubscription?.cancel();
    _connectionSubscription = null;
    _jobsSubscription?.cancel();
    _jobsSubscription = null;
    _pollTimer?.cancel();
    _pollTimer = null;
    super.dispose();
  }
}

/// Job queue provider
final jobProvider =
    StateNotifierProvider<JobNotifier, JobState>((ref) {
  final client = ref.watch(apiClientProvider);
  final websocket = ref.watch(websocketProvider);
  return JobNotifier(apiClient: client, websocket: websocket);
});
