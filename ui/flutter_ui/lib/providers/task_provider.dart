import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import 'providers.dart';

const _unset = Object();

/// State tracked by TaskNotifier
class TaskState {
  final List<Task> tasks;
  final bool isLoading;
  final String? error;

  const TaskState({
    this.tasks = const [],
    this.isLoading = false,
    this.error,
  });

  TaskState copyWith({
    List<Task>? tasks,
    bool? isLoading,
    Object? error = _unset,
  }) {
    return TaskState(
      tasks: tasks ?? this.tasks,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

/// StateNotifier that manages task loading
class TaskNotifier extends StateNotifier<TaskState> {
  TaskNotifier({required this.sdkClient})
      : super(const TaskState());

  final SdkApiClient sdkClient;

  /// Fetch all tasks for the active session from the server
  Future<void> loadTasks() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      // SdkApiClient.listTasks returns the raw `tasks` array — callers
      // deserialize each entry via Task.fromJson because the OpenAPI spec
      // leaves the Task entity untyped.
      final rawTasks = await sdkClient.listTasks();
      final tasks = rawTasks.map((t) => Task.fromJson(t)).toList(growable: false);
      state = state.copyWith(tasks: tasks, isLoading: false);
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }

  /// Create a new task
  Future<Task?> createTask({
    required String title,
    String? sessionId,
  }) async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final raw = await sdkClient.createTask(
        title: title,
        sessionId: sessionId,
      );
      final task = Task.fromJson(raw);
      state = state.copyWith(tasks: [...state.tasks, task], isLoading: false);
      return task;
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
      return null;
    }
  }

  /// Update the state of a single task and refresh the list
  Future<bool?> updateTaskStatus(String id, String newStatus) async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      await sdkClient.updateTask(id, state: newStatus);
      // Refresh the full list from server
      await loadTasks();
      return true;
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
      return false;
    }
  }

  /// Cancel a task and refresh the list
  Future<bool> cancelTask(String id) async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      await sdkClient.cancelTask(id);
      await loadTasks();
      return true;
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
      return false;
    }
  }
}

/// Task state provider
final taskProvider =
    StateNotifierProvider<TaskNotifier, TaskState>((ref) {
  final client = ref.watch(sdkClientProvider);
  return TaskNotifier(sdkClient: client);
});
