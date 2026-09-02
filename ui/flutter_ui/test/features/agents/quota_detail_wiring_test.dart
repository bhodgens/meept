import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/agents/agents_tab.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/agent_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Adversarial-review tests for the quota detail lines wiring in the agents
/// tab goals pane (quota-reset-resilience leaf 09 review). Complements
/// quota_status_test.dart (owned by the leaf implementer); kept separate per
/// review rules so both files can evolve independently.

/// Fake SdkApiClient for tests.
class _FakeSdkClient implements SdkApiClient {
  final List<Agent> agents;

  _FakeSdkClient({this.agents = const []});

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    return agents
        .map(
          (a) => <String, dynamic>{
            'id': a.id,
            'name': a.name,
            'description': a.description,
          },
        )
        .toList();
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployeeGoals(String employeeId) =>
      Future.value(const []);

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Future<void> _pumpAgentsTab(
  WidgetTester tester,
  List<Agent> agents,
  Map<String, AgentQuotaState> quotaEpisodes,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        agentProvider.overrideWith(
          (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents))
            ..state = AgentState(agents: agents, quotaEpisodes: quotaEpisodes),
        ),
      ],
      child: const MaterialApp(home: Scaffold(body: AgentsTab())),
    ),
  );
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 100));
  if (agents.isNotEmpty) {
    // Select the first agent so the goals pane (which carries the quota
    // detail lines) is rendered.
    await tester.tap(find.byKey(ValueKey('agent-tile-${agents.first.id}')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('agents tab quota detail lines (goals pane)', () {
    testWidgets('fallback active renders primary/active lines', (tester) async {
      final agents = [const Agent(id: 'agent-1', name: 'coder')];
      final quotaState = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: DateTime.now()
            .add(const Duration(hours: 3))
            .millisecondsSinceEpoch,
        fallbackModel: 'glm-4.7',
      );

      await _pumpAgentsTab(tester, agents, {'agent-1': quotaState});

      // The goals pane shows the two parity lines only when a fallback is
      // carrying work. The primary model is not stored in AgentQuotaState,
      // so the primary line degrades to "unknown" by design.
      expect(
        find.textContaining('primary: unknown (blocked until '),
        findsOneWidget,
      );
      expect(find.text('active: glm-4.7'), findsOneWidget);
    });

    testWidgets('no fallback renders no detail lines', (tester) async {
      final agents = [const Agent(id: 'agent-1', name: 'coder')];
      final quotaState = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: DateTime.now()
            .add(const Duration(hours: 3))
            .millisecondsSinceEpoch,
      );

      await _pumpAgentsTab(tester, agents, {'agent-1': quotaState});

      // Leaf 04 wait label still present on the tile...
      expect(find.textContaining('quota_wait · '), findsOneWidget);
      // ...but no primary/active detail lines without a fallback.
      expect(find.textContaining('primary: '), findsNothing);
      expect(find.textContaining('active: '), findsNothing);
    });

    testWidgets('no quota episode renders no quota chrome at all', (
      tester,
    ) async {
      final agents = [
        const Agent(id: 'agent-1', name: 'coder'),
        const Agent(id: 'agent-2', name: 'dispatcher'),
      ];

      await _pumpAgentsTab(tester, agents, const {});

      expect(find.textContaining('quota_wait · '), findsNothing);
      expect(find.textContaining('blocked'), findsNothing);
      expect(find.textContaining('primary: '), findsNothing);
    });

    testWidgets('long model ids ellipsize without overflow', (tester) async {
      final agents = [const Agent(id: 'agent-1', name: 'coder')];
      const longModel = 'anthropic/claude-opus-4-20260831-us-east-fallback';
      final quotaState = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: DateTime.now()
            .add(const Duration(hours: 3))
            .millisecondsSinceEpoch,
        fallbackModel: longModel,
      );

      await _pumpAgentsTab(tester, agents, {'agent-1': quotaState});

      expect(
        tester.takeException(),
        isNull,
        reason: 'long fallback model id must not crash layout',
      );
      expect(find.textContaining('active: '), findsOneWidget);
    });
  });
}
