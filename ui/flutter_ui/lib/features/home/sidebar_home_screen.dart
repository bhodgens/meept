/// Sidebar-based home screen layout
///
/// Alternative to the traditional top-tab navigation, featuring:
/// - Left sidebar with session tree (expandable/collapsible)
/// - Chat view always visible in main content area
/// - Session info overlay accessible via [i] icon
///
/// Structure:
/// ```
/// +--------------+------------------------------------------+
/// |  SESSIONS    |  Header: Session Title                   |
/// |  ──────────  +──────────────────────────────────────────+
/// |  [+] sess1   |                                          |
/// |  ├─ task1    │           Chat Message Area              |
/// |  │ └─ plan1  │                                          |
/// |  ▼ sess2     │                                          |
/// |  ├─ plan1    │                                          |
/// |  │ └─ task1  ├──────────────────────────────────────────+
/// |  │ └─ task2  │  [ Chat Input Area ]                     |
/// +──────────────+──────────────────────────────────────────+
/// |  [Status Bar]                                           |
/// +----------------------------------------------------------+
/// ```

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
import '../../widgets/background_image.dart';
import '../../widgets/status_bar.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/tab_activation_provider.dart';
import '../../providers/verbosity_provider.dart';
import '../chat/chat_tab.dart';
import 'tools_dropdown.dart' show HamburgerMenu;
import 'session_info_overlay.dart';

/// Home tab enum for sidebar layout
enum SidebarHomeTab { chat }

/// Sidebar-based home screen
class SidebarHomeScreen extends ConsumerStatefulWidget {
  const SidebarHomeScreen({super.key});

  @override
  ConsumerState<SidebarHomeScreen> createState() => _SidebarHomeScreenState();
}

class _SidebarHomeScreenState extends ConsumerState<SidebarHomeScreen> {
  SidebarHomeTab _selectedTab = SidebarHomeTab.chat;
  Session? _selectedSession;
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
      if (_selectedTab != SidebarHomeTab.chat) {
        setState(() => _selectedTab = SidebarHomeTab.chat);
      }
      ref.read(focusInputRequestProvider.notifier).state = true;
    };
    _leaderController.onInSessionFind = () {
      if (_selectedTab != SidebarHomeTab.chat) {
        setState(() => _selectedTab = SidebarHomeTab.chat);
      }
      final session = _selectedSession ?? ref.read(activeSessionProvider);
      final sid = session?.id ?? 'default';
      ref.read(findBarVisibleProvider(sid).notifier).state = true;
    };
    _leaderController.onGlobalSearch = () {
      context.goToolSearch();
    };
    _leaderController.onBranches = () {
      context.goToolBranches();
    };
    _leaderController.onShowCommandPalette = _showCommandPalette;
    _leaderController.onCycleVerbosity = _cycleVerbosity;

    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(chatProvider);
      unawaited(_onConnectionChanged(ref.read(connectionStateProvider)));

      // Auto-create session if none exists
      final active = ref.read(activeSessionProvider);
      if (active == null) {
        _createInitialSession();
      } else {
        setState(() => _selectedSession = active);
      }
    });
  }

  @override
  void dispose() {
    _leaderController.dispose();
    super.dispose();
  }

  Future<void> _createInitialSession() async {
    final session = await ref.read(sessionProvider.notifier).createSession('new session');
    if (session != null && mounted) {
      setState(() => _selectedSession = session);
      ref.read(activeSessionProvider.notifier).state = session;
    }
  }

  void _onLeaderTabSelected(int index) {
    // Sidebar only has chat tab, but handle for compatibility
    if (index == 0) {
      setState(() => _selectedTab = SidebarHomeTab.chat);
    }
  }

  void _onLeaderNavigate(String path) {
    context.go(path);
  }

  Future<void> _onConnectionChanged(bool connected) async {
    if (connected && !_initialLoadDone) {
      _initialLoadDone = true;
      await ref.read(sessionProvider.notifier).loadSessions();
      ref.read(taskProvider.notifier).loadTasks();
      ref.read(agentProvider.notifier).loadAgents();
      ref.read(currentProjectProvider.notifier).refresh();
    }
  }

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

  void _handlePaletteSelection(CommandPaletteItem item) {
    switch (item.label) {
      case 'chat':
        setState(() => _selectedTab = SidebarHomeTab.chat);
        break;
      case 'find…':
        _leaderController.onFind?.call();
        break;
      case 'new session':
        _createInitialSession();
        break;
      case 'projects':
        _leaderController.onBranches?.call();
        break;
      default:
        break;
    }
  }

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
              _buildHelpRow('f', 'global search'),
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

  void _onSessionSelected(Session session) {
    setState(() => _selectedSession = session);
    ref.read(activeSessionProvider.notifier).state = session;
  }

  @override
  Widget build(BuildContext context) {
    ref.listen<bool>(connectionStateProvider, (prev, connected) {
      unawaited(_onConnectionChanged(connected));
    });

    return AppShortcuts(
      controller: _leaderController,
      child: Scaffold(
        backgroundColor: CyberpunkColors.black,
        body: Row(
          children: [
            // Left sidebar with session tree
            _Sidebar(
              onSessionSelected: _onSessionSelected,
              selectedSession: _selectedSession,
            ),
            // Main content area
            Expanded(
              child: Column(
                children: [
                  // Header with session info
                  _Header(session: _selectedSession),
                  // Chat content
                  Expanded(
                    child: _selectedSession != null
                        ? ChatTab(sessionId: _selectedSession!.id)
                        : const _NoSessionPlaceholder(),
                  ),
                  // Chat input is part of ChatTab
                ],
              ),
            ),
          ],
        ),
        bottomNavigationBar: StatusBar(selectedTabIndex: 0),
      ),
    );
  }
}

class _NoSessionPlaceholder extends StatelessWidget {
  const _NoSessionPlaceholder();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.chat_bubble_outline,
            size: 48,
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.5),
          ),
          const SizedBox(height: 16),
          Text(
            'select a session to begin',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
        ],
      ),
    );
  }
}

/// Sidebar with session tree
class _Sidebar extends ConsumerStatefulWidget {
  final ValueChanged<Session> onSessionSelected;
  final Session? selectedSession;

  const _Sidebar({
    required this.onSessionSelected,
    this.selectedSession,
  });

  @override
  ConsumerState<_Sidebar> createState() => _SidebarState();
}

class _SidebarState extends ConsumerState<_Sidebar> {
  final Map<String, bool> _expandedSessions = {};

  @override
  Widget build(BuildContext context) {
    final sessions = ref.watch(sessionProvider).sessions;

    return Container(
      width: 220,
      decoration: BoxDecoration(
        color: CyberpunkColors.greenSuccess.withValues(alpha: 0.1),
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          // Sidebar header
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: CyberpunkColors.orangePrimary,
                  width: 2,
                ),
              ),
            ),
            child: Text(
              'sessions',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
                letterSpacing: 2,
              ),
            ),
          ),
          // Session tree
          Expanded(
            child: ListView.builder(
              padding: const EdgeInsets.symmetric(vertical: 8),
              itemCount: sessions.length,
              itemBuilder: (context, index) {
                final session = sessions[index];
                return _SessionTreeItem(
                  session: session,
                  isSelected: widget.selectedSession?.id == session.id,
                  isExpanded: _expandedSessions[session.id] ?? false,
                  onToggle: () {
                    setState(() {
                      _expandedSessions[session.id] = !(_expandedSessions[session.id] ?? false);
                    });
                  },
                  onSelect: () => widget.onSessionSelected(session),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

/// Individual session tree item with expand/collapse
class _SessionTreeItem extends StatelessWidget {
  final Session session;
  final bool isSelected;
  final bool isExpanded;
  final VoidCallback onToggle;
  final VoidCallback onSelect;

  const _SessionTreeItem({
    required this.session,
    required this.isSelected,
    required this.isExpanded,
    required this.onToggle,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Container(
          decoration: BoxDecoration(
            color: isSelected
                ? CyberpunkColors.orangePrimary.withValues(alpha: 0.2)
                : Colors.transparent,
            border: Border(
              left: BorderSide(
                color: isSelected ? CyberpunkColors.orangePrimary : Colors.transparent,
                width: 3,
              ),
            ),
          ),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          child: Row(
            children: [
              // Expand/collapse indicator
              GestureDetector(
                onTap: onToggle,
                child: SizedBox(
                  width: 16,
                  height: 16,
                  child: Icon(
                    isExpanded ? Icons.arrow_drop_down : Icons.arrow_right,
                    size: 14,
                    color: CyberpunkColors.lightGray,
                  ),
                ),
              ),
              // Session name (clickable)
              Expanded(
                child: GestureDetector(
                  onTap: onSelect,
                  child: Text(
                    session.title.isEmpty ? 'unnamed' : session.title,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : CyberpunkColors.lightGray,
                      fontFamily: 'SourceCodePro',
                      fontSize: 11,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ),
              // Info icon
              GestureDetector(
                onTap: () {
                  showDialog(
                    context: context,
                    barrierDismissible: true,
                    builder: (_) => SessionInfoOverlay(session: session),
                  );
                },
                child: Padding(
                  padding: const EdgeInsets.all(4),
                  child: Icon(
                    Icons.info_outline,
                    size: 14,
                    color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
                  ),
                ),
              ),
            ],
          ),
        ),
        // Children (tasks/plans) - shown when expanded
        if (isExpanded) ..._buildChildren(),
      ],
    );
  }

  List<Widget> _buildChildren() {
    // TODO: This will be populated with actual tasks/plans in Phase 5
    // For now, show a placeholder
    return [
      Padding(
        padding: const EdgeInsets.only(left: 24, bottom: 4),
        child: Text(
          'loading...',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
            fontFamily: 'SourceCodePro',
            fontSize: 10,
          ),
        ),
      ),
    ];
  }
}

/// Header bar showing session info
class _Header extends StatelessWidget {
  final Session? session;

  const _Header({this.session});

  @override
  Widget build(BuildContext context) {
    String headerText = 'meept';

    if (session != null && session!.title.isNotEmpty) {
      headerText = session!.title;
      if (session!.description != null && session!.description!.isNotEmpty) {
        final truncated = session!.description!.length > 60
            ? '${session!.description!.substring(0, 57)}...'
            : session!.description!;
        headerText = '$headerText │ $truncated';
      }
    }

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      color: CyberpunkColors.orangePrimary,
      child: Row(
        children: [
          Expanded(
            child: Text(
              headerText.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.black,
                fontWeight: FontWeight.bold,
                fontFamily: 'SourceCodePro',
                fontSize: 13,
              ),
              overflow: TextOverflow.ellipsis,
              maxLines: 1,
            ),
          ),
          // Connection indicator
          _ConnectionDot(),
          const SizedBox(width: 12),
          // Hamburger menu
          HamburgerMenu(
            onToolSelected: (route) {
              // Navigate to tool
              switch (route) {
                case 'search':
                  context.goToolSearch();
                case 'branches':
                  context.goToolBranches();
                default:
                  break;
              }
            },
          ),
        ],
      ),
    );
  }
}

/// Connection status indicator
class _ConnectionDot extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);
    final isConnecting = ref.watch(isConnectingProvider);

    return Row(
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
          isConnecting ? 'connecting...' : (connected ? 'connected' : 'disconnected'),
          style: CyberpunkTypography.bodySmall.copyWith(
            color: isConnecting
                ? CyberpunkColors.orangePrimary
                : connected
                    ? CyberpunkColors.greenSuccess
                    : CyberpunkColors.redAlert,
            fontFamily: 'SourceCodePro',
            fontSize: 10,
          ),
        ),
      ],
    );
  }
}
