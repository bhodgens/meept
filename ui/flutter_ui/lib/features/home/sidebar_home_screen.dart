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
library;

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
import '../../dialogs/directory_browser_dialog.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';
import '../../providers/status_message_provider.dart';
import '../../providers/session_detail.dart';
import '../../providers/verbosity_provider.dart';
import '../../providers/rendering_prefs_provider.dart';
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

  /// Guards the one-time initial session selection so reconnects after a
  /// transient network drop don't re-run it (mirrors HomeScreen).
  bool _initialLoadDone = false;

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
      unawaited(_onConnectionChanged(ref.read(connectionStateProvider)));

      // Sync local selection from any already-active session. The initial
      // session selection (reuse-or-create) happens in _onConnectionChanged
      // after the session list has loaded — doing it here would race the
      // load and always create a new session.
      final active = ref.read(activeSessionProvider);
      if (active != null) {
        setState(() => _selectedSession = active);
      }
    });
  }

  @override
  void dispose() {
    _leaderController.dispose();
    super.dispose();
  }

  /// Select an initial session, reusing an existing empty one when possible
  /// so relaunching the UI doesn't accumulate unused "new session" entries.
  /// Falls back to creating a new session when none is reusable.
  Future<void> _createInitialSession() async {
    final notifier = ref.read(sessionProvider.notifier);
    final session =
        notifier.findReusableEmptySession() ??
        await notifier.createSession('new session');
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

    // One-time initial session selection, run after the session list has
    // loaded so findReusableEmptySession() sees real data. Guarded so
    // reconnects after a transient network drop don't re-run it.
    if (!_initialLoadDone) {
      _initialLoadDone = true;
      final active = ref.read(activeSessionProvider);
      // session.auto_resume=false means "start with no session selected";
      // the user picks one from the list (or creates one) explicitly.
      final autoResume = ref.read(renderingPrefsProvider).autoResume;
      if (!autoResume) return;
      if (active == null) {
        await _createInitialSession();
      } else if (mounted) {
        setState(() => _selectedSession = active);
      }
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
        _showFullWindowDialog(
          'sessions',
          Icons.folder,
          const _SessionsDialog(),
        );
        break;
      case 'plans':
        _showFullWindowDialog(
          'plans',
          Icons.document_scanner,
          const _PlansDialog(),
        );
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
    final controller = TextEditingController(
      text: _selectedSession?.description ?? '',
    );
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
            border: const OutlineInputBorder(
              borderSide: BorderSide(color: CyberpunkColors.orangeDark),
            ),
          ),
          maxLines: 3,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('cancel', style: CyberpunkTypography.bodySmall),
          ),
          TextButton(
            onPressed: () async {
              final session = _selectedSession;
              final description = controller.text.trim();
              Navigator.of(context).pop();
              if (session == null) {
                showStatusMessage(ref, 'no active session');
                return;
              }
              try {
                await ref
                    .read(sdkClientProvider)
                    .updateSessionDescription(session.id, description);
                // Refresh cached session detail so the UI reflects the
                // change without a full session reload.
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
    debugPrint(
      '[session-debug] _onSessionSelected: id=${session.id} title=${session.title}',
    );
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
                child: Builder(
                  builder: (context) {
                    debugPrint(
                      '[session-debug] build main area: _selectedSession=${_selectedSession?.id}',
                    );
                    return _selectedSession != null
                        ? ChatTab(sessionId: _selectedSession!.id)
                        : const _NoSessionPlaceholder();
                  },
                ),
              ),
            ],
          ),
        ),
        bottomNavigationBar: const StatusBar(selectedTabIndex: 0),
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

  const _Sidebar({required this.onSessionSelected, this.selectedSession});

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

  /// Tracks the session ID we last auto-expanded for, so we only
  /// auto-expand once per session selection (not on every rebuild).
  String? _lastAutoExpandedSessionId;

  @override
  Widget build(BuildContext context) {
    final sessions = ref.watch(sessionProvider).sessions;
    final projects = ref.watch(resolveActiveProjectProvider).value;

    // Group sessions by project
    final groups = _groupByProject(sessions, projects);

    // Auto-expand the group containing the selected session so newly
    // created sessions are immediately visible. Only fires once per
    // session selection — the user can still collapse it afterward.
    if (widget.selectedSession != null &&
        widget.selectedSession!.id != _lastAutoExpandedSessionId) {
      _lastAutoExpandedSessionId = widget.selectedSession!.id;
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
                        case 'memory':
                          context.goToolMemory();
                        case 'prompts':
                          context.goToolPrompts();
                        case 'settings':
                          context.goSettings();
                        case 'files':
                          context.goToolFiles();
                        case 'terminal':
                          context.goToolTerminal();
                        case 'calendar':
                          context.goToolCalendar();
                        case 'metrics':
                          context.goToolMetrics();
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
                  final isExpanded =
                      _expandedProjects[group.projectId] ?? false;
                  final limit =
                      _projectSessionLimit[group.projectId] ??
                      _defaultSessionLimit;
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
            // Sidebar footer with "add project" button
            Container(
              padding: const EdgeInsets.all(4),
              decoration: BoxDecoration(
                border: Border(
                  top: BorderSide(color: CyberpunkColors.midGray, width: 0.5),
                ),
              ),
              child: Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.add, size: 18),
                    iconSize: 18,
                    padding: const EdgeInsets.all(4),
                    constraints: const BoxConstraints(
                      minWidth: 32,
                      minHeight: 32,
                    ),
                    color: CyberpunkColors.greenSuccess,
                    tooltip: 'add project',
                    onPressed: _addProject,
                  ),
                  Text(
                    'add project',
                    style: CyberpunkTypography.bodySmall.copyWith(
                      fontSize: 11,
                      color: CyberpunkColors.midGray,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// Normalise a filesystem path so that equivalent paths from different
  /// sources (e.g. trailing slash, case differences) match. This is a
  /// best-effort lexicographic normalisation — not a real canonical path
  /// resolver — but it covers the common cases that cause duplicate groups.
  String _normalisePath(String p) {
    var n = p.trim();
    // Strip trailing slashes (but keep a bare "/" as-is).
    while (n.length > 1 && n.endsWith('/')) {
      n = n.substring(0, n.length - 1);
    }
    // On case-insensitive filesystems (macOS HFS+/APFS default, Windows)
    // we can't reliably know the on-disk case, but lowercasing the ASCII
    // portion dramatically improves merge rates without breaking Linux.
    // We avoid forcing case on the whole string to preserve readability
    // of any unmatched fallback keys; matching is done on this normalised
    // form only.
    return n.toLowerCase();
  }

  /// Group sessions by project. Uses projectId when available, otherwise
  /// falls back to projectPath or detectionContext.cwd. Display name is
  /// the directory suffix (last path component) — matching the status bar.
  ///
  /// When a session has a projectPath but no projectId, we try to match that
  /// path against the paths of sessions (or the active project) that *do*
  /// have a projectId. This merges sessions into the same group instead of
  /// creating a duplicate `path:<...>` group for the same logical project.
  List<_ProjectGroup> _groupByProject(
    List<Session> sessions,
    Project? activeProject,
  ) {
    final byProject = <String, List<Session>>{};
    final projectNames = <String, String>{};
    final projectBranches = <String, String>{};

    // Pass 1: build a path → projectId map from sessions that have both,
    // plus the active project (if any).
    final pathToProjectId = <String, String>{};
    if (activeProject != null && activeProject.localPath.isNotEmpty) {
      pathToProjectId[_normalisePath(activeProject.localPath)] =
          activeProject.id;
    }
    for (final session in sessions) {
      final pid = session.projectId;
      if (pid == null || pid.isEmpty) continue;
      final p = session.projectPath ?? session.detectionContext?.cwd;
      if (p != null && p.isNotEmpty) {
        pathToProjectId.putIfAbsent(_normalisePath(p), () => pid);
      }
    }

    // Pass 2: assign each session a grouping key.
    for (final session in sessions) {
      // Prefer projectId directly.
      String key = session.projectId ?? '';
      if (key.isEmpty) {
        final path = session.projectPath ?? session.detectionContext?.cwd ?? '';
        if (path.isNotEmpty) {
          // Try to match the path to a known projectId first so we don't
          // create a duplicate group for the same logical project.
          final matched = pathToProjectId[_normalisePath(path)];
          key = matched ?? 'path:$path';
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
        path =
            entry.value.first.projectPath ??
            entry.value.first.detectionContext?.cwd;
      }
      if (path != null && path.isNotEmpty) {
        projectNames[entry.key] =
            path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? path;
      } else if (entry.key.isNotEmpty) {
        projectNames[entry.key] = entry.key.length > 8
            ? entry.key.substring(0, 8)
            : entry.key;
      }
    }

    final groups = <_ProjectGroup>[];

    for (final entry in byProject.entries) {
      if (entry.key.isEmpty) continue; // truly no project — handled below
      final sorted = List<Session>.from(entry.value)
        ..sort(
          (a, b) => (b.lastActivity ?? b.createdAt).compareTo(
            a.lastActivity ?? a.createdAt,
          ),
        );
      groups.add(
        _ProjectGroup(
          projectId: entry.key,
          projectName: projectNames[entry.key] ?? entry.key,
          branch: projectBranches[entry.key] ?? '',
          sessions: sorted,
        ),
      );
    }

    // Sessions with truly no project info — group under the active
    // project's directory name (the daemon's working project).
    final noProject = byProject[''];
    if (noProject != null && noProject.isNotEmpty) {
      final sorted = List<Session>.from(noProject)
        ..sort(
          (a, b) => (b.lastActivity ?? b.createdAt).compareTo(
            a.lastActivity ?? a.createdAt,
          ),
        );
      // Use active project name if available, otherwise "no project"
      final fallbackName = activeProject != null
          ? activeProject.name.split('/').last
          : 'no project';
      final fallbackBranch = activeProject?.branch ?? '';
      groups.add(
        _ProjectGroup(
          projectId: '',
          projectName: fallbackName,
          branch: fallbackBranch,
          sessions: sorted,
        ),
      );
    }

    // Sort groups by most recent session activity
    groups.sort((a, b) {
      final aTime = a.sessions.isEmpty
          ? DateTime(1970)
          : (a.sessions.first.lastActivity ?? a.sessions.first.createdAt);
      final bTime = b.sessions.isEmpty
          ? DateTime(1970)
          : (b.sessions.first.lastActivity ?? b.sessions.first.createdAt);
      return bTime.compareTo(aTime);
    });

    return groups;
  }

  Future<void> _createSessionInProject(_ProjectGroup group) async {
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession(
      'new session',
      projectId: group.projectId,
    );
    if (session != null && mounted) {
      widget.onSessionSelected(session);
      ref.read(activeSessionProvider.notifier).state = session;
    }
  }

  /// Open a daemon-side directory browser to add a new project, then
  /// create a session bound to the selected directory.
  Future<void> _addProject() async {
    final path = await DirectoryBrowserDialog.show(context);
    if (path == null || !mounted) return;

    // Create a session bound to the selected directory. The daemon's
    // SessionService resolves the path into a project (CreateOrResolve)
    // and binds it to the session.
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession('new session', cwd: path);
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
              // Project label (directory name)
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
                child: const Icon(
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
class _SidebarSessionRow extends ConsumerWidget {
  final Session session;
  final bool isSelected;
  final VoidCallback onSelect;

  const _SidebarSessionRow({
    required this.session,
    required this.isSelected,
    required this.onSelect,
  });

  /// Confirm and archive this session. Mirrors the sessions-list flow:
  /// a dialog guards the destructive action, and a status message reports
  /// the outcome only after the RPC resolves (parity with TUI).
  Future<void> _showArchiveConfirmation(
    BuildContext context,
    WidgetRef ref,
  ) async {
    await showDialog(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'archive session?',
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        content: Text(
          '"${(session.title.isEmpty ? 'unnamed' : session.title).toLowerCase()}"',
          style: CyberpunkTypography.bodyMedium,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext),
            child: Text(
              'cancel',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ),
          FilledButton(
            onPressed: () async {
              final notifier = ref.read(sessionProvider.notifier);
              await notifier.archiveSession(session.id);
              final error = ref.read(sessionProvider).error;
              if (error == null) {
                showStatusMessage(
                  ref,
                  'archived: ${(session.title.isEmpty ? 'unnamed' : session.title).toLowerCase()}',
                );
                if (dialogContext.mounted) Navigator.pop(dialogContext);
              } else {
                showStatusMessage(ref, 'archive failed: $error');
              }
            },
            child: const Text('archive', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
                session.worktreePath != null && session.worktreePath!.isNotEmpty
                    ? 'wt: ${session.title.isEmpty ? 'unnamed' : session.title}'
                    : session.title.isEmpty
                    ? 'unnamed'
                    : session.title,
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
            // Archive icon — confirm + archive this session
            GestureDetector(
              onTap: () => _showArchiveConfirmation(context, ref),
              behavior: HitTestBehavior.opaque,
              child: Padding(
                padding: const EdgeInsets.all(4),
                child: Icon(
                  Icons.archive_outlined,
                  size: 13,
                  color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
                ),
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
              decoration: const BoxDecoration(
                color: CyberpunkColors.orangePrimary,
                borderRadius: BorderRadius.only(
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
                      child: Icon(
                        Icons.close,
                        size: 20,
                        color: CyberpunkColors.black,
                      ),
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
            border: Border.all(
              color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            ),
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
            border: Border.all(
              color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            ),
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
            border: Border.all(
              color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            ),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Row(
            children: [
              Icon(
                task.status == 'completed'
                    ? Icons.check_circle
                    : task.status == 'failed'
                    ? Icons.error
                    : task.status == 'in_progress'
                    ? Icons.hourglass_empty
                    : Icons.circle_outlined,
                size: 18,
                color: task.status == 'completed'
                    ? CyberpunkColors.greenSuccess
                    : task.status == 'failed'
                    ? CyberpunkColors.redAlert
                    : task.status == 'in_progress'
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.lightGray,
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
            border: Border.all(
              color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            ),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Row(
            children: [
              const Icon(
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
                  decoration: const BoxDecoration(
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
