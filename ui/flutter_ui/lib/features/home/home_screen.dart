import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import '../../widgets/tab_bar.dart';
import '../../providers/providers.dart';
import 'tab_content.dart';

/// Home tab enum - 4 tabs as specified
enum HomeTab { chat, sessions, tasks, agents }

/// Home screen - main app screen with top tab navigation
class HomeScreen extends ConsumerStatefulWidget {
  const HomeScreen({super.key});

  @override
  ConsumerState<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends ConsumerState<HomeScreen> {
  HomeTab _selectedTab = HomeTab.chat;

  final List<String> _tabLabels = ['chat', 'sessions', 'tasks', 'agents'];

  @override
  void initState() {
    super.initState();
    // Initialize all providers and start WebSocket on app start
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(sessionProvider.notifier).loadSessions();
      ref.read(taskProvider.notifier).loadTasks();
      ref.read(agentProvider.notifier).loadAgents();
      // Initialize chat provider (triggers WebSocket connect + subscription)
      ref.read(chatProvider);
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: CyberpunkColors.black,
      body: Container(
        decoration: const BoxDecoration(
          gradient: CyberpunkEffects.angularGradient,
        ),
        child: SafeArea(
          child: Column(
            children: [
              // Top tab bar
              OrangeVoidTabBar(
                tabs: _tabLabels,
                selectedIndex: _selectedTab.index,
                onTabSelected: (index) =>
                    setState(() => _selectedTab = HomeTab.values[index]),
              ),
              // Main content area
              Expanded(
                child: _buildTabContent(),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTabContent() {
    return TabContent(
      selectedTab: _selectedTab,
    );
  }
}
