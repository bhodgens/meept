import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';

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

  void _showCreateSessionDialog() async {
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
                Navigator.pop(context);
                // Auto-switch to new session and chat tab
                if (session != null && context.mounted) {
                  ref.read(activeSessionProvider.notifier).state = session;
                  ref.read(selectedTabIndexProvider.notifier).state = 0;
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

  void _showDeleteConfirmation(String sessionId, String title) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text('delete session?', style: CyberpunkTypography.headlineMedium),
        content: Text('"$title"', style: CyberpunkTypography.bodyMedium),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
                backgroundColor: CyberpunkColors.redAlert),
            onPressed: () {
              ref
                  .read(sessionProvider.notifier)
                  .deleteSession(sessionId);
              Navigator.pop(context);
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
    return InkWell(
      onTap: () => ref.read(activeSessionProvider.notifier).state = session,
      onDoubleTap: () {
        ref.read(activeSessionProvider.notifier).state = session;
        ref.read(selectedTabIndexProvider.notifier).state = 0;
      },
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
                    _formatLastActivity(session.lastActivity ?? session.createdAt),
                    style: CyberpunkTypography.bodySmall,
                  ),
                ],
              ),
            ),
            IconButton(
              icon: const Icon(Icons.delete_outline, size: 16),
              color: CyberpunkColors.orangeDark,
              onPressed: () =>
                  _showDeleteConfirmation(session.id, session.title),
            ),
          ],
        ),
      ),
    );
  }

  String _formatLastActivity(DateTime date) {
    final now = DateTime.now();
    final diff = now.difference(date);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes}m ago';
    if (diff.inDays < 1) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
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
