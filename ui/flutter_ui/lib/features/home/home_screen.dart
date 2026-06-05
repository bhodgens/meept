import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/shortcuts.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import '../../theme/typography.dart';
import '../../widgets/tab_bar.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';
import '../drawer/drawer_overlay.dart';
import 'tab_content.dart';
import 'tools_dropdown.dart';

/// Home tab enum - 5 tabs
enum HomeTab { chat, sessions, plans, tasks, agents }

/// Home screen - main app screen with top tab navigation and toolbar
class HomeScreen extends ConsumerStatefulWidget {
  const HomeScreen({super.key});

  @override
  ConsumerState<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends ConsumerState<HomeScreen> {
  final List<String> _tabLabels = ['chat', 'sessions', 'plans', 'tasks', 'agents'];

  bool _initialLoadDone = false;
  late final LeaderKeyController _leaderController;

  @override
  void initState() {
    super.initState();
    _leaderController = LeaderKeyController();
    _leaderController.onTabSelected = (index) {
      if (index >= 0 && index < HomeTab.values.length) {
        ref.read(selectedTabIndexProvider.notifier).state = index;
      }
    };
    _leaderController.onToggleDrawer = () {
      final isOpen = ref.read(drawerOpenProvider);
      ref.read(drawerOpenProvider.notifier).state = !isOpen;
    };
    _leaderController.onShowHelp = _showHelpDialog;
    _leaderController.onFocusInput = () {
      ref.read(selectedTabIndexProvider.notifier).state = 0;
      ref.read(focusInputSlashPrefixProvider.notifier).state =
          _leaderController.slashPrefix;
      ref.read(focusInputRequestProvider.notifier).state = true;
    };
    _leaderController.onFind = () {
      _showSnack('search: not yet implemented');
    };
    _leaderController.onBranches = () {
      _showSnack('branches: not yet implemented');
    };
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(chatProvider);
      _onConnectionChanged(ref.read(connectionStateProvider));
    });
  }

  @override
  void dispose() {
    _leaderController.dispose();
    super.dispose();
  }

  void _onConnectionChanged(bool connected) {
    if (connected && !_initialLoadDone) {
      _initialLoadDone = true;
      ref.read(sessionProvider.notifier).loadSessions();
      ref.read(taskProvider.notifier).loadTasks();
      ref.read(agentProvider.notifier).loadAgents();
      // Default to 'default' session on startup
      final active = ref.read(activeSessionProvider);
      if (active == null) {
        ref.read(activeSessionProvider.notifier).state = Session(
          id: 'default',
          title: 'default',
          createdAt: DateTime.now(),
          lastActivity: DateTime.now(),
        );
      }
    }
  }

  void _showSnack(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          message,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.orangePrimary,
            fontFamily: 'SourceCodePro',
          ),
        ),
        backgroundColor: CyberpunkColors.darkGray,
        duration: const Duration(seconds: 2),
      ),
    );
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
              _buildHelpRow('leader d', 'toggle drawer'),
              _buildHelpRow('leader p', 'find / search'),
              _buildHelpRow('leader b', 'branches'),
              _buildHelpRow('leader ?', 'this help'),
              _buildHelpRow('ctrl+k / cmd+k', 'focus input (/)'),
              _buildHelpRow('esc', 'close / dismiss / blur'),
              const SizedBox(height: 8),
              Text(
                'leader = ctrl+x or cmd+x',
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
    final drawerOpen = ref.watch(drawerOpenProvider);
    final selectedTabIndex = ref.watch(selectedTabIndexProvider);
    final selectedTab = HomeTab.values[selectedTabIndex.clamp(0, HomeTab.values.length - 1)];

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
                      selectedIndex: selectedTabIndex,
                      onTabSelected: (index) =>
                          ref.read(selectedTabIndexProvider.notifier).state = index,
                    ),
                    // Toolbar with tools dropdown, session name, connection indicator
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
                      decoration: BoxDecoration(
                        color: CyberpunkColors.orangePrimary,
                        border: const Border(
                          bottom: BorderSide(color: CyberpunkColors.orangeDark, width: 1),
                        ),
                      ),
                      child: Row(
                        children: [
                          // Tools dropdown
                          ToolsDropdown(
                            onToolSelected: (route) {
                              ref.read(activeToolProvider.notifier).state = route;
                              // Switch to chat tab to show the tool
                              ref.read(selectedTabIndexProvider.notifier).state = 0;
                            },
                          ),
                          const SizedBox(width: 16),
                          // Session name in toolbar
                          Consumer(
                            builder: (context, ref, child) {
                              final session = ref.watch(activeSessionProvider);
                              final displayName = session?.title ?? 'meept';
                              return Text(
                                displayName.toLowerCase(),
                                style: CyberpunkTypography.bodyMedium.copyWith(
                                  color: CyberpunkColors.black,
                                  fontFamily: 'SourceCodePro',
                                  fontWeight: FontWeight.bold,
                                  fontSize: 13,
                                ),
                              );
                            },
                          ),
                          const Spacer(),
                          // Connection dot (light colors for orange bar)
                          Consumer(
                            builder: (context, ref, child) {
                              final connected = ref.watch(connectionStateProvider);
                              return Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Container(
                                    width: 8,
                                    height: 8,
                                    decoration: BoxDecoration(
                                      color: connected
                                          ? CyberpunkColors.black
                                          : CyberpunkColors.redAlert,
                                      shape: BoxShape.circle,
                                    ),
                                  ),
                                  const SizedBox(width: 6),
                                  Text(
                                    connected ? 'connected' : 'disconnected',
                                    style: CyberpunkTypography.bodySmall.copyWith(
                                      color: connected
                                          ? CyberpunkColors.black
                                          : CyberpunkColors.redAlert,
                                      fontFamily: 'SourceCodePro',
                                      fontSize: 10,
                                      fontWeight: FontWeight.bold,
                                    ),
                                  ),
                                ],
                              );
                            },
                          ),
                        ],
                      ),
                    ),
                    const Divider(height: 1, color: CyberpunkColors.midGray),
                    // Main content area
                    Expanded(
                      child: TabContent(
                        selectedTab: selectedTab,
                        activeSession: ref.watch(activeSessionProvider),
                      ),
                    ),
                  ],
                ),
                // Drawer overlay
                if (drawerOpen) const DrawerOverlay(),
                // Leader key waiting indicator with shortcut list
                ListenableBuilder(
                  listenable: _leaderController,
                  builder: (context, _) {
                    if (!_leaderController.isWaiting) {
                      return const SizedBox.shrink();
                    }
                    return Positioned(
                      top: 80,
                      left: 16,
                      child: Container(
                        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                        decoration: BoxDecoration(
                          color: CyberpunkColors.darkGray.withValues(alpha: 0.95),
                          border: Border.all(color: CyberpunkColors.orangePrimary, width: 1),
                          borderRadius: BorderRadius.circular(6),
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(
                              'ctrl+x — leader key',
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: CyberpunkColors.orangePrimary,
                                fontWeight: FontWeight.bold,
                                fontSize: 11,
                              ),
                            ),
                            const SizedBox(height: 8),
                            _buildHelpRow('s', 'sessions'),
                            _buildHelpRow('c', 'chat'),
                            _buildHelpRow('d', 'drawer'),
                            _buildHelpRow('p', 'find'),
                            _buildHelpRow('b', 'branches'),
                            _buildHelpRow('?', 'more help'),
                            const SizedBox(height: 4),
                            Text(
                              'esc to cancel',
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: CyberpunkColors.midGray,
                                fontSize: 10,
                              ),
                            ),
                          ],
                        ),
                      ),
                    );
                  },
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
