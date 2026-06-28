import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../features/home/home_screen.dart';
import '../features/settings/settings_panel.dart';
import '../features/search/search_panel.dart';
import '../features/projects/branches_panel.dart';
import '../features/skills/skill_panel.dart';
import '../features/memory/memory_panel.dart';
import '../features/reflection/reflection_panel.dart';
import '../features/prompts/prompt_panel.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// Global GoRouter instance, accessible via [routerProvider] or directly.
///
/// Route map:
///   /              -> HomeScreen (chat tab, default)
///   /sessions      -> HomeScreen (sessions tab)
///   /tasks         -> HomeScreen (tasks tab)
///   /settings      -> SettingsPanel (full-screen)
///   /tools/search   -> SearchPanel
///   /tools/branches -> BranchesPanel
///   /tools/skills   -> SkillPanel
///   /tools/memory   -> MemoryPanel
///   /tools/reflection -> ReflectionPanel
///   /tools/prompts    -> PromptPanel
final GoRouter router = GoRouter(
  initialLocation: '/',
  debugLogDiagnostics: true,
  routes: [
    // --- Top-level tab routes ---
    // HomeScreen is the shell; it reads the current route to pick the tab.
    GoRoute(
      path: '/',
      name: 'chat',
      pageBuilder: (context, state) {
        return const NoTransitionPage(
          child: _HomeShell(initialTab: HomeTab.chat),
        );
      },
    ),
    GoRoute(
      path: '/sessions',
      name: 'sessions',
      pageBuilder: (context, state) {
        return const NoTransitionPage(
          child: _HomeShell(initialTab: HomeTab.sessions),
        );
      },
    ),
    GoRoute(
      path: '/tasks',
      name: 'tasks',
      pageBuilder: (context, state) {
        return const NoTransitionPage(
          child: _HomeShell(initialTab: HomeTab.tasks),
        );
      },
    ),
    GoRoute(
      path: '/plans',
      name: 'plans',
      pageBuilder: (context, state) {
        return const NoTransitionPage(
          child: _HomeShell(initialTab: HomeTab.plans),
        );
      },
    ),
    GoRoute(
      path: '/agents',
      name: 'agents',
      pageBuilder: (context, state) {
        return const NoTransitionPage(
          child: _HomeShell(initialTab: HomeTab.agents),
        );
      },
    ),

    // --- Full-screen routes ---
    GoRoute(
      path: '/settings',
      name: 'settings',
      builder: (context, state) {
        return const SettingsPanel();
      },
    ),

    // --- Tool sub-routes ---
    GoRoute(
      path: '/tools/search',
      name: 'toolsSearch',
      builder: (context, state) {
        return const SearchPanel();
      },
    ),
    GoRoute(
      path: '/tools/branches',
      name: 'toolsBranches',
      builder: (context, state) {
        return const BranchesPanel();
      },
    ),
    GoRoute(
      path: '/tools/skills',
      name: 'toolsSkills',
      builder: (context, state) {
        return const SkillPanel();
      },
    ),
    GoRoute(
      path: '/tools/memory',
      name: 'toolsMemory',
      builder: (context, state) {
        return const MemoryPanel();
      },
    ),
    GoRoute(
      path: '/tools/reflection',
      name: 'toolsReflection',
      builder: (context, state) {
        return const ReflectionPanel();
      },
    ),
    GoRoute(
      path: '/tools/prompts',
      name: 'toolsPrompts',
      builder: (context, state) {
        return const PromptPanel();
      },
    ),
  ],

  // Gentle error page instead of a redirect.
  errorBuilder: (context, state) => Scaffold(
    backgroundColor: CyberpunkColors.black,
    body: Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            '404',
            style: CyberpunkTypography.headlineLarge.copyWith(
              color: CyberpunkColors.redAlert,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'route not found',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
          const SizedBox(height: 16),
          TextButton(
            onPressed: () => context.go('/'),
            child: Text(
              'go home',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
        ],
      ),
    ),
  ),
);

/// A Riverpod provider that exposes the [GoRouter] instance.
///
/// Widgets can call `ref.read(routerProvider)` if they need access
/// to the router outside of the widget tree (e.g. in shortcut
/// callbacks that don't have a BuildContext).
final routerProvider = Provider<GoRouter>((ref) => router);

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/// Wraps [HomeScreen] with a forced initial tab determined by the route.
///
/// This is a lightweight adapter — HomeScreen still owns tab state
/// internally, but it will call `setState` to the provided [initialTab]
/// during the first build frame.
class _HomeShell extends StatefulWidget {
  final HomeTab initialTab;

  const _HomeShell({required this.initialTab});

  @override
  State<_HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<_HomeShell> {
  late HomeTab _selectedTab;

  @override
  void initState() {
    super.initState();
    _selectedTab = widget.initialTab;
  }

  @override
  void didUpdateWidget(covariant _HomeShell oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.initialTab != widget.initialTab) {
      _selectedTab = widget.initialTab;
    }
  }

  @override
  Widget build(BuildContext context) {
    // Delegate to HomeScreen — the router has already picked the tab.
    // HomeScreen itself still handles the tab bar UI; this wrapper
    // ensures the correct tab is pre-selected on route entry.
    return TabOverrideScope(
      initialTab: _selectedTab,
      child: const HomeScreen(),
    );
  }
}

/// InheritedWidget that lets HomeScreen (or any descendant) read the
/// router-forced initial tab without requiring HomeScreen to know
/// about go_router.
class TabOverrideScope extends InheritedWidget {
  final HomeTab initialTab;

  const TabOverrideScope({
    super.key,
    required this.initialTab,
    required super.child,
  });

  static HomeTab? of(BuildContext context) {
    final scope =
        context.dependOnInheritedWidgetOfExactType<TabOverrideScope>();
    return scope?.initialTab;
  }

  @override
  bool updateShouldNotify(covariant TabOverrideScope oldWidget) =>
      oldWidget.initialTab != initialTab;
}

// ---------------------------------------------------------------------------
// Navigation helper extensions
// ---------------------------------------------------------------------------

/// Convenience extensions on [BuildContext] for go_router navigation.
///
/// These keep call sites readable and centralize route paths so they
/// don't need to be hard-coded throughout the widget tree.
extension AppRouterExtension on BuildContext {
  /// Navigate to the chat tab (replace current entry).
  void goChat() => go('/');

  /// Navigate to the sessions tab (replace current entry).
  void goSessions() => go('/sessions');

  /// Navigate to the tasks tab (replace current entry).
  void goTasks() => go('/tasks');

  /// Navigate to the plans tab (replace current entry).
  void goPlans() => go('/plans');

  /// Navigate to the agents tab (replace current entry).
  void goAgents() => go('/agents');

  /// Navigate to the settings screen (replace current entry).
  void goSettings() => go('/settings');

  /// Navigate to the search tool panel (replace current entry).
  void goToolSearch() => go('/tools/search');

  /// Navigate to the branches tool panel (replace current entry).
  void goToolBranches() => go('/tools/branches');

  /// Navigate to the skills tool panel (replace current entry).
  void goToolSkills() => go('/tools/skills');

  /// Navigate to the memory tool panel (replace current entry).
  void goToolMemory() => go('/tools/memory');

  /// Navigate to the reflection tool panel (replace current entry).
  void goToolReflection() => go('/tools/reflection');

  /// Navigate to the prompt-editor tool panel (replace current entry).
  void goToolPrompts() => go('/tools/prompts');
}
