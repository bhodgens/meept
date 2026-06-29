import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../core/router.dart';
import '../../core/shortcuts.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import '../../theme/typography.dart';
import '../../widgets/command_palette.dart';
import '../../widgets/status_bar.dart';
import '../../widgets/tab_bar.dart';
import '../../providers/providers.dart';
import '../../providers/project_provider.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/tab_activation_provider.dart';
import '../../providers/verbosity_provider.dart';
import 'tab_content.dart';
import 'tools_dropdown.dart';

/// Dialog showing connection details (host, port, cert, uptime, version).
class _ConnectionDetailsDialog extends ConsumerWidget {
  final ConnectionDetails? details;

  const _ConnectionDetailsDialog({required this.details});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rows = details?.dialogRows ?? [];

    return AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: Text(
        'connection details',
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: rows.map((row) {
            final value = details != null ? details!.rowValue(row.label) : row.value;
            return Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  SizedBox(
                    width: 90,
                    child: Text(
                      '${row.label}:',
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.midGray,
                        fontFamily: 'SourceCodePro',
                      ),
                    ),
                  ),
                  Expanded(
                    child: GestureDetector(
                      onLongPress: () {
                        Clipboard.setData(ClipboardData(text: value));
                      },
                      child: Text(
                        value,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: CyberpunkColors.lightGray,
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            );
          }).toList(),
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
    );
  }
}

/// Home tab enum - 5 tabs
enum HomeTab { chat, sessions, plans, tasks, agents }

/// Connection status dot - small indicator in toolbar.
/// Tapping opens a popup menu with details, disconnect/reconnect actions.
class _ConnectionDot extends ConsumerWidget {
  const _ConnectionDot();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);
    final isConnecting = ref.watch(isConnectingProvider);
    final statusText = ref.watch(connectionStatusProvider);
    final statusColor = ref.watch(connectionColorProvider);
    final details = ref.watch(connectionDetailsProvider);
    final summary = details?.summary;

    final items = <PopupMenuEntry<String>>[
      if (summary != null)
        PopupMenuItem<String>(
          enabled: false,
          child: Text(
            summary,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
        ),
      if (summary != null) const PopupMenuDivider(),
      if (connected)
        const PopupMenuItem<String>(
          value: 'details',
          child: Text('details'),
        ),
      if (connected)
        const PopupMenuItem<String>(
          value: 'disconnect',
          child: Text('disconnect'),
        ),
      if (!connected)
        const PopupMenuItem<String>(
          value: 'reconnect',
          child: Text('reconnect'),
        ),
    ];

    return PopupMenuButton<String>(
      onSelected: (value) {
        if (value == 'details') {
          final details = ref.read(connectionDetailsProvider);
          showDialog(
            context: context,
            builder: (_) => _ConnectionDetailsDialog(details: details),
          );
        } else if (value == 'disconnect') {
          ref.read(websocketProvider).disconnect();
        } else if (value == 'reconnect') {
          ref.read(websocketProvider).connect();
        }
      },
      itemBuilder: (context) => items,
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
    _leaderController.onFocusInput = () {
      if (_selectedTab != HomeTab.chat) {
        setState(() => _selectedTab = HomeTab.chat);
      }
      ref.read(focusInputRequestProvider.notifier).state = true;
    };
    _leaderController.onFind = () {
      context.goToolSearch();
    };
    _leaderController.onInSessionFind = () {
      if (_selectedTab != HomeTab.chat) {
        setState(() => _selectedTab = HomeTab.chat);
      }
      final session = ref.read(activeSessionProvider);
      final sid = session?.id ?? 'default';
      ref.read(findBarVisibleProvider(sid).notifier).state = true;
    };
    _leaderController.onGlobalSearch = () {
      // Single `f` key shortcut fires only when on the sessions tab.
      if (_selectedTab == HomeTab.sessions) {
        context.goToolSearch();
      }
    };
    _leaderController.onBranches = () {
      context.goToolBranches();
    };
    _leaderController.onShowCommandPalette = _showCommandPalette;
    _leaderController.onCycleVerbosity = _cycleVerbosity;
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
      // Best-effort refresh of the active-project indicator. The
      // notifier swallows errors and degrades to CurrentProject.empty.
      ref.read(currentProjectProvider.notifier).refresh();
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
      case 'reflection':
        context.goToolReflection();
      case 'prompts':
        context.goToolPrompts();
      case 'settings':
        context.goSettings();
      // Other tools (files, terminal, calendar, metrics) don't have
      // dedicated routes yet — they stay on the chat tab with the
      // activeTool provider handling the panel switch.
    }
  }

  /// Returns true if [toolName] has a dedicated full-screen route.
  bool _hasRoute(String toolName) {
    const routedTools = {'search', 'branches', 'skills', 'memory', 'reflection', 'prompts', 'settings'};
    return routedTools.contains(toolName);
  }

  /// Open the command palette modal. Replaces the former leader-key
  /// two-stage input. Items mirror the TUI modal.go command list.
  void _showCommandPalette() {
    showDialog(
      context: context,
      builder: (_) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'command palette',
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        contentPadding: const EdgeInsets.symmetric(vertical: 8),
        content: SizedBox(
          width: 480,
          child: CommandPalette(
            items: CommandPalette.defaultItems,
            onSelected: (item) {
              Navigator.of(context).pop();
              _handlePaletteSelection(item);
            },
          ),
        ),
      ),
    );
  }

  /// Dispatch a palette selection to the appropriate action. Tab
  /// switches route through [_onLeaderTabSelected] for DRY (it handles
  /// both setState and router sync). The find/projects items reuse the
  /// existing shortcut callbacks.
  void _handlePaletteSelection(CommandPaletteItem item) {
    switch (item.label) {
      case 'chat':
        _onLeaderTabSelected(HomeTab.chat.index);
        break;
      case 'sessions':
        _onLeaderTabSelected(HomeTab.sessions.index);
        break;
      case 'plans':
        _onLeaderTabSelected(HomeTab.plans.index);
        break;
      case 'tasks':
        _onLeaderTabSelected(HomeTab.tasks.index);
        break;
      case 'agents':
        _onLeaderTabSelected(HomeTab.agents.index);
        break;
      case 'find…':
        _leaderController.onFind?.call();
        break;
      case 'new session':
        _onLeaderTabSelected(HomeTab.sessions.index);
        // TODO: trigger new-session flow once session tab exposes it
        break;
      case 'edit description':
        _onLeaderTabSelected(HomeTab.sessions.index);
        // TODO: trigger edit-description flow once session tab exposes it
        break;
      case 'projects':
        _leaderController.onBranches?.call();
        break;
    }
  }

  /// Cycle the verbosity level (Ctrl+V, all platforms, TUI parity).
  /// UI state only — backend persistence is tracked as task #23
  /// (deferred: PATCH /api/v1/config/client merge-patch route).
  void _cycleVerbosity() {
    ref.read(verbosityProvider.notifier).cycle();
    final level = ref.read(verbosityProvider);
    showStatusMessage(ref, 'verbosity: ${VerbosityLevel.name(level)}');
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
              _buildHelpRow('cmd+x / ctrl+x', 'command palette'),
              _buildHelpRow('ctrl+v', 'cycle verbosity'),
              _buildHelpRow('cmd+k / ctrl+k', 'focus input (/)'),
              _buildHelpRow('cmd+f / ctrl+f', 'find in session'),
              _buildHelpRow('f', 'global search (sessions tab)'),
              _buildHelpRow('esc', 'close / dismiss / blur'),
              const SizedBox(height: 8),
              Text(
                'ctrl = cmd on mac; ctrl+v matches TUI on all platforms',
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

    // Child widgets request tab switches via tabActivationProvider.
    // Apply the switch and clear the request back to null.
    ref.listen<HomeTab?>(tabActivationProvider, (prev, next) {
      if (next != null && next != _selectedTab) {
        setState(() => _selectedTab = next);
      }
      if (next != null) {
        ref.read(tabActivationProvider.notifier).state = null;
      }
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
            child: Column(
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
                // Status bar (TUI parity)
                StatusBar(selectedTabIndex: _selectedTab.index),
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
