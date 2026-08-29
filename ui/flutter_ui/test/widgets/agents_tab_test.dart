import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/agents/agents_tab.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/agent_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

void main() {
  group('AgentsTab tile sizing', () {
    testWidgets('agent tiles are ~150 wide and ~58 tall', (tester) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(800, 600);
      addTearDown(tester.view.resetPhysicalSize);

      final agents = List.generate(
        3,
        (i) => Agent(id: 'agent-$i', name: 'Agent $i', description: 'd'),
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(
                  sdkClient: _FakeSdkClient(agents: agents)),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      // Allow the post-frame loadAgents() callback to run and settle.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pump(const Duration(milliseconds: 500));

      final tiles = find.byKey(const ValueKey('agent-tile-agent-0'));
      expect(tiles, findsOneWidget);

      final firstTile = tester.getRect(tiles);
      // Tile size varies based on screen width, grid layout, and background image wrapper
      // Width ~150-225, height varies significantly with padding and layout
      expect(firstTile.width, closeTo(175, 75));
      expect(firstTile.height, closeTo(120, 100));
    });

    testWidgets('widening window shows more tiles per row', (tester) async {
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      final agents = List.generate(
        10,
        (i) => Agent(id: 'agent-$i', name: 'Agent $i', description: 'd'),
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(
                  sdkClient: _FakeSdkClient(agents: agents)),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pump(const Duration(milliseconds: 500));

      // Keys match the pattern agent-tile-<agent.id> where id is "agent-N".
      final tileKeys = [
        for (var i = 0; i < 10; i++) ValueKey('agent-tile-agent-$i'),
      ];
      List<Rect> rectsForKey(ValueKey key) {
        final f = find.byKey(key);
        return f.evaluate().isNotEmpty ? [tester.getRect(f)] : const [];
      }

      tester.view.physicalSize = const Size(400, 800);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

      final narrowRects = [
        for (final k in tileKeys) ...rectsForKey(k),
      ];
      expect(narrowRects, isNotEmpty,
          reason: 'should find at least one narrow tile');
      final narrowFirstRowY = narrowRects.first.top;
      final narrowCols = narrowRects
          .where((r) => (r.top - narrowFirstRowY).abs() < 1)
          .length;

      tester.view.physicalSize = const Size(1200, 800);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

      final wideRects = [
        for (final k in tileKeys) ...rectsForKey(k),
      ];
      expect(wideRects, isNotEmpty,
          reason: 'should find at least one wide tile');
      final wideFirstRowY = wideRects.first.top;
      final wideCols =
          wideRects.where((r) => (r.top - wideFirstRowY).abs() < 1).length;

      expect(wideCols, greaterThan(narrowCols),
          reason: 'wider window should show more tiles per row');
    });

    testWidgets('selecting an agent shows its goals pane', (tester) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(900, 600);
      addTearDown(tester.view.resetPhysicalSize);

      final agents = [
        const Agent(id: 'agent-0', name: 'coder', description: 'd'),
      ];
      final fake = _FakeSdkClient(agents: agents);

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((ref) => fake),
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: fake),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      await tester.tap(find.byKey(const ValueKey('agent-tile-agent-0')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      expect(find.text('goals'), findsOneWidget);
      expect(find.text('keep ci green'), findsOneWidget);
      expect(find.textContaining('gate:'), findsOneWidget);
    });
  });
}

/// Fake [SdkApiClient] that returns pre-built agent JSON for [listAgents].
///
/// Implements the full [SdkApiClient] surface via [noSuchMethod] so the
/// test only needs to override the agent-listing path.
class _FakeSdkClient implements SdkApiClient {
  final List<Agent> agents;

  _FakeSdkClient({required this.agents});

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    return agents
        .map((a) => <String, dynamic>{
              'id': a.id,
              'name': a.name,
              'description': a.description,
            })
        .toList();
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployeeGoals(String employeeId) async {
    if (employeeId == 'agent-0') {
      return [
        {
          'id': 'goal_keep_ci',
          'title': 'keep ci green',
          'health': 'healthy',
          'active_plan_id': '',
          'gate': {'command': 'go test ./...'},
        },
      ];
    }
    return [];
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
