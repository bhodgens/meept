import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../core/router.dart';
import '../../core/shortcuts.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import '../../theme/typography.dart';
import '../../widgets/tab_bar.dart';
import '../../providers/providers.dart';
import 'tab_content.dart';
import 'tools_dropdown.dart';

/// Home tab enum - 5 tabs
enum HomeTab { chat, sessions, plans, tasks, agents }

/// Connection status dot - small indicator in toolbar.
/// Tapping opens a popup menu with disconnect/reconnect actions.
class _ConnectionDot extends ConsumerWidget {
  const _ConnectionDot();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);
    final isConnecting = ref.watch(isConnectingProvider);
    final statusText = ref.watch(connectionStatusProvider);
    final statusColor = ref.watch(connectionColorProvider);

    return PopupMenuButton<String>(
      onSelected: (value) {
        if (value == 'disconnect') {
          ref.read(websocketProvider).disconnect();
        } else if (value == 'reconnect') {
          ref.read(websocketProvider).connect();
        }
      },
      itemBuilder: (context) => connected
          ? const [
              PopupMenuItem<String>(
                value: 'disconnect',
                child: Text('disconnect'),
              ),
            ]
          : const [
              PopupMenuItem<String>(
                value: 'reconnect',
                child: Text('reconnect'),
              ),
            ],
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: isConnecting
                  ? CyberpunkColors.orangePrimary
                  : connected
                      ? CyberpunkColors.greenSuccess
                      : CyberpunkColors.redAlert,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 6),
          Text(
            statusText,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: statusColor == 'green'
                  ? CyberpunkColors.greenSuccess
                  : statusColor == 'orange'
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
              fontSize: 10,
            ),
          ),
        ],
      ),
    );
  }
}

/// Home screen - main app screen with top tab navigation and toolbar
class HomeScreen extends ConsumerStatefulWidget {
  const HomeScreen({super.key});

  @override
  ConsumerState<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends ConsumerState<HomeScreen> {
  HomeTab _selectedTab = HomeTab.chat;

  final List<String> _tabLabels = ['chat', 'sessions', 'plans', 'tasks', 'agents'];

  /// Route paths corresponding to each tab index.
  static const List<String> _tabRoutes = ['/', '/sessions', '/plans', '/tasks', '/agents'];

  bool _initialLoadDone = false;
  late final LeaderKeyController _leaderController;

  @override
  void initState() {
    super.initState();
    _leaderController = LeaderKeyController();
    _leaderController.onTabSelected = _onLeaderTabSelected;
    _leaderController.onNavigate = _onLeaderNavigate;
    _leaderController.onShowHelp = _showHelpDialog;
    _leaderController.onFocusInput = ({slashPrefix = false}) {
      if (_selectedTab != HomeTab.chat) {
        setState(() => _selectedTab = HomeTab.chat);
      }
      ref.read(focusInputRequestProvider.notifier).state = true;
    };
    _leaderController.onFind = () {
      context.goToolSearch();
    };
    _leaderController.onBranches = () {
      context.goToolBranches();
    };
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(chatProvider);
      _onConnectionChanged(ref.read(connectionStateProvider));
      // Apply the router-forced initial tab if present.
      final override = TabOverrideScope.of(context);
      if (override != null && override != HomeTab.chat) {
        setState(() => _selectedTab = override);
      }
    });
  }

  @override
  void dispose() {
    _leaderController.dispose();
    super.dispose();
  }

  /// Handle leader key tab selection — switch to the tab locally and
  /// update the router so the URL stays in sync.
  void _onLeaderTabSelected(int index) {
    if (index >= 0 && index < HomeTab.values.length) {
      setState(() => _selectedTab = HomeTab.values[index]);
      context.go(_tabRoutes[index]);
    }
  }

  /// Handle leader key navigation via go_router.
  void _onLeaderNavigate(String path) {
    context.go(path);
  }

  void _onConnectionChanged(bool connected) {
    if (connected && !_initialLoadDone) {
      _initialLoadDone = true;
      ref.read(sessionProvider.notifier).loadSessions();
      ref.read(taskProvider.notifier).loadTasks();
      ref.read(agentProvider.notifier).loadAgents();
    }
  }

  /// Navigate to a tool panel via go_router if it has a registered route.
  void _navigateTool(String toolName) {
    switch (toolName) {
      case 'search':
        context.goToolSearch();
      case 'branches':
        context.goToolBranches();
      case 'skills':
        context.goToolSkills();
      case 'memory':
        context.goToolMemory();
      case 'settings':
        context.goSettings();
      // Other tools (files, terminal, calendar, metrics) don't have
      // dedicated routes yet — they stay on the chat tab with the
      // activeTool provider handling the panel switch.
    }
  }

  /// Returns true if [toolName] has a dedicated full-screen route.
  bool _hasRoute(String toolName) {
    const routedTools = {'search', 'branches', 'skills', 'memory', 'settings'};
    return routedTools.contains(toolName);
  }

  void _showHelpDialog() {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'keyboard shortcuts',
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _buildHelpRow('leader s', 'sessions tab'),
              _buildHelpRow('leader c', 'chat tab'),
              _buildHelpRow('leader p', 'find / search'),
              _buildHelpRow('leader b', 'branches'),
              _buildHelpRow('leader ?', 'this help'),
              _buildHelpRow('cmd+k', 'focus input (/)'),
              _buildHelpRow('esc', 'close / dismiss / blur'),
              const SizedBox(height: 8),
              Text(
                'leader = cmd+x (mac) / ctrl+x (linux/win)',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                  fontSize: 10,
                ),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: Text(
              'close',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildHelpRow(String key, String description) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        children: [
          SizedBox(
            width: 100,
            child: Text(
              key,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangePrimary,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
          Expanded(
            child: Text(
              description,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    ref.listen<bool>(connectionStateProvider, (prev, connected) {
      _onConnectionChanged(connected);
    });

    return AppShortcuts(
      controller: _leaderController,
      child: Scaffold(
        backgroundColor: CyberpunkColors.black,
        body: Container(
          decoration: const BoxDecoration(
            gradient: CyberpunkEffects.angularGradient,
          ),
          child: SafeArea(
            child: Stack(
              children: [
                Column(
                  children: [
                    // Top tab bar
                    OrangeVoidTabBar(
                      tabs: _tabLabels,
                      selectedIndex: _selectedTab.index,
                      onTabSelected: (index) {
                        setState(() => _selectedTab = HomeTab.values[index]);
                        context.go(_tabRoutes[index]);
                      },
                    ),
                    // Toolbar with tools dropdown + connection indicator
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
                      color: CyberpunkColors.blackTransparent(0.7),
                      child: Row(
                        children: [
                          const Spacer(),
                          ToolsDropdown(
                            onToolSelected: (route) {
                              if (_hasRoute(route)) {
                                // Full-screen route — don't set activeTool
                                // to avoid orphaned state (bug F7).
                                _navigateTool(route);
                              } else {
                                ref.read(activeToolProvider.notifier).state = route;
                                if (_selectedTab != HomeTab.chat) {
                                  setState(() => _selectedTab = HomeTab.chat);
                                }
                              }
                            },
                          ),
                          const SizedBox(width: 12),
                          const _ConnectionDot(),
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
                // Leader key waiting indicator
                if (_leaderController.isWaiting)
                  Positioned(
                    top: 80,
                    left: 16,
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                      decoration: BoxDecoration(
                        color: CyberpunkColors.orangePrimary.withValues(alpha: 0.9),
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text(
                        'leader key — waiting...',
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: CyberpunkColors.black,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildTabContent() {
    final activeSession = ref.watch(activeSessionProvider);
    return TabContent(
      selectedTab: _selectedTab,
      activeSession: activeSession,
    );
  }
}
