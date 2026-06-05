import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'async_state.dart';
import 'providers.dart';
import '../services/websocket_service.dart';

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
class JobNotifier extends StateNotifier<AsyncState<List<Job>>> {
  JobNotifier({
    required this.apiClient,
    required this.websocket,
  }) : super(const AsyncState.loading()) {
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
        _pollTimer?.cancel();
        _pollTimer = null;
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
      state = AsyncState.data(jobs);
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  void _subscribeToJobs() {
    if (_jobsSubscription != null) return;

    _jobsSubscription = websocket.subscribeToJobs().listen((msg) {
      try {
        final update = JobUpdate.fromJson(msg);

        final currentJobs = state.whenOrNull(data: (j) => j) ?? [];

        // Update existing job or prepend new one
        final existingIndex =
            currentJobs.indexWhere((j) => j.id == update.jobId);

        List<Job> updatedJobs;
        if (existingIndex >= 0) {
          updatedJobs = [...currentJobs];
          updatedJobs[existingIndex] = currentJobs[existingIndex].copyWith(
            status: update.status,
          );
        } else {
          // Create a minimal Job from the update for display purposes
          updatedJobs = [
            Job(
              id: update.jobId,
              type: update.type,
              status: update.status,
              agentId: update.agentId,
              createdAt: update.timestamp,
            ),
            ...currentJobs,
          ];
        }

        // Keep max 50
        if (updatedJobs.length > 50) {
          updatedJobs = updatedJobs.sublist(0, 50);
        }

        state = AsyncState.data(updatedJobs);
      } catch (e, st) {
        state = AsyncState.error(e, st);
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
    state = const AsyncState.loading();
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
    StateNotifierProvider<JobNotifier, AsyncState<List<Job>>>((ref) {
  final client = ref.watch(apiClientProvider);
  final websocket = ref.watch(websocketProvider);
  return JobNotifier(apiClient: client, websocket: websocket);
});
