import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:timeago/timeago.dart' as timeago;
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../features/home/home_screen.dart' show HomeTab;
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import '../../providers/session_project_provider.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/tab_activation_provider.dart';

/// Sessions list widget - displays all sessions with selection
class SessionsList extends ConsumerStatefulWidget {
  const SessionsList({super.key});

  @override
  ConsumerState<SessionsList> createState() => _SessionsListState();
}

class _SessionsListState extends ConsumerState<SessionsList> {
  int _selectedIndex = 0;
  final FocusNode _focusNode = FocusNode();

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

  @override
  void dispose() {
    _focusNode.dispose();
    super.dispose();
  }

  /// Handle keyboard navigation for the sessions list.
  KeyEventResult _handleKey(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent && node.hasFocus) {
      final sessionState = ref.read(sessionProvider);
      final maxIndex = sessionState.sessions.length - 1;
      if (maxIndex < 0) return KeyEventResult.ignored;

      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        setState(() {
          _selectedIndex = (_selectedIndex + 1).clamp(0, maxIndex);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        setState(() {
          _selectedIndex = (_selectedIndex - 1).clamp(0, maxIndex);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.enter) {
        final sessions = sessionState.sessions;
        if (_selectedIndex >= 0 && _selectedIndex < sessions.length) {
          _activateSession(context, sessions[_selectedIndex]);
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
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession('new session');
    if (session == null) {
      // Notifier sets state.error on failure; surface it as a status.
      final error = ref.read(sessionProvider).error ?? 'failed to create session';
      if (mounted) {
        showStatusMessage(ref, 'create failed: $error');
      }
      return;
    }
    ref.read(activeSessionProvider.notifier).state = session;
    ref.read(tabActivationProvider.notifier).state = HomeTab.chat;
    if (mounted) context.go('/');
  }

  /// Activate [session] as the active session, prompting for project
  /// binding if the session has no project context.
  void _activateSession(BuildContext context, Session session) {
    if (!SessionProjectChecker.needsProjectPrompt(session)) {
      // Already bound — fast path.
      _doActivateSession(session);
      return;
    }

    SessionProjectChecker.checkAndPrompt(
      context: context,
      ref: ref,
      session: session,
      onSkip: () {
        _doActivateSession(session);
      },
      onProjectBound: (cwd) {
        // Project will be bound by the daemon on next RPC; for now
        // just activate the session and let the project context
        // resolve on the next refresh.
        _doActivateSession(session);
      },
    );
  }

  /// Perform the actual session activation (navigate, clear messages).
  void _doActivateSession(Session session) {
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
            child: const Text(
              'archive',
              style: CyberpunkTypography.bodyMedium,
            ),
          ),
        ],
      ),
    );
  }

  void _showContextMenu(BuildContext context, Session session) {
    showMenu<String>(
      context: context,
      position: const RelativeRect.fromLTRB(0, 0, 0, 0),
      items: const [
        PopupMenuItem(value: 'delete', child: Text('delete permanently')),
      ],
    ).then((value) {
      if (value == 'delete') {
        _showDeleteConfirmation(session.id, session.title);
      }
    });
  }

  Future<void> _showDeleteConfirmation(String sessionId, String title) async {
    await showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text('delete permanently?', style: CyberpunkTypography.headlineMedium),
        content: Text('"$title"', style: CyberpunkTypography.bodyMedium),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
                backgroundColor: CyberpunkColors.redAlert),
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
                Text(
                  'sessions',
                  style: CyberpunkTypography.headlineMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                IconButton(
                  icon: const Icon(Icons.add, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: _createQuickSession,
                ),
              ],
            ),
          ),
          if (sessionState.isLoading)
            const Expanded(
              child: Center(
                child: CircularProgressIndicator(),
              ),
            )
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
                    onPressed: () => ref.read(sessionProvider.notifier).loadSessions(),
                    child: const Text('retry', style: CyberpunkTypography.bodySmall),
                  ),
                ],
              ),
            )
          else if (sessionState.sessions.isEmpty)
            const Expanded(
              child: Center(
                child: Text('no sessions'),
              ),
            )
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
                  itemCount: sessionState.sessions.length,
                  itemBuilder: (context, index) {
                    final session = sessionState.sessions[index];
                    final isSelected = activeSession?.id == session.id;
                    final isKeyboardSelected = _selectedIndex == index;
                    return _buildSessionTile(session, isSelected || isKeyboardSelected, isKeyboardSelected);
                  },
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildSessionTile(Session session, bool isSelected, bool isKeyboardSelected) {
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
              IconButton(
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
          const Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
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
