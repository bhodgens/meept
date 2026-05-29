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

  bool _initialLoadDone = false;

  @override
  void initState() {
    super.initState();
    // Initialize connection monitor (triggers WebSocket connect) and watch
    // for the first successful connection to load data.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      // Trigger WebSocket connection via chat provider
      ref.read(chatProvider);
      // Start listening for connection state changes
      _onConnectionChanged(ref.read(connectionStateProvider));
    });
  }

  void _onConnectionChanged(bool connected) {
    if (connected && !_initialLoadDone) {
      _initialLoadDone = true;
      ref.read(sessionProvider.notifier).loadSessions();
      ref.read(taskProvider.notifier).loadTasks();
      ref.read(agentProvider.notifier).loadAgents();
    }
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
    // Watch connection state to trigger initial load when daemon connects
    final connected = ref.watch(connectionStateProvider);
    _onConnectionChanged(connected);
    return TabContent(
      selectedTab: _selectedTab,
      activeSession: activeSession,
    );
  }
}
