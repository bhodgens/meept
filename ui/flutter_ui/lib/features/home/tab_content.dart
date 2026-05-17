import 'package:flutter/material.dart';
import 'home_screen.dart';
import '../sessions/sessions_overview_tab.dart';
import '../agents/agents_tab.dart';
import '../tasks/tasks_tab.dart';

class CyberpunkTabContent extends StatelessWidget {
  final HomeTab selectedTab;
  final bool isSidebarCollapsed;

  const CyberpunkTabContent({
    super.key,
    required this.selectedTab,
    required this.isSidebarCollapsed,
  });

  @override
  Widget build(BuildContext context) {
    switch (selectedTab) {
      case HomeTab.agents:
        return const AgentsTab();
      case HomeTab.tasks:
        return const TasksTab();
      case HomeTab.sessions:
        return const SessionsOverviewTab();
    }
  }
}
