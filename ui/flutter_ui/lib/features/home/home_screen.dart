import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../core/router.dart';
import '../../core/shortcuts.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/command_palette.dart';
import '../../widgets/status_bar.dart';
import '../../widgets/tab_bar.dart';
import '../../providers/providers.dart';
import '../../providers/session_detail.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/tab_activation_provider.dart';
import '../../providers/verbosity_provider.dart';
import 'tab_content.dart';
import 'tools_dropdown.dart' show HamburgerMenu;

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
            final value = details != null
                ? details!.rowValue(row.label)
                : row.value;
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
        const PopupMenuItem<String>(value: 'details', child: Text('details')),
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
          ref.read(websocketProvider).pause();
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

  final List<String> _tabLabels = [
    'chat',
    'sessions',
    'plans',
    'tasks',
    'agents',
  ];

  /// Route paths corresponding to each tab index.
  static const List<String> _tabRoutes = [
    '/',
    '/sessions',
    '/plans',
    '/tasks',
    '/agents',
  ];

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
    _leaderController.onToggleSteer = _toggleSteerMode;
    _leaderController.onToggleTts = () async {
      await ref.read(ttsProvider.notifier).toggleTts();
      showStatusMessage(
        ref,
        'tts ${ref.read(ttsProvider.notifier).enabled ? 'on' : 'off'}',
      );
    };
    // TUI Ctrl+P opens the fuzzy finder over sessions/tasks; the Flutter
    // equivalent is the search tool panel.
    _leaderController.onFuzzyFinder = () => context.goToolSearch();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      unawaited(_onConnectionChanged(ref.read(connectionStateProvider)));
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

  /// Refresh all data providers from the daemon.
  ///
  /// Called on every successful connection (initial connect AND reconnect)
  /// so the UI always reflects current server state without user intervention.
  Future<void> _refreshAllData() async {
    await ref.read(sessionProvider.notifier).loadSessions();
    ref.read(taskProvider.notifier).loadTasks();
    ref.read(agentProvider.notifier).loadAgents();
    // Best-effort refresh of the active-project indicator. The
    // notifier swallows errors and degrades to CurrentProject.empty.
    ref.read(currentProjectProvider.notifier).refresh();
  }

  Future<void> _onConnectionChanged(bool connected) async {
    if (!connected) return;

    // Always refresh data on every (re)connect so the UI reflects
    // current server state without user intervention.
    await _refreshAllData();

    // Auto-select a session if none is active. This guarantees a chat
    // target on first launch and mirrors the TUI's "always have a
    // session" behaviour. Skip when a previous run already selected one
    // (e.g. reconnect after a transient network drop).
    //
    // Prefer reusing an existing empty session over creating a new one so
    // relaunching the UI doesn't accumulate unused "new session" entries.
    if (!_initialLoadDone) {
      _initialLoadDone = true;
      final active = ref.read(activeSessionProvider);
      if (active == null) {
        final notifier = ref.read(sessionProvider.notifier);
        final session =
            notifier.findReusableEmptySession() ??
            await notifier.createSession('new session');
        if (session != null) {
          ref.read(activeSessionProvider.notifier).state = session;
        }
      }
    }
  }

  /// Edit the active session's description via PATCH /sessions/{id}.
  void _showEditDescriptionDialog() {
    final session = ref.read(activeSessionProvider);
    if (session == null) {
      showStatusMessage(ref, 'no active session');
      return;
    }
    final controller = TextEditingController(text: session.description ?? '');
    showDialog(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'edit description',
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        content: TextField(
          controller: controller,
          autofocus: true,
          style: CyberpunkTypography.bodyMedium,
          decoration: InputDecoration(
            hintText: 'session description',
            hintStyle: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.midGray,
            ),
            filled: true,
            fillColor: CyberpunkColors.black,
            border: const OutlineInputBorder(
              borderSide: BorderSide(color: CyberpunkColors.orangeDark),
            ),
          ),
          maxLines: 3,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('cancel', style: CyberpunkTypography.bodySmall),
          ),
          TextButton(
            onPressed: () async {
              final description = controller.text.trim();
              Navigator.of(dialogContext).pop();
              try {
                await ref
                    .read(sdkClientProvider)
                    .updateSessionDescription(session.id, description);
                ref.invalidate(sessionDetailFamily(session.id));
                showStatusMessage(ref, 'description updated');
              } catch (e) {
                showStatusMessage(ref, 'update failed: $e');
              }
            },
            child: Text(
              'save',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
        ],
      ),
    );
  }

  /// Ctrl+S (TUI parity): when the agent is processing, arm/disarm steer
  /// mode for the next message; when idle, jump to the sessions tab —
  /// exactly the dual behaviour of TUI app.go ctrl+s.
  void _toggleSteerMode() {
    final session = ref.read(activeSessionProvider);
    final sid = session?.id;
    if (sid == null) {
      showStatusMessage(ref, 'no active session');
      return;
    }
    final chat = ref.read(chatProvider(sid).notifier);
    final state = ref.read(chatProvider(sid));
    if (state.isAgentProcessing || state.isLoading) {
      final armed = chat.toggleSteerMode();
      showStatusMessage(ref, 'steer mode: ${armed ? 'on' : 'off'}');
    } else {
      _onLeaderTabSelected(HomeTab.sessions.index);
    }
  }

  /// Navigate to a tool panel via go_router if it has a registered route.
  void _navigateTool(String toolName) {
    switch (toolName) {
      case 'search':
        context.goToolSearch();
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
    const routedTools = {
      'search',
      'branches',
      'skills',
      'memory',
      'reflection',
      'prompts',
      'settings',
    };
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
        ref.read(createSessionRequestProvider.notifier).state = true;
        break;
      case 'edit description':
        _showEditDescriptionDialog();
        break;
      case 'projects':
        _leaderController.onBranches?.call();
        break;
    }
  }

  /// Cycle the verbosity level (Ctrl+V, all platforms, TUI parity).
  /// State cycles immediately; persistence is fire-and-forget via
  /// VerbosityNotifier.onPersist → PATCH /api/v1/config/client.
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
      unawaited(_onConnectionChanged(connected));
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
        body: SafeArea(
          child: Column(
            children: [
              // Top tab bar
              OrangeVoidTabBar(
                tabs: _tabLabels,
                selectedIndex: _selectedTab.index,
                onTabSelected: (index) {
                  setState(() => _selectedTab = HomeTab.values[index]);
                  context.go(_tabRoutes[index]);
                  // Refresh active-project indicator on tab switch so a
                  // project change made on the projects panel propagates
                  // to the status bar without a reconnect (TUI parity).
                  ref.read(currentProjectProvider.notifier).refresh();
                },
              ),
              // Toolbar with hamburger menu (left) + connection indicator (right)
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 4,
                ),
                color: CyberpunkColors.blackTransparent(0.7),
                child: Row(
                  children: [
                    HamburgerMenu(
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
                    const Spacer(),
                  ],
                ),
              ),
              const Divider(height: 1, color: CyberpunkColors.midGray),
              // Main content area
              Expanded(child: _buildTabContent()),
              // Status bar (TUI parity)
              StatusBar(selectedTabIndex: _selectedTab.index),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTabContent() {
    final activeSession = ref.watch(activeSessionProvider);
    return TabContent(selectedTab: _selectedTab, activeSession: activeSession);
  }
}
