// 05-navigation TEST-GATE T6 (leaf 00T-gate.md, matrix F12c; leaf
// 03-restore-queue-visibility T1; F-ORCH-7, notes-plan-hitl F2/F3).
//
// Pins the GUI half of the aligned restore policy: on (re)connect the
// top-tabs HomeScreen must honor session.auto_resume exactly like the
// sidebar layout and the TUI:
//   - auto_resume=true: restore the LAST session (most recent activity),
//     not "reuse any empty session or create a new one".
//   - auto_resume=false: explicit empty start - no session auto-selected.
//
// RED today: home_screen.dart _onConnectionChanged never reads
// renderingPrefsProvider.autoResume (sidebar does) and picks
// findReusableEmptySession() ?? createSession - the most-recent session
// with real history is never restored (policy divergence, F-ORCH-7).
//
// Harness: the real HomeScreen on a router with stubbed SDK/WS (same
// pattern as tool_exit_guard_test.dart); connection flips through
// connectionStateProvider after mount so the one-time initial selection
// runs against the seeded session list.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/home/home_screen.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/rendering_prefs_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

/// Offline stub serving two sessions: an OLD empty one (the reuse trap)
/// and the MOST RECENT one with real history (the restore target).
class _RestoreStubClient extends SdkApiClient {
  _RestoreStubClient() : super(host: 'localhost', port: 65432);

  final created = <String>[];

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async => [
    {
      'id': 'sess-last',
      'name': 'last real session',
      'created_at': '2026-11-18T09:00:00Z',
      'last_activity': '2026-11-19T09:00:00Z',
      'leaf_message_id': 42, // has history: NOT reusable-empty
      'project_path': '/test/project',
    },
    {
      'id': 'sess-empty-old',
      'name': 'old empty session',
      'created_at': '2026-11-17T09:00:00Z',
      'last_activity': '2026-11-17T09:00:00Z',
      'leaf_message_id': null,
    },
  ];

  @override
  Future<Map<String, dynamic>> createSession({
    required String title,
    String? agentId,
    String? cwd,
    String? projectId,
  }) async {
    created.add(title);
    return {
      'id': 'sess-created',
      'name': title,
      'created_at': '2026-11-19T10:00:00Z',
    };
  }

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async => [];

  @override
  Future<List<Map<String, dynamic>>> listAgents() async => [];

  @override
  Future<List<Map<String, dynamic>>> listProjects() async => [];

  @override
  Future<String> getClientConfig() async => '';
}

class _ConnectedWs extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

GoRouter _homeRouter() => GoRouter(
  initialLocation: '/',
  routes: [
    GoRoute(path: '/', builder: (_, __) => const HomeScreen()),
    GoRoute(
      path: '/sessions',
      builder: (_, __) => const Scaffold(body: Text('sessions marker')),
    ),
    GoRoute(
      path: '/plans',
      builder: (_, __) => const Scaffold(body: Text('plans marker')),
    ),
    GoRoute(
      path: '/tasks',
      builder: (_, __) => const Scaffold(body: Text('tasks marker')),
    ),
    GoRoute(
      path: '/agents',
      builder: (_, __) => const Scaffold(body: Text('agents marker')),
    ),
    GoRoute(
      path: '/queue',
      builder: (_, __) => const Scaffold(body: Text('queue marker')),
    ),
  ],
);

/// Pumps the real HomeScreen DISCONNECTED, then flips the connection so
/// the one-time initial-selection path runs against the seeded list.
Future<ProviderContainer> _pumpAndConnect(
  WidgetTester tester, {
  required bool autoResume,
}) async {
  tester.view.physicalSize = const Size(1600, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);

  final container = ProviderContainer(
    overrides: [
      sdkClientProvider.overrideWith((_) => _RestoreStubClient()),
      websocketProvider.overrideWith((_) => _ConnectedWs()),
    ],
  );
  container.read(renderingPrefsProvider.notifier).setAutoResume(autoResume);

  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: _homeRouter()),
    ),
  );
  await tester.pumpAndSettle();

  // Relaunch semantics: the connection arrives after mount. Bounded
  // pumps (not pumpAndSettle): the ConnectionMonitor holds a 500ms
  // periodic timer that never settles; the initial-selection future
  // chain completes within a few frames against the instant stub.
  container.read(connectionStateProvider.notifier).state = true;
  for (var i = 0; i < 8; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
  return container;
}

Future<void> _dispose(WidgetTester tester, ProviderContainer container) async {
  await tester.pumpWidget(const SizedBox());
  container.dispose();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GUI restore policy alignment (05-navigation T6)', () {
    testWidgets('TestRestorePolicyAligned: auto_resume=true restores last', (
      tester,
    ) async {
      final container = await _pumpAndConnect(tester, autoResume: true);

      final active = container.read(activeSessionProvider);
      expect(
        active?.id,
        'sess-last',
        reason:
            'auto_resume=true did not restore the most-recent session '
            '(top-tabs reuses an empty session / creates new instead; '
            'policy diverges from sidebar + TUI, F-ORCH-7)',
      );

      await _dispose(tester, container);
    });

    testWidgets('auto_resume=false: explicit empty start', (tester) async {
      final container = await _pumpAndConnect(tester, autoResume: false);

      expect(
        container.read(activeSessionProvider),
        isNull,
        reason:
            'auto_resume=false still auto-selected a session '
            '(top-tabs never reads renderingPrefsProvider.autoResume; '
            'sidebar honors it - policy divergence)',
      );

      await _dispose(tester, container);
    });
  });
}
