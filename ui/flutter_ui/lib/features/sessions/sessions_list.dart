import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:timeago/timeago.dart' as timeago;
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../features/home/home_screen.dart' show HomeTab;
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/tab_activation_provider.dart';

/// Sessions list widget - displays all sessions with selection
class SessionsList extends ConsumerStatefulWidget {
  const SessionsList({super.key});

  @override
  ConsumerState<SessionsList> createState() => _SessionsListState();
}

class _SessionsListState extends ConsumerState<SessionsList> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(sessionProvider.notifier).loadSessions();
    });
  }

  Future<void> _showCreateSessionDialog() async {
    final controller = TextEditingController();
    final notifier = ref.read(sessionProvider.notifier);
    await showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text('create session', style: CyberpunkTypography.headlineMedium),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: 'enter session title...',
            hintStyle: CyberpunkTypography.bodySmall,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            onPressed: () async {
              if (controller.text.isNotEmpty) {
                final session = await notifier.createSession(controller.text);
                if (session != null) {
                  // Refresh the list so the new session appears immediately (Issue 6).
                  await notifier.loadSessions();
                  if (!context.mounted) return;
                  Navigator.pop(context);
                  ref.read(activeSessionProvider.notifier).state = session;
                  context.go('/');
                } else {
                  // Error: don't pop, show feedback (bug F4).
                  if (context.mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      const SnackBar(
                        content: Text('failed to create session'),
                        backgroundColor: CyberpunkColors.redAlert,
                      ),
                    );
                  }
                }
              }
            },
            child: const Text('create', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
    controller.dispose();
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
                  onPressed: _showCreateSessionDialog,
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
              child: ListView.builder(
                itemCount: sessionState.sessions.length,
                itemBuilder: (context, index) {
                  final session = sessionState.sessions[index];
                  final isSelected = activeSession?.id == session.id;
                  return _buildSessionTile(session, isSelected);
                },
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildSessionTile(Session session, bool isSelected) {
    return Opacity(
      opacity: session.archived ? 0.5 : 1.0,
      child: InkWell(
        key: ValueKey('session-tile-${session.id}'),
        onTap: () => ref.read(activeSessionProvider.notifier).state = session,
        onDoubleTap: () {
          ref.read(activeSessionProvider.notifier).state = session;
          ref.read(tabActivationProvider.notifier).state = HomeTab.chat;
          context.go('/');
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
