import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import '../../theme/typography.dart';
import '../../widgets/tab_bar.dart';
import '../../providers/providers.dart';
import 'tab_content.dart';

/// Home tab enum - 5 tabs
enum HomeTab { chat, sessions, plans, tasks, agents }

/// Connection status indicator - visible pill showing daemon connectivity
class ConnectionStatusIndicator extends ConsumerWidget {
  const ConnectionStatusIndicator({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: (connected
                ? CyberpunkColors.greenSuccess
                : CyberpunkColors.redAlert)
            .withValues(alpha: 0.15),
        border: Border.all(
          color: connected
              ? CyberpunkColors.greenSuccess
              : CyberpunkColors.redAlert,
          width: 1,
        ),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(
              color: connected
                  ? CyberpunkColors.greenSuccess
                  : CyberpunkColors.redAlert,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 6),
          Text(
            connected ? 'connected' : 'disconnected',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: connected
                  ? CyberpunkColors.greenSuccess
                  : CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ],
      ),
    );
  }
}

/// Home screen - main app screen with top tab navigation
class HomeScreen extends ConsumerStatefulWidget {
  const HomeScreen({super.key});

  @override
  ConsumerState<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends ConsumerState<HomeScreen> {
  HomeTab _selectedTab = HomeTab.chat;

  final List<String> _tabLabels = ['chat', 'sessions', 'plans', 'tasks', 'agents'];

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
              // Connection status bar
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
                color: CyberpunkColors.blackTransparent(0.7),
                child: const Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    ConnectionStatusIndicator(),
                  ],
                ),
              ),
              const Divider(height: 1, color: CyberpunkColors.midGray),
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
    // Watch active session to pass to chat tab
    final activeSession = ref.watch(activeSessionProvider);
    return TabContent(
      selectedTab: _selectedTab,
      activeSession: activeSession,
    );
  }
}
