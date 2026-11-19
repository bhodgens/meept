import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:timeago/timeago.dart' as timeago;
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../dialogs/rename_session_dialog.dart' show showRenameSessionDialog;
import '../../core/router.dart';
import '../../features/home/home_screen.dart' show HomeTab;
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import '../../providers/session_project_provider.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/tab_activation_provider.dart';
import '../../providers/tool_exit_guard.dart';
import '../../services/session_notifier.dart' show SessionState;
import 'session_tree.dart';

/// Sessions list widget - displays all sessions with selection
class SessionsList extends ConsumerStatefulWidget {
  const SessionsList({super.key});

  @override
  ConsumerState<SessionsList> createState() => _SessionsListState();
}

class _SessionsListState extends ConsumerState<SessionsList> {
  /// Cursor into [_visibleRows] — the bucket-ordered list of rows the
  /// render actually shows (headers + children of expanded buckets).
  int _selectedIndex = 0;
  final FocusNode _focusNode = FocusNode();

  /// Buckets the user collapsed. ExpansionTile keeps its own expansion
  /// state; onExpansionChanged mirrors it here so keyboard navigation
  /// indexes the SAME visible-row order the render uses (M14 — the old
  /// flat-list indexing could activate a session hidden in a collapsed
  /// bucket). Mirrors the TUI's collapsed map: collapsed children are
  /// unreachable by arrows/enter.
  final Set<String> _collapsedBuckets = {};

  /// Per-bucket ExpansionTile controllers so a keyboard enter on a header
  /// drives the SAME widget the mouse toggles (one source of truth).
  final Map<String, ExpansibleController> _bucketControllers = {};

  ExpansibleController _bucketController(String bucket) =>
      _bucketControllers.putIfAbsent(bucket, ExpansibleController.new);

  @override
  void dispose() {
    _focusNode.dispose();
    for (final c in _bucketControllers.values) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(sessionProvider.notifier).loadSessions();
      // Request focus for keyboard navigation when the list is first shown
      _focusNode.requestFocus();
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // Re-request focus when dependencies change (e.g., when tab becomes active)
    if (!_focusNode.hasFocus) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && !_focusNode.hasFocus) {
          _focusNode.requestFocus();
        }
      });
    }
  }

  /// One navigable row of the sessions tree: a bucket header (session ==
  /// null) or a session child of an expanded bucket.
  ///
  /// Built from the SAME grouping + collapse state the render uses, in
  /// render order (bucket insertion order, sessions in list order inside
  /// each bucket).
  List<({String bucket, Session? session})> _visibleRows(
    SessionState sessionState,
  ) {
    final grouped = groupSessionsByBucket(sessionState.sessions);
    final rows = <({String bucket, Session? session})>[];
    for (final entry in grouped.entries) {
      rows.add((bucket: entry.key, session: null));
      if (_collapsedBuckets.contains(entry.key)) continue;
      for (final session in entry.value) {
        rows.add((bucket: entry.key, session: session));
      }
    }
    return rows;
  }

  /// Handle keyboard navigation for the sessions list.
  ///
  /// Navigates the VISIBLE rows (M14): arrows move across bucket headers
  /// and the children of expanded buckets only; enter on a header toggles
  /// its bucket (TUI parity — collapsed children are unreachable), enter
  /// on a session activates it.
  KeyEventResult _handleKey(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent && node.hasFocus) {
      final rows = _visibleRows(ref.read(sessionProvider));
      if (rows.isEmpty) return KeyEventResult.ignored;
      final maxIndex = rows.length - 1;
      final index = _selectedIndex.clamp(0, maxIndex);

      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        setState(() {
          _selectedIndex = (index + 1).clamp(0, maxIndex);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        setState(() {
          _selectedIndex = (index - 1).clamp(0, maxIndex);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.enter) {
        final row = rows[index];
        final session = row.session;
        if (session == null) {
          // Header row: toggle the bucket through its ExpansionTile
          // controller — onExpansionChanged mirrors the state into
          // _collapsedBuckets and fixes up the cursor.
          final controller = _bucketController(row.bucket);
          if (_collapsedBuckets.contains(row.bucket)) {
            controller.expand();
          } else {
            controller.collapse();
          }
        } else {
          _activateSession(context, session);
        }
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  /// Quick-create a new session and switch to the chat tab.
  ///
  /// Replaces the former title-prompt dialog (`_showCreateSessionDialog`)
  /// because session titles are now auto-derived from the first user
  /// message (see `_maybeGenerateSessionDescription` in chat_input.dart).
  /// The session is created with a placeholder title "new session" and
  /// the LLM summariser rewrites it after the first exchange.
  ///
  /// Parity: mirrors the TUI's "always have a session" behaviour.
  Future<void> _createQuickSession() async {
    // Leaving the sessions tab for chat replaces any tool panel embedded in
    // the chat tab, so the switch asks the open panel's exit guard first -
    // the same question the tab bar and the hamburger menu ask. A refusal
    // changes nothing: no session is created and the route stays put.
    if (!await _maySwitchToChat()) return;
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession('new session');
    if (session == null) {
      // Notifier sets state.error on failure; surface it as a status.
      final error =
          ref.read(sessionProvider).error ?? 'failed to create session';
      if (mounted) {
        showStatusMessage(ref, 'create failed: $error');
      }
      return;
    }
    ref.read(activeSessionProvider.notifier).state = session;
    ref.read(tabActivationProvider.notifier).state = HomeTab.chat;
    if (mounted) context.go('/');
  }

  /// Whether the chat tab may be entered.
  ///
  /// Between them, the session list's two switches to chat change providers,
  /// the tab and the route; all of that is a tool panel's exit if one is open,
  /// so the open panel's guard answers first (and releases its registration
  /// when it allows the exit).
  Future<bool> _maySwitchToChat() =>
      ref.read(toolExitGuardProvider).requestExit();

  /// Activate [session] as the active session, prompting for project
  /// binding if the session has no project context.
  Future<void> _activateSession(BuildContext context, Session session) async {
    if (!SessionProjectChecker.needsProjectPrompt(session)) {
      // Already bound — fast path.
      await _doActivateSession(session);
      return;
    }

    SessionProjectChecker.checkAndPrompt(
      context: context,
      ref: ref,
      session: session,
      onSkip: () {
        unawaited(_doActivateSession(session));
      },
      onProjectBound: (cwd) async {
        // Project will be bound by the daemon on next RPC; for now
        // just activate the session and let the project context
        // resolve on the next refresh.
        await _doActivateSession(session);
      },
    );
  }

  /// Perform the actual session activation (navigate, clear messages).
  Future<void> _doActivateSession(Session session) async {
    // Activation leaves the sessions tab for chat, dropping any tool panel
    // embedded there, so the guard answers before anything changes.
    if (!await _maySwitchToChat()) return;
    if (!mounted) return;
    ref.read(activeSessionProvider.notifier).state = session;
    ref.read(tabActivationProvider.notifier).state = HomeTab.chat;
    context.go('/');
  }

  Future<void> _showArchiveConfirmation(String sessionId, String title) async {
    await showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'archive session?',
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        content: Text(
          '"${title.toLowerCase()}"',
          style: CyberpunkTypography.bodyMedium,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: Text(
              'cancel',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ),
          FilledButton(
            onPressed: () async {
              final notifier = ref.read(sessionProvider.notifier);
              await notifier.archiveSession(sessionId);
              // Reflect outcome: notifier sets state.error on failure, clears
              // it on success. Show status AFTER the async resolves so we
              // never report success prematurely (parity with TUI).
              final error = ref.read(sessionProvider).error;
              if (error == null) {
                showStatusMessage(ref, 'archived: ${title.toLowerCase()}');
                if (context.mounted) Navigator.pop(context);
              } else {
                showStatusMessage(ref, 'archive failed: $error');
                // Keep dialog open so user can retry / see the failure.
              }
            },
            child: const Text('archive', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }

  /// Rename dialog: mirrors the TUI rename modal (default key 'r').
  /// Submits through [SessionNotifier.renameSession], which hits the same
  /// PATCH /api/v1/sessions/{id} description endpoint the TUI's
  /// renameSession RPC path uses.
  Future<void> _showRenameDialog(String sessionId, String currentTitle) async {
    final value = await showRenameSessionDialog(
      context,
      currentTitle: currentTitle,
    );
    if (value == null || value.isEmpty) return;
    if (!mounted) return;
    await ref.read(sessionProvider.notifier).renameSession(sessionId, value);
    final error = ref.read(sessionProvider).error;
    if (!mounted) return;
    if (error == null) {
      showStatusMessage(ref, 'renamed: ${value.toLowerCase()}');
    } else {
      showStatusMessage(ref, 'rename failed: $error');
    }
  }

  /// Unarchive via the notifier (archive toggled off) — same status-message
  /// feedback pattern as archive/delete.
  Future<void> _unarchiveSession(String sessionId, String title) async {
    final notifier = ref.read(sessionProvider.notifier);
    await notifier.unarchiveSession(sessionId);
    final error = ref.read(sessionProvider).error;
    if (!mounted) return;
    if (error == null) {
      showStatusMessage(ref, 'unarchived: ${title.toLowerCase()}');
    } else {
      showStatusMessage(ref, 'unarchive failed: $error');
    }
  }

  void _showContextMenu(BuildContext context, Session session) {
    showMenu<String>(
      context: context,
      position: const RelativeRect.fromLTRB(0, 0, 0, 0),
      items: [
        const PopupMenuItem(value: 'rename', child: Text('rename')),
        PopupMenuItem(
          value: session.archived ? 'unarchive' : 'archive',
          child: Text(session.archived ? 'unarchive' : 'archive'),
        ),
        const PopupMenuItem(value: 'delete', child: Text('delete permanently')),
      ],
    ).then((value) {
      switch (value) {
        case 'rename':
          _showRenameDialog(session.id, session.title);
          break;
        case 'archive':
          _showArchiveConfirmation(session.id, session.title);
          break;
        case 'unarchive':
          _unarchiveSession(session.id, session.title);
          break;
        case 'delete':
          _showDeleteConfirmation(session.id, session.title);
          break;
      }
    });
  }

  Future<void> _showDeleteConfirmation(String sessionId, String title) async {
    await showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'delete permanently?',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: Text('"$title"', style: CyberpunkTypography.bodyMedium),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: CyberpunkColors.redAlert,
            ),
            onPressed: () async {
              final activeSession = ref.read(activeSessionProvider);
              final isActive = activeSession?.id == sessionId;
              final notifier = ref.read(sessionProvider.notifier);
              await notifier.deleteSession(sessionId);
              // Reflect outcome: notifier sets state.error on failure. Show
              // status AFTER the async resolves — parity with TUI delete
              // (which uses SessionDeletedMsg to set status from RPC result).
              final error = ref.read(sessionProvider).error;
              if (error == null) {
                showStatusMessage(ref, 'deleted: ${title.toLowerCase()}');
                if (isActive) {
                  ref.read(activeSessionProvider.notifier).state = null;
                }
                if (context.mounted) Navigator.pop(context);
              } else {
                showStatusMessage(ref, 'delete failed: $error');
                // Keep dialog open so user can see the failure.
              }
            },
            child: const Text('delete', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final sessionState = ref.watch(sessionProvider);
    final activeSession = ref.watch(activeSessionProvider);

    // C2: group sessions into project buckets for the expandable tree.
    // Keyboard navigation indexes the SAME bucket/collapse state via
    // _visibleRows (M14), not the flat session list.
    final groupedSessions = groupSessionsByBucket(sessionState.sessions);
    final bucketOrder = groupedSessions.keys.toList();
    final visibleRows = _visibleRows(sessionState);
    if (_selectedIndex >= visibleRows.length) {
      _selectedIndex = visibleRows.isEmpty ? 0 : visibleRows.length - 1;
    }

    // Listen for command-palette "new session" requests. Mirrors the
    // focusInputRequestProvider pattern used by the chat input.
    ref.listen<bool>(createSessionRequestProvider, (previous, next) {
      if (next) {
        _createQuickSession();
        ref.read(createSessionRequestProvider.notifier).state = false;
      }
    });

    return Container(
      width: 280,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Flexible(
                  child: Text(
                    'sessions',
                    overflow: TextOverflow.ellipsis,
                    style: CyberpunkTypography.headlineMedium.copyWith(
                      color: CyberpunkColors.orangePrimary,
                    ),
                  ),
                ),
                const Spacer(),
                IconButton(
                  tooltip: 'search sessions',
                  icon: const Icon(Icons.search, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: () =>
                      context.goToolSearchScoped(SearchScope.sessions),
                ),
                IconButton(
                  tooltip: 'new session',
                  icon: const Icon(Icons.add, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: _createQuickSession,
                ),
              ],
            ),
          ),
          if (sessionState.isLoading)
            const Expanded(child: Center(child: CircularProgressIndicator()))
          else if (sessionState.error != null)
            Expanded(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  SizedBox(
                    width: double.infinity,
                    child: _SessionErrorBanner(message: sessionState.error!),
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () =>
                        ref.read(sessionProvider.notifier).loadSessions(),
                    child: const Text(
                      'retry',
                      style: CyberpunkTypography.bodySmall,
                    ),
                  ),
                ],
              ),
            )
          else if (sessionState.sessions.isEmpty)
            const Expanded(child: Center(child: Text('no sessions')))
          else
            Expanded(
              child: Focus(
                focusNode: _focusNode,
                onFocusChange: (hasFocus) {
                  if (hasFocus) {
                    ref.read(keyboardFocusProvider.notifier).setFocusedPane(0);
                  }
                },
                onKeyEvent: _handleKey,
                child: ListView.builder(
                  itemCount: bucketOrder.length,
                  itemBuilder: (context, groupIndex) {
                    final bucket = bucketOrder[groupIndex];
                    return _buildBucketGroup(
                      bucket,
                      groupedSessions[bucket]!,
                      visibleRows,
                      activeSession,
                    );
                  },
                ),
              ),
            ),
        ],
      ),
    );
  }

  /// Build one C2 project bucket as an expandable group. Headers render the
  /// bucket name (project id, project path, or 'unbound') in lowercase;
  /// children are the existing session tiles indented under it. Groups start
  /// expanded; `ExpansionTile` owns its own expand/collapse state, keyed by
  /// the stable bucket key so it survives rebuilds — onExpansionChanged
  /// mirrors that state into [_collapsedBuckets] for keyboard nav (M14).
  Widget _buildBucketGroup(
    String bucket,
    List<Session> bucketSessions,
    List<({String bucket, Session? session})> visibleRows,
    Session? activeSession,
  ) {
    final collapsed = _collapsedBuckets.contains(bucket);
    final headerIndex = visibleRows.indexWhere(
      (r) => r.bucket == bucket && r.session == null,
    );
    final isHeaderKeyboardSelected =
        headerIndex >= 0 && _selectedIndex == headerIndex;
    return ExpansionTile(
      key: ValueKey('session-bucket-$bucket'),
      controller: _bucketController(bucket),
      initiallyExpanded: !collapsed,
      onExpansionChanged: (expanded) {
        setState(() {
          if (expanded) {
            _collapsedBuckets.remove(bucket);
          } else {
            _collapsedBuckets.add(bucket);
          }
          // If the cursor sat on a child that just became hidden, move it
          // to this bucket's header (TUI parity: the cursor returns to
          // the header after a collapse). Otherwise keep the cursor.
          final rows = _visibleRows(ref.read(sessionProvider));
          final cursor = _selectedIndex.clamp(0, rows.length - 1);
          final row = rows.isEmpty ? null : rows[cursor];
          if (row == null ||
              (!expanded && row.session != null && row.bucket == bucket)) {
            final h = rows.indexWhere(
              (r) => r.bucket == bucket && r.session == null,
            );
            if (h >= 0) _selectedIndex = h;
          } else {
            _selectedIndex = cursor;
          }
        });
      },
      tilePadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 2),
      iconColor: CyberpunkColors.orangePrimary,
      collapsedIconColor: CyberpunkColors.orangeDark,
      backgroundColor: isHeaderKeyboardSelected
          ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
          : null,
      collapsedBackgroundColor: isHeaderKeyboardSelected
          ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
          : null,
      title: Text(
        bucket.toLowerCase(),
        key: ValueKey('session-bucket-label-$bucket'),
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.orangePrimary,
          fontWeight: FontWeight.bold,
        ),
      ),
      subtitle: Text(
        '${bucketSessions.length}',
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.midGray,
        ),
      ),
      children: [
        for (final session in bucketSessions)
          Padding(
            padding: const EdgeInsets.only(left: 16),
            child: _buildSessionTile(
              session,
              activeSession?.id == session.id ||
                  _isKeyboardRow(visibleRows, session.id),
              _isKeyboardRow(visibleRows, session.id),
            ),
          ),
      ],
    );
  }

  bool _isKeyboardRow(
    List<({String bucket, Session? session})> visibleRows,
    String sessionId,
  ) {
    if (_selectedIndex < 0 || _selectedIndex >= visibleRows.length) {
      return false;
    }
    return visibleRows[_selectedIndex].session?.id == sessionId;
  }

  Widget _buildSessionTile(
    Session session,
    bool isSelected,
    bool isKeyboardSelected,
  ) {
    return Opacity(
      opacity: session.archived ? 0.5 : 1.0,
      child: InkWell(
        key: ValueKey('session-tile-${session.id}'),
        onTap: () => _activateSession(context, session),
        onDoubleTap: () {
          // Activate unconditionally: Flutter suppresses onTap when a
          // double-tap is recognized, so we cannot assume activation
          // already happened (fast double-click on an unbound session
          // would otherwise navigate to chat with no active session).
          _activateSession(context, session);
        },
        onLongPress: () => _showContextMenu(context, session),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          decoration: BoxDecoration(
            color: isSelected
                ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
                : null,
            border: Border(
              left: BorderSide(
                color: isSelected
                    ? CyberpunkColors.orangePrimary
                    : Colors.transparent,
                width: 2,
              ),
            ),
          ),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      session.title.toLowerCase(),
                      style: CyberpunkTypography.bodyMedium.copyWith(
                        color: isSelected
                            ? CyberpunkColors.orangePrimary
                            : CyberpunkColors.greenSuccess,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      timeago.format(session.lastActivity ?? session.createdAt),
                      style: CyberpunkTypography.bodySmall,
                    ),
                  ],
                ),
              ),
              if (session.archived)
                IconButton(
                  tooltip: 'unarchive session',
                  icon: const Icon(Icons.unarchive, size: 16),
                  color: CyberpunkColors.orangeDark,
                  onPressed: () => _unarchiveSession(session.id, session.title),
                )
              else
                IconButton(
                  tooltip: 'archive session',
                  icon: const Icon(Icons.archive_outlined, size: 16),
                  color: CyberpunkColors.orangeDark,
                  onPressed: () =>
                      _showArchiveConfirmation(session.id, session.title),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Inline error banner for session list errors
class _SessionErrorBanner extends StatelessWidget {
  final String message;

  const _SessionErrorBanner({required this.message});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
      child: Row(
        children: [
          Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}
