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

  /// Refresh all data providers from the daemon.
  ///
  /// Called on every successful connection (initial connect AND reconnect)
  /// so the UI always reflects current server state without user intervention.
  Future<void> _refreshAllData() async {
    await ref.read(sessionProvider.notifier).loadSessions();
    ref.read(taskProvider.notifier).loadTasks();
    ref.read(agentProvider.notifier).loadAgents();
    ref.read(currentProjectProvider.notifier).refresh();
  }

  Future<void> _onConnectionChanged(bool connected) async {
    if (!connected) return;

    // Always refresh data on every (re)connect so the UI reflects
    // current server state without user intervention.
    await _refreshAllData();
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
                      bottom: 0,
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

    return SizedBox(
      width: 18,
      child: Stack(
        children: [
          // Full-height vertical line at the appropriate edge (non-interactive).
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
            child: GestureDetector(
              onTap: onTap,
              child: MouseRegion(
                cursor: SystemMouseCursors.click,
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
            ),
          ),
        ],
      ),
    );
  }
}

/// Sidebar with hierarchical project → session tree
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

/// A project group with its sessions.
class _ProjectGroup {
  final String projectId;
  final String projectName;
  final String branch;
  final List<Session> sessions;

  const _ProjectGroup({
    required this.projectId,
    required this.projectName,
    required this.branch,
    required this.sessions,
  });

  /// Display label: directory name of the project repo (no branch).
  String get displayLabel => projectName.split('/').last;
}

class _SidebarState extends ConsumerState<_Sidebar> {
  final Map<String, bool> _expandedProjects = {};
  final Map<String, int> _projectSessionLimit = {};
  static const int _defaultSessionLimit = 10;

  @override
  Widget build(BuildContext context) {
    final sessions = ref.watch(sessionProvider).sessions;
    final projects = ref.watch(resolveActiveProjectProvider).value;

    // Group sessions by project
    final groups = _groupByProject(sessions, projects);

    // Auto-expand the group containing the selected session so newly
    // created sessions are immediately visible.
    if (widget.selectedSession != null) {
      for (final group in groups) {
        if (group.sessions.any((s) => s.id == widget.selectedSession!.id)) {
          if (_expandedProjects[group.projectId] != true) {
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (mounted) {
                setState(() => _expandedProjects[group.projectId] = true);
              }
            });
          }
          break;
        }
      }
    }

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
          // Project-grouped session tree
          Expanded(
            child: ListView.builder(
              padding: const EdgeInsets.symmetric(vertical: 8),
              itemCount: groups.length,
              itemBuilder: (context, index) {
                final group = groups[index];
                final isExpanded = _expandedProjects[group.projectId] ?? false;
                final limit = _projectSessionLimit[group.projectId] ?? _defaultSessionLimit;
                return _ProjectGroupItem(
                  group: group,
                  isExpanded: isExpanded,
                  sessionLimit: limit,
                  selectedSessionId: widget.selectedSession?.id,
                  onToggle: () {
                    setState(() {
                      _expandedProjects[group.projectId] = !isExpanded;
                    });
                  },
                  onShowMore: () {
                    setState(() {
                      _projectSessionLimit[group.projectId] = limit + 10;
                    });
                  },
                  onSessionSelected: widget.onSessionSelected,
                  onCreateSession: () => _createSessionInProject(group),
                );
              },
            ),
          ),
        ],
        ),
      ),
    );
  }

  /// Group sessions by project. Uses projectId when available, otherwise
  /// falls back to projectPath or detectionContext.cwd. Display name is
  /// the directory suffix (last path component) — matching the status bar.
  List<_ProjectGroup> _groupByProject(List<Session> sessions, Project? activeProject) {
    final byProject = <String, List<Session>>{};
    final projectNames = <String, String>{};
    final projectBranches = <String, String>{};

    for (final session in sessions) {
      // Determine grouping key: prefer projectId, fall back to path.
      // When neither is available, merge into the active project's group
      // so legacy sessions (created before project wiring) don't form a
      // duplicate group with the same display name.
      String key = session.projectId ?? '';
      if (key.isEmpty) {
        final path = session.projectPath ?? session.detectionContext?.cwd ?? '';
        if (path.isNotEmpty) {
          key = 'path:$path';
        } else if (activeProject != null) {
          // No project info at all — fold into the active project.
          key = activeProject.id;
        }
      }
      byProject.putIfAbsent(key, () => []).add(session);
    }

    // Resolve display names: directory suffix of the project path
    if (activeProject != null) {
      projectNames[activeProject.id] = activeProject.name.split('/').last;
      projectBranches[activeProject.id] = activeProject.branch;
    }

    for (final entry in byProject.entries) {
      if (projectNames.containsKey(entry.key)) continue;
      // Derive name from path suffix
      String? path;
      if (entry.key.startsWith('path:')) {
        path = entry.key.substring(5);
      } else {
        path = entry.value.first.projectPath ?? entry.value.first.detectionContext?.cwd;
      }
      if (path != null && path.isNotEmpty) {
        projectNames[entry.key] = path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? path;
      } else if (entry.key.isNotEmpty) {
        projectNames[entry.key] = entry.key.length > 8 ? entry.key.substring(0, 8) : entry.key;
      }
    }

    final groups = <_ProjectGroup>[];

    for (final entry in byProject.entries) {
      if (entry.key.isEmpty) continue; // truly no project — handled below
      final sorted = List<Session>.from(entry.value)
        ..sort((a, b) => (b.lastActivity ?? b.createdAt).compareTo(a.lastActivity ?? a.createdAt));
      groups.add(_ProjectGroup(
        projectId: entry.key,
        projectName: projectNames[entry.key] ?? entry.key,
        branch: projectBranches[entry.key] ?? '',
        sessions: sorted,
      ));
    }

    // Sessions with truly no project info — group under the active
    // project's directory name (the daemon's working project).
    final noProject = byProject[''];
    if (noProject != null && noProject.isNotEmpty) {
      final sorted = List<Session>.from(noProject)
        ..sort((a, b) => (b.lastActivity ?? b.createdAt).compareTo(a.lastActivity ?? a.createdAt));
      // Use active project name if available, otherwise "no project"
      final fallbackName = activeProject != null
          ? activeProject.name.split('/').last
          : 'no project';
      final fallbackBranch = activeProject?.branch ?? '';
      groups.add(_ProjectGroup(
        projectId: '',
        projectName: fallbackName,
        branch: fallbackBranch,
        sessions: sorted,
      ));
    }

    // Sort groups by most recent session activity
    groups.sort((a, b) {
      final aTime = a.sessions.isEmpty ? DateTime(1970) : (a.sessions.first.lastActivity ?? a.sessions.first.createdAt);
      final bTime = b.sessions.isEmpty ? DateTime(1970) : (b.sessions.first.lastActivity ?? b.sessions.first.createdAt);
      return bTime.compareTo(aTime);
    });

    return groups;
  }

  Future<void> _createSessionInProject(_ProjectGroup group) async {
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession('new session');
    if (session != null && mounted) {
      widget.onSessionSelected(session);
      ref.read(activeSessionProvider.notifier).state = session;
    }
  }
}

/// Project group header + session list with expand/collapse
class _ProjectGroupItem extends StatelessWidget {
  final _ProjectGroup group;
  final bool isExpanded;
  final int sessionLimit;
  final String? selectedSessionId;
  final VoidCallback onToggle;
  final VoidCallback onShowMore;
  final ValueChanged<Session> onSessionSelected;
  final VoidCallback onCreateSession;

  const _ProjectGroupItem({
    required this.group,
    required this.isExpanded,
    required this.sessionLimit,
    required this.selectedSessionId,
    required this.onToggle,
    required this.onShowMore,
    required this.onSessionSelected,
    required this.onCreateSession,
  });

  @override
  Widget build(BuildContext context) {
    final visibleSessions = group.sessions.take(sessionLimit).toList();
    final hasMore = group.sessions.length > sessionLimit;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Project header row: [▶/▼] project-suffix branch  [+]
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          decoration: BoxDecoration(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.05),
          ),
          child: Row(
            children: [
              // Expand/collapse arrow
              GestureDetector(
                onTap: onToggle,
                behavior: HitTestBehavior.opaque,
                child: Icon(
                  isExpanded ? Icons.arrow_drop_down : Icons.arrow_right,
                  size: 16,
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const SizedBox(width: 2),
              // Project label (suffix + branch)
              Expanded(
                child: GestureDetector(
                  onTap: onToggle,
                  behavior: HitTestBehavior.opaque,
                  child: Text(
                    group.displayLabel,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.orangePrimary,
                      fontFamily: 'SourceCodePro',
                      fontSize: 11,
                      fontWeight: FontWeight.bold,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ),
              // Session count badge
              Text(
                '${group.sessions.length}',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                  fontFamily: 'SourceCodePro',
                  fontSize: 9,
                ),
              ),
              const SizedBox(width: 4),
              // + button to create session in this project
              GestureDetector(
                onTap: onCreateSession,
                behavior: HitTestBehavior.opaque,
                child: Icon(
                  Icons.add,
                  size: 14,
                  color: CyberpunkColors.orangeDark,
                ),
              ),
            ],
          ),
        ),
        // Session list (only when expanded)
        if (isExpanded) ...[
          for (final session in visibleSessions)
            _SidebarSessionRow(
              session: session,
              isSelected: selectedSessionId == session.id,
              onSelect: () => onSessionSelected(session),
            ),
          // "..." show more button
          if (hasMore)
            GestureDetector(
              onTap: onShowMore,
              behavior: HitTestBehavior.opaque,
              child: Padding(
                padding: const EdgeInsets.only(left: 28, top: 2, bottom: 4),
                child: Text(
                  '··· ${group.sessions.length - sessionLimit} more',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontFamily: 'SourceCodePro',
                    fontSize: 10,
                  ),
                ),
              ),
            ),
        ],
      ],
    );
  }
}

/// Single session row within a project group
class _SidebarSessionRow extends StatelessWidget {
  final Session session;
  final bool isSelected;
  final VoidCallback onSelect;

  const _SidebarSessionRow({
    required this.session,
    required this.isSelected,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onSelect,
      behavior: HitTestBehavior.opaque,
      child: Container(
        padding: const EdgeInsets.only(left: 28, right: 4, top: 4, bottom: 4),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.15)
              : Colors.transparent,
          border: Border(
            left: BorderSide(
              color: isSelected
                  ? CyberpunkColors.orangePrimary
                  : Colors.transparent,
              width: 2,
            ),
          ),
        ),
        child: Row(
          children: [
            Expanded(
              child: Text(
                session.title.isEmpty ? 'unnamed' : session.title,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: isSelected
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.orangeDark,
                  fontFamily: 'SourceCodePro',
                  fontSize: 11,
                ),
                overflow: TextOverflow.ellipsis,
              ),
            ),
            // Info icon — opens 4-tab session overlay
            GestureDetector(
              onTap: () {
                showDialog(
                  context: context,
                  barrierDismissible: true,
                  builder: (_) => SessionInfoOverlay(session: session),
                );
              },
              behavior: HitTestBehavior.opaque,
              child: Padding(
                padding: const EdgeInsets.all(4),
                child: Icon(
                  Icons.info_outline,
                  size: 13,
                  color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
                ),
              ),
            ),
          ],
        ),
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
