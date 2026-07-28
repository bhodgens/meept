import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import 'sdk_client.dart';
import 'websocket_service.dart';

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
  SessionNotifier({required this.sdkClient, required WebSocketService websocket})
      : super(const SessionState()) {
    _initWebSocket(websocket);
  }

  final SdkApiClient sdkClient;
  StreamSubscription<Map<String, dynamic>>? _titleSubscription;

  /// Initialize WebSocket subscription for session title updates
  void _initWebSocket(WebSocketService websocket) {
    _titleSubscription = websocket.subscribeToSessionTitles().listen((message) {
      final sessionId = message['session_id'] as String?;
      // Backend sends 'name' but Session model uses 'title'
      final title = message['name'] as String?;
      final description = message['description'] as String?;
      if (sessionId != null && title != null && description != null) {
        _updateSessionTitle(sessionId, title, description);
      }
    });
  }

  /// Internal method to update session title (called from WebSocket listener)
  void _updateSessionTitle(String sessionId, String title, String description) {
    final updated = state.sessions.map((s) {
      if (s.id == sessionId) {
        return s.copyWith(title: title, description: description);
      }
      return s;
    }).toList();
    updated.sort(_sessionSort);
    state = state.copyWith(sessions: updated, error: null);
  }

  @override
  void dispose() {
    _titleSubscription?.cancel();
    _titleSubscription = null;
    super.dispose();
  }

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

  /// Find an existing empty session to reuse instead of creating a new one.
  ///
  /// An "empty" session has no messages, which the backend signals via a
  /// null `leaf_message_id`. Returns the most recent non-archived empty
  /// session (state.sessions is already sorted non-archived-first, then by
  /// lastActivity descending), or null when every session has content.
  ///
  /// Callers should prefer this over [createSession] on startup so relaunching
  /// the UI doesn't accumulate a pile of unused "new session" entries.
  Session? findReusableEmptySession() {
    for (final s in state.sessions) {
      if (s.archived) continue;
      if (s.leafMessageId == null) return s;
    }
    return null;
  }

  /// Create a new session with the given title.
  ///
  /// Inserts the newly created session into local state immediately so
  /// the caller gets it back without waiting for a list refresh. A
  /// background [loadSessions] is fired-and-forgotten via [unawaited]
  /// to reconcile with the server's persisted ordering; if that call
  /// hangs, the UI is unaffected because the Future is detached.
  ///
  /// When [cwd] is provided, the daemon resolves or registers a project
  /// at that path, binding the session to the user's actual repo instead
  /// of a synthetic default.
  Future<Session?> createSession(String title, {String? projectId, String? cwd}) async {
    try {
      final raw = await sdkClient.createSession(title: title, projectId: projectId, cwd: cwd);
      final session = Session.fromJson(raw);
      // Insert into local state right away — no server round-trip.
      final updated = [session, ...state.sessions];
      updateSessions(updated);
      // Refresh in the background to pick up server-side sort order or
      // server-defaulted fields. Deliberately unawaited: the caller has
      // already received the session and the UI must not hang if the
      // daemon's GET /api/v1/sessions stalls.
      unawaited(loadSessions());
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
        error: null,
      );
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }

  /// Replace the sessions list in state without a server round-trip.
  ///
  /// Used by callers that patch a single session field (e.g. the chat
  /// input auto-derives a title via `session.generate_description` and
  /// patches the matching entry locally). The daemon has already
  /// persisted the change by the time this is called, so a subsequent
  /// [loadSessions] would also reflect it — this method avoids the
  /// extra round-trip.
  void updateSessions(List<Session> sessions) {
    state = state.copyWith(sessions: sessions, error: null);
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
  return current.copyWith(sessions: updated, error: null);
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
