import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/home/session_info_overlay.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/session_notifier.dart';

import '../../mocks/mock_websocket_service.dart';

/// Finds a substring rendered as a plain [Text] or inside a [RichText] span.
///
/// The canonical C1 agent roster line is built from TextSpans, so a plain
/// `find.text` cannot see it (flutter-agent-testing pitfall: RichText finder).
Finder findSpanText(String needle) => find.byWidgetPredicate((Widget w) {
  if (w is RichText) return w.text.toPlainText().contains(needle);
  if (w is Text) {
    return (w.data ?? '').contains(needle) ||
        (w.textSpan?.toPlainText() ?? '').contains(needle);
  }
  return false;
});

void main() {
  const testWindow = Size(1200, 900);
  final session = Session(
    id: 'session-1',
    title: 'info test',
    createdAt: DateTime.utc(2026, 1, 1),
  );

  /// Pump the popup with a fully stubbed provider graph so no test touches
  /// the network. The popup is the Scaffold body (not a showDialog route) so
  /// the fixed 800x560 Container is exercised directly: a full-window fix
  /// makes the content stack grow to nearly the whole 1200x900 test window.
  Future<void> pumpOverlay(
    WidgetTester tester, {
    required _StubSdkClient client,
  }) async {
    tester.view.physicalSize = testWindow;
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    final ws = MockWebSocketService();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sdkClientProvider.overrideWith((ref) => client),
          websocketProvider.overrideWith((ref) => ws),
          activeSessionProvider.overrideWith((ref) => session),
          sessionProvider.overrideWith(
            (ref) => SessionNotifier(sdkClient: client, websocket: ws),
          ),
          planProvider.overrideWith((ref) => PlanNotifier(sdkClient: client)),
          taskProvider.overrideWith((ref) => TaskNotifier(sdkClient: client)),
          agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: client)),
        ],
        child: MaterialApp(
          home: Scaffold(body: SessionInfoOverlay(session: session)),
        ),
      ),
    );
    // initState post-frame callbacks load sessions / plans / tasks / agents.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));
  }

  Future<void> tapTab(WidgetTester tester, String label) async {
    // The custom _TabBar label is the first exact `label` Text in the tree.
    await tester.tap(find.text(label).first);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400)); // finish animateTo
  }

  group('SessionInfoOverlay', () {
    testWidgets('T1: popup fills the full window, not a fixed 800x560 box', (
      tester,
    ) async {
      await pumpOverlay(tester, client: _StubSdkClient());

      expect(find.byType(SessionInfoOverlay), findsOneWidget);
      expect(find.text('plans'), findsWidgets, reason: 'tab bar visible');

      // The content stack lives inside the popup body. With the old fixed
      // 800x560 Container it is only ~800 wide and ~480 tall; once the
      // popup fills the window it grows to nearly 1200x900.
      final stack = find.byType(IndexedStack);
      expect(stack, findsOneWidget);
      final size = tester.getSize(stack);
      expect(
        size.width,
        greaterThan(900),
        reason:
            'popup content must be wider than the old fixed 800px box '
            '(was ${size.width})',
      );
      expect(
        size.height,
        greaterThan(600),
        reason:
            'popup content must be taller than the old fixed 560px box '
            '(was ${size.height})',
      );
    });

    testWidgets(
      'T2a: plans tab shows seeded plans via the canonical PlansTab',
      (tester) async {
        // A plan belonging to a DIFFERENT session: the popup's private
        // filtered copy would hide it, the canonical PlansTab shows it.
        final plan = Plan(
          id: 'plan-c1',
          title: 'Canonical Plan Row',
          description: '',
          state: 'draft',
          createdAt: DateTime.utc(2026, 1, 1),
          updatedAt: DateTime.utc(2026, 1, 1),
          sourceSession: 'a-different-session',
        );

        await pumpOverlay(tester, client: _StubSdkClient(plans: [plan]));
        await tapTab(tester, 'plans');

        expect(
          find.text('canonical plan row'),
          findsWidgets,
          reason: 'canonical PlansTab renders all seeded plans (lowercased)',
        );
        expect(
          find.text('no plans for this session'),
          findsNothing,
          reason: 'popup must not use its own session-filtered private copy',
        );
      },
    );

    testWidgets(
      'T2b: tasks tab shows seeded tasks via the canonical TasksTab',
      (tester) async {
        final task = Task(
          id: 'task-c1',
          title: 'Canonical Task Row',
          description: '',
          status: 'pending',
          sessionId: 'a-different-session',
          createdAt: DateTime.utc(2026, 1, 1),
        );

        await pumpOverlay(tester, client: _StubSdkClient(tasks: [task]));
        await tapTab(tester, 'tasks');

        expect(
          find.text('canonical task row'),
          findsWidgets,
          reason: 'canonical TasksTab renders all seeded tasks (lowercased)',
        );
        expect(
          find.text('no tasks for this session'),
          findsNothing,
          reason: 'popup must not use its own session-filtered private copy',
        );
      },
    );

    testWidgets('T2c: agents tab renders C1 type/active/cost text', (
      tester,
    ) async {
      // C1 RosterEntry-shaped rows: type, active_count, daily_cost_cents.
      await pumpOverlay(
        tester,
        client: _StubSdkClient(
          agentRows: [
            {
              'id': 'bot-1',
              'name': 'coder',
              'type': 'bot',
              'status': 'running',
              'active_count': 13,
              'daily_cost_cents': 1234,
              'enabled': true,
            },
          ],
        ),
      );
      await tapTab(tester, 'agents');
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      expect(
        findSpanText('bot'),
        findsWidgets,
        reason: 'agent type from the C1 row must be rendered',
      );
      expect(
        findSpanText('13'),
        findsWidgets,
        reason: 'active_count from the C1 row must be rendered',
      );
      expect(
        findSpanText('12.34'),
        findsWidgets,
        reason: 'daily_cost_cents (1234) must be rendered as dollars',
      );
    });
  });
}

/// Stub [SdkApiClient]: overrides only the list endpoints this popup's
/// providers call, returning model rows seeded by the test. Signatures mirror
/// the real client so the stub cannot silently drift from the production API.
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient({
    List<Plan> plans = const [],
    List<Task> tasks = const [],
    List<Map<String, dynamic>> agentRows = const [],
  }) : _plans = plans,
       _tasks = tasks,
       _agentRows = agentRows,
       super(host: 'localhost', port: 65432);

  final List<Plan> _plans;
  final List<Task> _tasks;
  final List<Map<String, dynamic>> _agentRows;

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async =>
      const [];

  @override
  Future<List<Map<String, dynamic>>> listPlansBySession(
    String sessionId,
  ) async => _plans.map((p) => p.toJson()).toList();

  @override
  Future<List<Map<String, dynamic>>> listPlans({
    String? projectId,
    int limit = 50,
  }) async => _plans.map((p) => p.toJson()).toList();

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async =>
      _tasks.map((t) => t.toJson()).toList();

  @override
  Future<List<Map<String, dynamic>>> listAgents() async =>
      _agentRows.map((a) => Map<String, dynamic>.from(a)).toList();

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async =>
      // T2c seeds C1 RosterEntry-shaped rows; serve them on the unified
      // roster route (M17) that loadAgents now reads first.
      {'agents': _agentRows.map((a) => Map<String, dynamic>.from(a)).toList()};

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}
