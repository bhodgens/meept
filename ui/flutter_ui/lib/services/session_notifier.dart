import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import 'sdk_client.dart';

const _unset = Object();

/// Session state tracked by SessionNotifier
class SessionState {
  final List<Session> sessions;
  final bool isLoading;
  final String? error;

  const SessionState({
    this.sessions = const [],
    this.isLoading = false,
    this.error,
  });

  SessionState copyWith({
    List<Session>? sessions,
    bool? isLoading,
    Object? error = _unset,
  }) {
    return SessionState(
      sessions: sessions ?? this.sessions,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

/// StateNotifier that manages session CRUD operations
class SessionNotifier extends StateNotifier<SessionState> {
  SessionNotifier({required this.sdkClient})
      : super(const SessionState());

  final SdkApiClient sdkClient;

  /// Fetch all sessions from the server
  Future<void> loadSessions() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      // SdkApiClient.listSessions returns the raw `sessions` array;
      // callers deserialize each entry via Session.fromJson because
      // the OpenAPI spec leaves the Session entity untyped.
      final rawSessions = await sdkClient.listSessions();
      final sessions =
          rawSessions.map(Session.fromJson).toList(growable: false);
      state = state.copyWith(sessions: sessions, isLoading: false);
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }

  /// Create a new session with the given title
  Future<Session?> createSession(String title) async {
    try {
      final raw = await sdkClient.createSession(title: title);
      final session = Session.fromJson(raw);
      // Reload sessions from server to ensure we have the persisted list
      state = state.copyWith(isLoading: true, error: null);
      final rawSessions = await sdkClient.listSessions();
      final sessions =
          rawSessions.map(Session.fromJson).toList(growable: false);
      state = state.copyWith(sessions: sessions, isLoading: false);
      return session;
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
      return null;
    }
  }

  /// Delete a session by its ID
  Future<void> deleteSession(String id) async {
    try {
      await sdkClient.deleteSession(id);
      state = state.copyWith(
        sessions: state.sessions.where((s) => s.id != id).toList(),
      );
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }

  /// Archive a session. Mutates local state to flip the flag and re-sorts
  /// the list so archived sessions move to the bottom.
  Future<void> archiveSession(String sessionId) async {
    try {
      await sdkClient.archiveSession(sessionId, archived: true);
      state = _withArchiveFlag(state, sessionId, archived: true);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  /// Unarchive a session. Mutates local state to flip the flag and re-sorts.
  Future<void> unarchiveSession(String sessionId) async {
    try {
      await sdkClient.archiveSession(sessionId, archived: false);
      state = _withArchiveFlag(state, sessionId, archived: false);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }
}

/// Returns a new SessionState with the given session's archived flag
/// flipped, and the list re-sorted (non-archived first, then by
/// lastActivity descending — mirroring the backend's ORDER BY).
SessionState _withArchiveFlag(
  SessionState current,
  String sessionId, {
  required bool archived,
}) {
  final updated = current.sessions.map((s) {
    if (s.id == sessionId) return s.copyWith(archived: archived);
    return s;
  }).toList();
  updated.sort(_sessionSort);
  return current.copyWith(sessions: updated);
}

/// Comparator: non-archived first, then by lastActivity descending
/// (falls back to createdAt when lastActivity is null).
int _sessionSort(Session a, Session b) {
  if (a.archived != b.archived) {
    return a.archived ? 1 : -1;
  }
  final aTime = a.lastActivity ?? a.createdAt;
  final bTime = b.lastActivity ?? b.createdAt;
  return bTime.compareTo(aTime);
}
