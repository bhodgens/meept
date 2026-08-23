import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../providers/status_message_provider.dart';
import 'home_screen.dart';
import '../chat/chat_tab.dart';
import '../sessions/sessions_overview_tab.dart';
import '../plans/plans_tab.dart';
import '../tasks/tasks_tab.dart';
import '../agents/agents_tab.dart';
import '../../models/api_models.dart';

/// Tab content router - displays the selected tab's content
class TabContent extends ConsumerWidget {
  final HomeTab selectedTab;
  final Session? activeSession;

  const TabContent({super.key, required this.selectedTab, this.activeSession});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    switch (selectedTab) {
      case HomeTab.chat:
        // When no session is active yet (e.g. before auto-create completes
        // or if it failed), render a placeholder instead of fabricating a
        // 'default' session ID that would 404 on every API call.
        final session = activeSession;
        if (session == null) {
          return const _NoSessionPlaceholder();
        }
        return ChatTab(sessionId: session.id);
      case HomeTab.sessions:
        return const SessionsOverviewTab();
      case HomeTab.plans:
        return const PlansTab();
      case HomeTab.tasks:
        return const TasksTab();
      case HomeTab.agents:
        return const AgentsTab();
    }
  }
}

/// Rendered in the chat tab when no session is active.  Avoids fabricating
/// a `'default'` session ID just to give [ChatTab] a non-null string, which
/// would otherwise cause noise from `GET /sessions/default/messages` 404s.
///
/// The placeholder is itself the entry point for session creation from the
/// chat tab: tapping the action text calls [SessionNotifier.createSession]
/// directly (mirroring `SessionsList._createQuickSession`) so the user never
/// has to leave the chat tab to recover from "no session".
class _NoSessionPlaceholder extends ConsumerStatefulWidget {
  const _NoSessionPlaceholder();

  @override
  ConsumerState<_NoSessionPlaceholder> createState() =>
      _NoSessionPlaceholderState();
}

class _NoSessionPlaceholderState extends ConsumerState<_NoSessionPlaceholder> {
  bool _creating = false;

  Future<void> _createSession() async {
    if (_creating) return;
    setState(() => _creating = true);
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession('new session');
    if (!mounted) {
      // Widget disposed while the RPC was in flight; nothing to update.
      return;
    }
    if (session == null) {
      setState(() => _creating = false);
      final error =
          ref.read(sessionProvider).error ?? 'failed to create session';
      showStatusMessage(ref, 'create failed: $error');
      return;
    }
    ref.read(activeSessionProvider.notifier).state = session;
    // No setState needed: setting activeSessionProvider re-renders
    // TabContent, which swaps this placeholder out for ChatTab.
  }

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            'no session',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
          const SizedBox(height: 8),
          GestureDetector(
            onTap: _creating ? null : _createSession,
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (_creating)
                  const Padding(
                    padding: EdgeInsets.only(right: 6),
                    child: SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        valueColor: AlwaysStoppedAnimation<Color>(
                          CyberpunkColors.orangePrimary,
                        ),
                      ),
                    ),
                  ),
                Text(
                  _creating ? 'creating...' : 'tap to create one',
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    decoration: TextDecoration.underline,
                    decorationColor: CyberpunkColors.orangePrimary,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
