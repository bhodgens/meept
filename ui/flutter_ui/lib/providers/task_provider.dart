import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'async_state.dart';
import 'providers.dart';

/// StateNotifier that manages task loading
class TaskNotifier extends StateNotifier<AsyncState<List<Task>>> {
  TaskNotifier({required this.apiClient})
      : super(const AsyncState.initial());

  final ApiClient apiClient;

  /// Fetch all tasks for the active session from the server
  Future<void> loadTasks() async {
    state = const AsyncState.loading();
    try {
      final tasks = await apiClient.listTasks();
      state = AsyncState.data(tasks);
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  /// Create a new task
  Future<Task?> createTask({
    required String title,
    String? sessionId,
  }) async {
    final currentTasks = state.whenOrNull(data: (t) => t) ?? [];
    state = const AsyncState.loading();
    try {
      final task = await apiClient.createTask(
        title: title,
        sessionId: sessionId,
      );
      state = AsyncState.data([...currentTasks, task]);
      return task;
    } catch (e, st) {
      state = AsyncState.error(e, st);
      return null;
    }
  }

  /// Update the state of a single task and refresh the list
  Future<bool?> updateTaskStatus(String id, String newStatus) async {
    state = const AsyncState.loading();
    try {
      await apiClient.updateTask(id, state: newStatus);
      // Refresh the full list from server
      await loadTasks();
      return true;
    } catch (e, st) {
      state = AsyncState.error(e, st);
      return false;
    }
  }

  /// Cancel a task and refresh the list
  Future<bool> cancelTask(String id) async {
    state = const AsyncState.loading();
    try {
      await apiClient.cancelTask(id);
      await loadTasks();
      return true;
    } catch (e, st) {
      state = AsyncState.error(e, st);
      return false;
    }
  }
}

/// Task state provider
final taskProvider =
    StateNotifierProvider<TaskNotifier, AsyncState<List<Task>>>((ref) {
  final client = ref.watch(apiClientProvider);
  return TaskNotifier(apiClient: client);
});
