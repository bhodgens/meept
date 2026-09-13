// Verifies the GUI tool menu end to end: every menu entry resolves to a
// router path (so the shared back control always returns to chat), every
// entry's panel renders that shared back control, and the terminal tool
// is no longer part of the GUI surface.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/core/router.dart';
import 'package:meept_ui/features/calendar/calendar_panel.dart';
import 'package:meept_ui/features/changes/changes_panel.dart';
import 'package:meept_ui/features/home/tools_dropdown.dart';
import 'package:meept_ui/features/memory/memory_panel.dart';
import 'package:meept_ui/features/metrics/metrics_panel.dart';
import 'package:meept_ui/features/projects/branches_panel.dart';
import 'package:meept_ui/features/prompts/prompt_panel.dart';
import 'package:meept_ui/features/reflection/reflection_panel.dart';
import 'package:meept_ui/features/search/search_panel.dart';
import 'package:meept_ui/features/settings/settings_panel.dart';
import 'package:meept_ui/features/skills/skill_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/tool_exit_guard.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/storage_service.dart';
import 'package:meept_ui/services/websocket_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Offline stub: every fetch the panels issue on first build resolves to
/// an empty payload (or a benign error), so no test depends on a daemon.
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> getRecentMemoriesRaw({
    int limit = 10,
  }) async => [];

  @override
  Future<List<Map<String, dynamic>>> queryMemoryRaw({
    required String query,
    int limit = 10,
    String? category,
  }) async => [];

  @override
  Future<List<Map<String, dynamic>>> listPendingChanges(
    String sessionId,
  ) async => [];

  @override
  Future<List<Map<String, dynamic>>> listChangesJournal({
    String? sessionId,
    int? limit,
  }) async => [];

  @override
  Future<Map<String, dynamic>> getCalendarTodayRaw() async => {'events': []};

  @override
  Future<Map<String, dynamic>> getLiveMetrics() async => {
    'timestamp': '2024-01-01T10:00:00Z',
    'active_agents': 0,
    'requests_per_sec': 0.0,
    'queue_depth': 0,
    'total_jobs': 0,
    'running_jobs': 0,
    'pending_jobs': 0,
  };

  @override
  Future<List<Map<String, dynamic>>> getSkillsRaw({String? category}) async =>
      [];

  @override
  Future<List<Map<String, dynamic>>> listBranches(String projectId) async => [];

  @override
  Future<List<Map<String, dynamic>>> listProjects() async => [];

  @override
  Future<List<Map<String, dynamic>>> getReflectionProposalsRaw() async => [];

  @override
  Future<List<Map<String, dynamic>>> listPromptsRaw() async => [];
}

/// Reports a live connection so the metrics notifier subscribes to WS
/// instead of starting its poll timer (keeps the test timer-free).
class _StubWebSocket extends WebSocketService {
  _StubWebSocket() : super(host: 'localhost', port: 8081);

  @override
  Future<void> connect({String? path}) async {}
  @override
  void disconnect() {}
  @override
  void send(Map<String, dynamic> message) {}

  @override
  bool get isConnected => true;

  @override
  Stream<Map<String, dynamic>> subscribeToMetrics() =>
      const Stream<Map<String, dynamic>>.empty();
}

List<Override> get _stubs => [
  sdkClientProvider.overrideWith((_) => _StubSdkClient()),
  websocketProvider.overrideWith((_) => _StubWebSocket()),
];

Widget _hostedPanel(Widget panel) => ProviderScope(
  overrides: _stubs,
  child: MaterialApp(home: Scaffold(body: panel)),
);

/// Router with the panel on its own route and a marker on `/`, matching
/// how a menu pick opens a panel in either home layout.
GoRouter _routedPanel(Widget panel) => GoRouter(
  initialLocation: '/tools/memory',
  routes: [
    GoRoute(
      path: '/',
      builder: (_, __) => const Scaffold(body: Text('chat home marker')),
    ),
    GoRoute(
      path: '/tools/memory',
      builder: (_, __) => Scaffold(body: panel),
    ),
  ],
);

/// Menu tool name -> the panel widget that renders it.
///
/// Every menu entry is here: the shared-chrome test walks this map, so a tool
/// that stopped rendering the shell would fail it. `prompts` used to be
/// absent (it built its own header and esc) and is now wrapped in the same
/// shell as the rest.
final Map<String, Widget Function()> _panelBuilders = {
  'memory': () => const MemoryPanel(),
  'changes': () => const ChangesPanel(),
  'calendar': () => const CalendarPanel(),
  'metrics': () => const MetricsPanel(),
  'prompts': () => const PromptPanel(),
  'settings': () => const SettingsPanel(),
  'skills': () => const SkillPanel(),
  'branches': () => const BranchesPanel(),
  'search': () => const SearchPanel(),
  'reflection': () => const ReflectionPanel(),
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'api_port': 8081,
      'api_host': 'localhost',
    });
    await StorageService.instance.init();
  });

  void useRoomyViewport(WidgetTester tester) {
    tester.view.physicalSize = const Size(1400, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
  }

  group('tool menu', () {
    testWidgets('lists exactly the tools that have panels', (tester) async {
      final picked = <String>[];
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: HamburgerMenu(onToolSelected: picked.add)),
        ),
      );
      await tester.pump();

      await tester.tap(find.byIcon(Icons.menu));
      await tester.pump();

      for (final tool in HamburgerMenu.knownToolNames) {
        expect(find.text(tool), findsOneWidget, reason: 'menu entry $tool');
      }
      // Exact menu contents: this pins the whole GUI tool surface, so a
      // removed tool (the terminal panel, the files panel) cannot creep
      // back in without failing here.
      expect(
        HamburgerMenu.knownToolNames,
        orderedEquals([
          'memory',
          'changes',
          'calendar',
          'metrics',
          'prompts',
          'settings',
        ]),
      );

      await tester.tap(find.text('memory'));
      await tester.pump();
      expect(picked, ['memory']);
    });

    testWidgets('every menu entry has a panel with the shared back control', (
      tester,
    ) async {
      useRoomyViewport(tester);
      for (final tool in HamburgerMenu.knownToolNames) {
        final builder = _panelBuilders[tool];
        expect(builder, isNotNull, reason: '$tool has no panel in this test');
        await tester.pumpWidget(_hostedPanel(builder!()));
        await tester.pump(const Duration(milliseconds: 50));
        expect(
          find.byTooltip('back (esc)'),
          findsOneWidget,
          reason: '$tool panel is missing the shared back control',
        );
      }
    });

    testWidgets(
      'every panel route vetoes a system pop through the shared guard',
      (tester) async {
        // The browser Back button and the OS back gesture reach the router
        // through RouterDelegate.popRoute, never through the shared back
        // control, so the veto has to sit on the route (GoRoute.onExit) for
        // every route a panel can be open on - and it has to be the shared
        // guard, not just any callback: an `onExit` that always allows would
        // satisfy a null-check while vetoing nothing.
        final panelRoutes = router.configuration.routes
            .whereType<GoRoute>()
            .where(
              (route) =>
                  route.path.startsWith('/tools/') || route.path == '/settings',
            );
        expect(panelRoutes, isNotEmpty);
        for (final route in panelRoutes) {
          expect(
            route.onExit,
            same(guardRouteExit),
            reason: '${route.path} does not veto through the shared guard',
          );
        }
      },
    );

    testWidgets('every menu entry resolves to a registered router path', (
      tester,
    ) async {
      // toolRouteFor is the single resolver both home layouts open menu
      // picks through (openToolFromMenu); every menu entry must land on a
      // real route or the shared back control has nowhere to return to.
      final paths = router.configuration.routes
          .whereType<GoRoute>()
          .map((route) => route.path)
          .toSet();

      for (final tool in HamburgerMenu.knownToolNames) {
        final path = toolRouteFor(tool);
        expect(path, isNotNull, reason: '$tool has no route');
        expect(
          paths,
          contains(path),
          reason: '$tool maps to $path which is not in core/router.dart',
        );
      }
    });

    testWidgets('esc exits a real routed tool panel back to chat', (
      tester,
    ) async {
      useRoomyViewport(tester);
      await tester.pumpWidget(
        ProviderScope(
          overrides: _stubs,
          child: MaterialApp.router(
            routerConfig: _routedPanel(const MemoryPanel()),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('chat home marker'), findsNothing);

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
    });

    testWidgets('the router exposes exactly the expected tool routes', (
      tester,
    ) async {
      final paths = router.configuration.routes
          .whereType<GoRoute>()
          .map((route) => route.path)
          .toSet();
      // Exact set: pins the tool-route surface, so the removed terminal
      // (and files) routes cannot come back unnoticed.
      expect(
        paths.where((path) => path.startsWith('/tools/')).toSet(),
        equals({
          '/tools/search',
          '/tools/branches',
          '/tools/skills',
          '/tools/memory',
          '/tools/reflection',
          '/tools/changes',
          '/tools/prompts',
          '/tools/calendar',
          '/tools/metrics',
        }),
      );
      // settings is the one menu entry that is not under /tools.
      expect(paths, contains('/settings'));
      expect(toolRouteFor('settings'), '/settings');
    });
  });
}
