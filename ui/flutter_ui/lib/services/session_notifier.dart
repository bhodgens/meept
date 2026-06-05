import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../providers/async_state.dart';
import 'api_client.dart';

/// StateNotifier that manages session CRUD operations
class SessionNotifier extends StateNotifier<AsyncState<List<Session>>> {
  SessionNotifier({required this.apiClient})
      : super(const AsyncState.initial());

  final ApiClient apiClient;

  /// Fetch all sessions from the server
  Future<void> loadSessions() async {
    state = const AsyncState.loading();
    try {
      final sessions = await apiClient.listSessions();
      state = AsyncState.data(sessions);
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  /// Create a new session with the given title
  Future<Session?> createSession(String title) async {
    final currentSessions = state.whenOrNull(data: (s) => s) ?? [];
    try {
      final session = await apiClient.createSession(title: title);
      state = AsyncState.data([...currentSessions, session]);
      return session;
    } catch (e, st) {
      state = AsyncState.error(e, st);
      return null;
    }
  }

  /// Delete a session by its ID
  Future<void> deleteSession(String id) async {
    final currentSessions = state.whenOrNull(data: (s) => s) ?? [];
    try {
      await apiClient.deleteSession(id);
      state = AsyncState.data(
        currentSessions.where((s) => s.id != id).toList(),
      );
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }
}
