import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';

/// Provider for ApiClient -- kept local to avoid circular import
final apiClientProvider = Provider<ApiClient>((ref) => ApiClient());

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
    String? error,
  }) {
    return TaskState(
      tasks: tasks ?? this.tasks,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

/// StateNotifier that manages task loading
class TaskNotifier extends StateNotifier<TaskState> {
  TaskNotifier({required this.apiClient})
      : super(const TaskState());

  final ApiClient apiClient;

  /// Fetch all tasks for the active session from the server
  Future<void> loadTasks() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final tasks = await apiClient.listTasks();
      state = state.copyWith(tasks: tasks, isLoading: false);
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }
}

/// Task state provider
final taskProvider =
    StateNotifierProvider<TaskNotifier, TaskState>((ref) {
  final client = ref.watch(apiClientProvider);
  return TaskNotifier(apiClient: client);
});
