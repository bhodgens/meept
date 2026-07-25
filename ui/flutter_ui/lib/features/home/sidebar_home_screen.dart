/// Sidebar-based home screen layout
///
/// Alternative to the traditional top-tab navigation, featuring:
/// - Left sidebar with session tree (expandable/collapsible)
/// - Chat view always visible in main content area
/// - Session info overlay accessible via [i] icon
/// - Resizable sidebar width with drag-to-resize divider
/// - Collapsible sidebar with arrow toggle
///
/// Structure:
/// ```
/// +------+------------------------------------------+
/// |  <   |  Header: Session Title                   |
/// | SESS |  ────────────────────────────────────────+
/// | [+]  |                                          |
/// | sess1│           Chat Message Area              |
/// | ├─ t │                                          |
/// | ▼ s2 │                                          |
/// | ├─ p ├──────────────────────────────────────────+
/// | │ t  │  [ Chat Input Area ]                     |
/// +------+──────────────────────────────────────────+
/// |  [Status Bar]                                   |
/// +-------------------------------------------------+
/// ```

/// Sidebar-based home screen layout
///
/// Alternative to the traditional top-tab navigation, featuring:
/// - Left sidebar with session tree, hamburger menu, and connection status
/// - Chat view always visible in main content area
/// - Session info overlay accessible via [i] icon
/// - Header bar with session title and description
///
/// Structure:
/// ```
/// +--------------+------------------------------------------+
/// | [汉堡] meept ● |  Header: Session Title                   |
/// |  SESSIONS    |  ────────────────────────────────────────+
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
///
/// The sidebar header includes:
/// - Hamburger menu button for tools (search, branches, etc.)
/// - ASCII-style "meept" logo
/// - Connection status indicator (green dot = connected)

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../core/router.dart';
import '../../core/shortcuts.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/background_image.dart';
import '../../widgets/command_palette.dart';
import '../../widgets/status_bar.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';
import '../../providers/status_message_provider.dart';
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
  bool _sidebarCollapsed = false;


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
      // Open full-window dialogs for tab views
      case 'sessions':
        _showFullWindowDialog('sessions', Icons.folder, const _SessionsDialog());
        break;
      case 'plans':
        _showFullWindowDialog('plans', Icons.document_scanner, const _PlansDialog());
        break;
      case 'tasks':
        _showFullWindowDialog('tasks', Icons.task_alt, const _TasksDialog());
        break;
      case 'agents':
        _showFullWindowDialog('agents', Icons.smart_toy, const _AgentsDialog());
        break;
      case 'edit description':
        _showEditDescriptionDialog();
        break;
    }
  }

  void _showFullWindowDialog(String title, IconData icon, Widget content) {
    showDialog(
      context: context,
      barrierDismissible: true,
      builder: (context) => _FullWindowDialog(
        title: title,
        icon: icon,
        content: content,
        onClose: () => Navigator.of(context).pop(),
      ),
    );
  }

  void _showEditDescriptionDialog() {
    final controller = TextEditingController(text: _selectedSession?.description ?? '');
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'edit description',
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        content: TextField(
          controller: controller,
          style: CyberpunkTypography.bodyMedium,
          decoration: InputDecoration(
            hintText: 'session description',
            hintStyle: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.midGray,
            ),
            filled: true,
            fillColor: CyberpunkColors.black,
            border: OutlineInputBorder(
              borderSide: BorderSide(color: CyberpunkColors.orangeDark),
            ),
          ),
          maxLines: 3,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text('cancel', style: CyberpunkTypography.bodySmall),
          ),
          TextButton(
            onPressed: () {
              Navigator.of(context).pop();
              showStatusMessage(ref, 'description updated');
            },
            child: Text('save', style: CyberpunkTypography.bodySmall.copyWith(color: CyberpunkColors.orangePrimary)),
          ),
        ],
      ),
    );
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
        body: BackgroundImage(
          child: Row(
            children: [
              // Left sidebar with session tree (collapsible)
              if (!_sidebarCollapsed)
                Stack(
                  children: [
                    _Sidebar(
                      onSessionSelected: _onSessionSelected,
                      selectedSession: _selectedSession,
                    ),
                    // Toggle overlaid at the sidebar's right edge
                    Positioned(
                      right: 0,
                      top: 0,
                      child: _SidebarToggle(
                        collapsed: false,
                        onTap: () {
                          setState(() => _sidebarCollapsed = true);
                        },
                      ),
                    ),
                  ],
                )
              else
                // Collapsed: toggle at window's left edge
                _SidebarToggle(
                  collapsed: true,
                  onTap: () {
                    setState(() => _sidebarCollapsed = false);
                  },
                ),
              // Main content area (ChatTab provides its own header)
              Expanded(
                child: _selectedSession != null
                    ? ChatTab(sessionId: _selectedSession!.id)
                    : const _NoSessionPlaceholder(),
              ),
            ],
          ),
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

/// Collapse/expand toggle arrow on the sidebar divider.
/// Shows a left arrow (chevron_left) when sidebar is open,
/// and a right arrow (chevron_right) when collapsed.
///
/// The clickable button matches the chat header height and sits at the top.
/// A full-height vertical line runs below it:
///   - expanded: right edge, ghosted orangeDark
///   - collapsed: left edge, solid orangePrimary (matching the header bar)
class _SidebarToggle extends StatelessWidget {
  final bool collapsed;
  final VoidCallback onTap;

  const _SidebarToggle({required this.collapsed, required this.onTap});

  @override
  Widget build(BuildContext context) {
    // Match the chat header height (vertical 8 padding + ~26px content row).
    const double headerHeight = 44;
    final lineColor = collapsed
        ? CyberpunkColors.orangePrimary
        : CyberpunkColors.orangeDark.withValues(alpha: 0.3);

    return GestureDetector(
      onTap: onTap,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: SizedBox(
          width: 18,
          child: Stack(
            children: [
              // Full-height vertical line at the appropriate edge.
              Positioned(
                left: collapsed ? 0 : null,
                right: collapsed ? null : 0,
                top: 0,
                bottom: 0,
                child: Container(width: 1, color: lineColor),
              ),
              // Clickable toggle button at the top, matching header height.
              Positioned(
                top: 0,
                left: 0,
                right: 0,
                child: Container(
                  height: headerHeight,
                  color: CyberpunkColors.orangeDark.withValues(alpha: 0.15),
                  child: Center(
                    child: Icon(
                      collapsed ? Icons.chevron_right : Icons.chevron_left,
                      size: 16,
                      color: CyberpunkColors.orangePrimary,
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
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

    return SizedBox(
      width: 220,
      child: TintedBackgroundImage(
        imageOpacity: 0.15,
        tintColor: CyberpunkColors.greenSuccess,
        tintOpacity: 0.08,
        child: Column(
        children: [
          // Sidebar header with hamburger, meept logo, and connection status
          Container(
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: CyberpunkColors.orangePrimary,
                  width: 2,
                ),
              ),
            ),
            child: Row(
              children: [
                // Hamburger menu
                HamburgerMenu(
                  onToolSelected: (route) {
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
                const SizedBox(width: 8),
                // ASCII-style meept logo
                Text(
                  'meept',
                  style: CyberpunkTypography.label.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontFamily: 'SourceCodePro',
                    fontWeight: FontWeight.bold,
                    letterSpacing: 1,
                  ),
                ),
                const Spacer(),
              ],
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
      ),
    );
  }
}

/// Individual session tree item with expand/collapse
class _SessionTreeItem extends ConsumerStatefulWidget {
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
  ConsumerState<_SessionTreeItem> createState() => _SessionTreeItemState();
}

/// Holds child items (tasks+plans) loaded for one session.
class _SessionChildren {
  final List<Task> tasks;
  final List<Plan> plans;
  const _SessionChildren({required this.tasks, required this.plans});
}

class _SessionTreeItemState extends ConsumerState<_SessionTreeItem> {
  Future<_SessionChildren?>? _childrenFuture;

  @override
  void didUpdateWidget(_SessionTreeItem oldWidget) {
    super.didUpdateWidget(oldWidget);
    // Reset on expansion toggle so we refetch
    if (widget.isExpanded != oldWidget.isExpanded) {
      _childrenFuture = null;
    }
  }

  Future<_SessionChildren?> _fetchChildren() async {
    final sdkClient = ref.read(sdkClientProvider);
    final sessionId = widget.session.id;
    try {
      final futures = <Future<dynamic>>[
        sdkClient.listTasks(sessionId: sessionId).then(
            (r) => r.map((t) => Task.fromJson(t)).toList()),
        sdkClient.listPlansBySession(sessionId).then(
            (r) => r.map((p) => Plan.fromJson(p)).toList()),
      ];
      final results = await Future.wait(futures);
      return _SessionChildren(
        tasks: results[0] as List<Task>,
        plans: results[1] as List<Plan>,
      );
    } catch (e) {
      debugPrint('[tree] load children for session $sessionId: $e');
      return null;
    }
  }

  @override
  Widget build(BuildContext context) {
    // Kick off loading when expanded for the first time
    if (widget.isExpanded && _childrenFuture == null) {
      _childrenFuture = _fetchChildren();
      // Trigger re-build once data arrives
      _childrenFuture!.then((_) => setState(() {}));
    }

    return Column(
      children: [
        Container(
          decoration: BoxDecoration(
            color: widget.isSelected
                ? CyberpunkColors.orangePrimary.withValues(alpha: 0.2)
                : Colors.transparent,
            border: Border(
              left: BorderSide(
                color: widget.isSelected
                    ? CyberpunkColors.orangePrimary
                    : Colors.transparent,
                width: 3,
              ),
            ),
          ),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          child: Row(
            children: [
              // Expand/collapse indicator (always show arrow to allow collapse)
              GestureDetector(
                onTap: widget.onToggle,
                child: SizedBox(
                  width: 16,
                  height: 16,
                  child: Icon(
                    widget.isExpanded
                        ? Icons.arrow_drop_down
                        : Icons.arrow_right,
                    size: 14,
                    color: widget.isExpanded
                        ? CyberpunkColors.orangePrimary
                        : CyberpunkColors.midGray,
                  ),
                ),
              ),
              // Session name (clickable)
              Expanded(
                child: GestureDetector(
                  onTap: widget.onSelect,
                  child: Text(
                    widget.session.title.isEmpty
                        ? 'unnamed'
                        : widget.session.title,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: widget.isSelected
                          ? CyberpunkColors.orangePrimary
                          : CyberpunkColors.orangeDark,
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
                    builder: (_) =>
                        SessionInfoOverlay(session: widget.session),
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
        if (widget.isExpanded) ..._buildChildren(),
      ],
    );
  }

  List<Widget> _buildChildren() {
    final future = _childrenFuture;

    // Not yet loaded — show loading indicator
    if (future == null) {
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

    return [
      FutureBuilder<_SessionChildren?>(
        future: future,
        builder: (context, snap) {
          if (snap.hasData && snap.data != null) {
            final data = snap.data!;
            final allChildren = <Map<String, dynamic>>[
              for (final t in data.tasks) {'type': 'task', 'data': t},
              for (final p in data.plans) {'type': 'plan', 'data': p},
            ]..sort((a, b) {
                final ta = a['type'] as String;
                final tb = b['type'] as String;
                if (ta != tb) return ta == 'task' ? -1 : 1;
                return 0;
              });

            if (allChildren.isEmpty) {
              return Padding(
                padding: const EdgeInsets.only(left: 24, bottom: 4),
                child: Text(
                  'no tasks or plans',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontFamily: 'SourceCodePro',
                    fontSize: 10,
                  ),
                ),
              );
            }

            return Column(
              children: allChildren
                  .map((child) {
                    final type = child['type'] as String;
                    final task = type == 'task' ? (child['data'] as Task?) : null;
                    final plan = type == 'plan' ? (child['data'] as Plan?) : null;
                    return Padding(
                      padding: const EdgeInsets.only(left: 24, bottom: 2),
                      child: _ChildWidget(type: type, task: task, plan: plan),
                    );
                  })
                  .toList(),
            );
          }

          if (snap.hasError) {
            return Padding(
              padding: const EdgeInsets.only(left: 24, bottom: 4),
              child: Text(
                'error loading children',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                  fontFamily: 'SourceCodePro',
                  fontSize: 10,
                ),
              ),
            );
          }

          return Padding(
            padding: const EdgeInsets.only(left: 24, bottom: 4),
            child: Text(
              'loading...',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontFamily: 'SourceCodePro',
                fontSize: 10,
              ),
            ),
          );
        },
      ),
    ];
  }
}

/// Renders a single child item (task or plan) in the session tree.
class _ChildWidget extends StatelessWidget {
  final String type;
  final Task? task;
  final Plan? plan;

  const _ChildWidget({
    required this.type,
    this.task,
    this.plan,
  });

  @override
  Widget build(BuildContext context) {
    final bgColor = CyberpunkColors.midGray.withValues(alpha: 0.2);
    final textColor = CyberpunkColors.lightGray;
    final accentColor =
        type == 'task' ? CyberpunkColors.orangeDark : CyberpunkColors.greenSuccess;
    final title = type == 'task' ? (task!.title.isEmpty ? 'untitled task' : task!.title) : plan!.title;

    return Container(
      decoration: BoxDecoration(
        color: bgColor,
        border: Border(
          left: BorderSide(
            color: accentColor.withValues(alpha: 0.3),
            width: 2,
          ),
        ),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      child: Row(
        children: [
          // Type icon
          Icon(
            type == 'task' ? Icons.check_box_outline_blank : Icons.folder_outlined,
            size: 12,
            color: accentColor,
          ),
          const SizedBox(width: 6),
          // Label
          Expanded(
            child: Text(
              title,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: textColor,
                fontFamily: 'SourceCodePro',
                fontSize: 10,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          // Task status
          if (type == 'task' && task!.status.isNotEmpty)
            Text(
              task!.status,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: accentColor.withValues(alpha: 0.6),
                fontFamily: 'SourceCodePro',
                fontSize: 9,
              ),
            ),
        ],
      ),
    );
  }
}

// -----------------------------------------------------------------------------
// Full-window dialog for tab views
// -----------------------------------------------------------------------------

class _FullWindowDialog extends StatelessWidget {
  final String title;
  final IconData icon;
  final Widget content;
  final VoidCallback onClose;

  const _FullWindowDialog({
    required this.title,
    required this.icon,
    required this.content,
    required this.onClose,
  });

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      insetPadding: const EdgeInsets.all(20),
      child: Container(
        width: 800,
        height: 600,
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border.all(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
            width: 2,
          ),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          children: [
            // Header
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: CyberpunkColors.orangePrimary,
                borderRadius: const BorderRadius.only(
                  topLeft: Radius.circular(8),
                  topRight: Radius.circular(8),
                ),
              ),
              child: Row(
                children: [
                  Icon(icon, color: CyberpunkColors.black, size: 20),
                  const SizedBox(width: 12),
                  Text(
                    title,
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.black,
                      fontWeight: FontWeight.bold,
                      fontFamily: 'SourceCodePro',
                    ),
                  ),
                  const Spacer(),
                  GestureDetector(
                    onTap: onClose,
                    child: Container(
                      padding: const EdgeInsets.all(4),
                      child: Icon(Icons.close, size: 20, color: CyberpunkColors.black),
                    ),
                  ),
                ],
              ),
            ),
            // Content
            Expanded(child: content),
          ],
        ),
      ),
    );
  }
}

// -----------------------------------------------------------------------------
// Sessions dialog
// -----------------------------------------------------------------------------

class _SessionsDialog extends ConsumerWidget {
  const _SessionsDialog();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sessionsState = ref.watch(sessionProvider);
    final sessions = sessionsState.sessions;

    if (sessions.isEmpty) {
      return const Center(
        child: Text(
          'no sessions',
          style: TextStyle(color: CyberpunkColors.midGray, fontSize: 16),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: sessions.length,
      itemBuilder: (context, index) {
        final session = sessions[index];
        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: CyberpunkColors.midGray.withValues(alpha: 0.2),
            border: Border.all(color: CyberpunkColors.orangeDark.withValues(alpha: 0.3)),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Text(
            session.title.isEmpty ? 'unnamed' : session.title,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.lightGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
        );
      },
    );
  }
}

// -----------------------------------------------------------------------------
// Plans dialog
// -----------------------------------------------------------------------------

class _PlansDialog extends ConsumerWidget {
  const _PlansDialog();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final plansState = ref.watch(planProvider);
    final plans = plansState.plans;

    if (plans.isEmpty) {
      return const Center(
        child: Text(
          'no plans',
          style: TextStyle(color: CyberpunkColors.midGray, fontSize: 16),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: plans.length,
      itemBuilder: (context, index) {
        final plan = plans[index];
        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: CyberpunkColors.midGray.withValues(alpha: 0.2),
            border: Border.all(color: CyberpunkColors.orangeDark.withValues(alpha: 0.3)),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                plan.title.isEmpty ? 'Untitled Plan' : plan.title,
                style: CyberpunkTypography.bodyMedium.copyWith(
                  color: CyberpunkColors.orangePrimary,
                  fontFamily: 'SourceCodePro',
                ),
              ),
              if (plan.description.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  plan.description,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                  ),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}

// -----------------------------------------------------------------------------
// Tasks dialog
// -----------------------------------------------------------------------------

class _TasksDialog extends ConsumerWidget {
  const _TasksDialog();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tasksState = ref.watch(taskProvider);
    final tasks = tasksState.tasks;

    if (tasks.isEmpty) {
      return const Center(
        child: Text(
          'no tasks',
          style: TextStyle(color: CyberpunkColors.midGray, fontSize: 16),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: tasks.length,
      itemBuilder: (context, index) {
        final task = tasks[index];
        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: CyberpunkColors.midGray.withValues(alpha: 0.2),
            border: Border.all(color: CyberpunkColors.orangeDark.withValues(alpha: 0.3)),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Row(
            children: [
              Icon(
                task.status == 'completed' ? Icons.check_circle :
                task.status == 'failed' ? Icons.error :
                task.status == 'in_progress' ? Icons.hourglass_empty :
                Icons.circle_outlined,
                size: 18,
                color: task.status == 'completed' ? CyberpunkColors.greenSuccess :
                     task.status == 'failed' ? CyberpunkColors.redAlert :
                     task.status == 'in_progress' ? CyberpunkColors.orangePrimary :
                     CyberpunkColors.lightGray,
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  task.title.isEmpty ? 'Untitled Task' : task.title,
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: CyberpunkColors.lightGray,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

// -----------------------------------------------------------------------------
// Agents dialog
// -----------------------------------------------------------------------------

class _AgentsDialog extends ConsumerWidget {
  const _AgentsDialog();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final agentsState = ref.watch(agentProvider);
    final agents = agentsState.agents;

    if (agents.isEmpty) {
      return const Center(
        child: Text(
          'no agents',
          style: TextStyle(color: CyberpunkColors.midGray, fontSize: 16),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: agents.length,
      itemBuilder: (context, index) {
        final agent = agents[index];
        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: CyberpunkColors.midGray.withValues(alpha: 0.2),
            border: Border.all(color: CyberpunkColors.orangeDark.withValues(alpha: 0.3)),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Row(
            children: [
              Icon(
                Icons.smart_toy,
                size: 20,
                color: CyberpunkColors.orangePrimary,
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      agent.name.isEmpty ? 'Unknown Agent' : agent.name,
                      style: CyberpunkTypography.bodyMedium.copyWith(
                        color: CyberpunkColors.orangePrimary,
                        fontFamily: 'SourceCodePro',
                      ),
                    ),
                    if (agent.description.isNotEmpty)
                      Text(
                        agent.description,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: CyberpunkColors.lightGray,
                          fontSize: 10,
                        ),
                      ),
                  ],
                ),
              ),
              if (agent.enabled)
                Container(
                  width: 8,
                  height: 8,
                  decoration: BoxDecoration(
                    color: CyberpunkColors.greenSuccess,
                    shape: BoxShape.circle,
                  ),
                ),
            ],
          ),
        );
      },
    );
  }
}
