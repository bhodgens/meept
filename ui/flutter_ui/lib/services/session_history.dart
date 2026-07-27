/// Per-session chat input history for the Flutter GUI.
///
/// Mirrors the TUI's `sharedclient.SessionHistory` (internal/sharedclient/
/// session_history.go): each session keeps its own bounded list of previously
/// sent inputs, and the user can scroll back through them with the Up/Down
/// arrow keys. History is persisted to SharedPreferences via StorageService
/// so it survives app restarts.
library;

import 'storage_service.dart';

/// A single session's input history with a navigation cursor.
///
/// [entries] is ordered oldest → newest. [_cursor] tracks the position while
/// the user is browsing: -1 means "not browsing" (the live draft is shown),
/// and values ≥ 0 index into [entries] from the newest end (0 = most recent).
class _SessionHistory {
  final List<String> entries = [];
  int cursor = -1;
  String draft = '';
}

/// In-memory, per-session input history store with persistence.
///
/// Implemented as a static singleton so history survives [ChatInput] widget
/// rebuilds and session switches. Entries are persisted to SharedPreferences
/// via [StorageService] so they survive app restarts.
class SessionHistoryStore {
  SessionHistoryStore._();

  static final SessionHistoryStore instance = SessionHistoryStore._();

  /// Cap per session, matching the TUI's maxHistorySize.
  static const int _maxSize = 100;

  final Map<String, _SessionHistory> _histories = {};
  final StorageService _storage = StorageService.instance;

  _SessionHistory _for(String sessionId) {
    return _histories.putIfAbsent(sessionId, () {
      // Load persisted history on first access.
      final entries = _storage.getInputHistory(sessionId);
      final h = _SessionHistory();
      h.entries.addAll(entries);
      return h;
    });
  }

  /// Persist the current entries for a session.
  void _persist(String sessionId) {
    final h = _histories[sessionId];
    if (h != null) {
      _storage.setInputHistory(sessionId, h.entries);
    }
  }

  /// Record a sent input. Empty strings are ignored; consecutive duplicates
  /// are collapsed (matches the TUI's History.Add behaviour). Persists to
  /// SharedPreferences after each add.
  void add(String sessionId, String entry) {
    final text = entry.trim();
    if (text.isEmpty) return;
    final h = _for(sessionId);
    if (h.entries.isNotEmpty && h.entries.last == text) {
      h.cursor = -1;
      return;
    }
    h.entries.add(text);
    if (h.entries.length > _maxSize) {
      h.entries.removeAt(0);
    }
    h.cursor = -1;
    _persist(sessionId);
  }

  /// Step backward (Up arrow) toward older entries.
  ///
  /// On the first call, [currentDraft] is stashed so it can be restored when
  /// the user navigates back down past the newest entry. Returns the entry to
  /// display, or null if there is no history.
  String? previous(String sessionId, String currentDraft) {
    final h = _for(sessionId);
    if (h.entries.isEmpty) return null;
    if (h.cursor == -1) {
      h.draft = currentDraft;
      h.cursor = 0;
    } else if (h.cursor < h.entries.length - 1) {
      h.cursor++;
    }
    return h.entries[h.entries.length - 1 - h.cursor];
  }

  /// Step forward (Down arrow) toward newer entries.
  ///
  /// Returns the entry to display, or the stashed draft once the user moves
  /// past the newest entry (cursor returns to -1). Returns null when not
  /// currently browsing.
  String? next(String sessionId) {
    final h = _for(sessionId);
    if (h.cursor == -1) return null;
    h.cursor--;
    if (h.cursor < 0) {
      return h.draft;
    }
    return h.entries[h.entries.length - 1 - h.cursor];
  }

  /// Reset the navigation cursor for a session (e.g. on session switch or
  /// after Escape). Does not clear the stored entries.
  void resetCursor(String sessionId) {
    final h = _histories[sessionId];
    if (h != null) {
      h.cursor = -1;
      h.draft = '';
    }
  }

  /// Drop the in-memory cache for a session so the next access reloads from
  /// persistent storage. Intended for tests that verify the reload path; not
  /// needed in normal operation (the cache lives for the process lifetime).
  void evict(String sessionId) {
    _histories.remove(sessionId);
  }
}
