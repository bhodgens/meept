import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
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
        // Use active session ID or 'default' if none selected
        final sessionId = activeSession?.id ?? 'default';
        return ChatTab(sessionId: sessionId);
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
