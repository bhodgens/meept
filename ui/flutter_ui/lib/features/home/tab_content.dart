import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
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

  const TabContent({
    super.key,
    required this.selectedTab,
    this.activeSession,
  });

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
class _NoSessionPlaceholder extends StatelessWidget {
  const _NoSessionPlaceholder();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Text(
        'no session — press + to create one',
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.midGray,
        ),
      ),
    );
  }
}
