import 'package:flutter/material.dart';
import 'home_screen.dart';
import '../chat/chat_tab.dart';
import '../sessions/sessions_overview_tab.dart';
import '../tasks/tasks_tab.dart';
import '../agents/agents_tab.dart';

/// Tab content router - displays the selected tab's content
class TabContent extends StatelessWidget {
  final HomeTab selectedTab;

  const TabContent({
    super.key,
    required this.selectedTab,
  });

  @override
  Widget build(BuildContext context) {
    switch (selectedTab) {
      case HomeTab.chat:
        return const ChatTab(sessionId: 'default');
      case HomeTab.sessions:
        return const SessionsOverviewTab();
      case HomeTab.tasks:
        return const TasksTab();
      case HomeTab.agents:
        return const AgentsTab();
    }
  }
}
