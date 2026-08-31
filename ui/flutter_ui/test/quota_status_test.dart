import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/agents/agents_tab.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/agent_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/utils/format_duration.dart';

void main() {
  group('formatDuration', () {
    test('returns resuming… for negative durations', () {
      // Dart normalises Duration(hours: -1) to positive, so construct
      // a truly negative duration via microseconds.
      expect(formatDuration(const Duration(milliseconds: -1)), equals('resuming…'));
      expect(formatDuration(const Duration(hours: -1, minutes: -30)), equals('resuming…'));
      expect(formatDuration(const Duration(days: -1)), equals('resuming…'));
    });

    test('formats hours+minutes >= 1h', () {
      expect(
        formatDuration(const Duration(hours: 3, minutes: 12)),
        equals('3 h 12 m'),
      );
      expect(formatDuration(const Duration(hours: 1)), equals('1 h'));
      expect(formatDuration(const Duration(hours: 2)), equals('2 h'));
      expect(formatDuration(const Duration(hours: 5, minutes: 0)), equals('5 h'));
      expect(
        formatDuration(const Duration(hours: 12, minutes: 30)),
        equals('12 h 30 m'),
      );
    });

    test('formats minutes >= 1m', () {
      expect(formatDuration(const Duration(minutes: 45)), equals('45 m'));
      expect(formatDuration(const Duration(minutes: 1)), equals('1 m'));
      expect(formatDuration(const Duration(minutes: 59)), equals('59 m'));
    });

    test('formats seconds < 1m', () {
      expect(formatDuration(const Duration(seconds: 59)), equals('59 s'));
      expect(formatDuration(const Duration(seconds: 5)), equals('5 s'));
      expect(formatDuration(const Duration(seconds: 1)), equals('1 s'));
      expect(formatDuration(const Duration(seconds: 0)), equals('0 s'));
    });

    test('boundary cases', () {
      // Exactly 1 minute
      expect(formatDuration(const Duration(minutes: 1)), equals('1 m'));
      // Just under 1 minute
      expect(formatDuration(const Duration(seconds: 59)), equals('59 s'));
      // Exactly 1 hour
      expect(formatDuration(const Duration(hours: 1)), equals('1 h'));
      // Just under 1 hour
      expect(
        formatDuration(const Duration(minutes: 59, seconds: 59)),
        equals('59 m'),
      );
    });
  });

  group('AgentQuotaState', () {
    test('default values', () {
      const state = AgentQuotaState(quotaBlocked: false);
      expect(state.quotaBlocked, isFalse);
      expect(state.quotaWaitUntilEpoch, isNull);
    });

    test('copyWith preserves unprovided fields', () {
      const original = AgentQuotaState(
        quotaBlocked: true,
        quotaWaitUntilEpoch: 123456789,
      );
      final copy = original.copyWith(quotaBlocked: false);
      expect(copy.quotaBlocked, isFalse);
      expect(copy.quotaWaitUntilEpoch, 123456789);
    });
  });

  group('AgentNotifier.handleQuotaEvent', () {
    test('quota_wait sets episode with wait until', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();
      expect(notifier.state.quotaEpisodes, isEmpty);

      notifier.handleQuotaEvent(
        agentId: 'agent-1',
        to: 'quota_wait',
        unblockAt: '2026-08-31T12:00:00Z',
      );

      final state = notifier.state;
      expect(state.quotaEpisodes.containsKey('agent-1'), isTrue);
      final ep = state.quotaEpisodes['agent-1']!;
      expect(ep.quotaBlocked, isFalse);
      expect(ep.quotaWaitUntilEpoch, isNotNull);
    });

    test('blocked sets episode with blocked=true', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      notifier.handleQuotaEvent(
        agentId: 'agent-2',
        to: 'blocked',
        unblockAt: '2026-08-31T14:00:00Z',
      );

      final state = notifier.state;
      final ep = state.quotaEpisodes['agent-2']!;
      expect(ep.quotaBlocked, isTrue);
      expect(ep.quotaWaitUntilEpoch, isNotNull);
    });

    test('running clears episode', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      // Set an episode first
      notifier.handleQuotaEvent(
        agentId: 'agent-3',
        to: 'quota_wait',
        unblockAt: '2026-08-31T12:00:00Z',
      );
      expect(notifier.state.quotaEpisodes.containsKey('agent-3'), isTrue);

      // Clear it
      notifier.handleQuotaEvent(
        agentId: 'agent-3',
        to: 'running',
      );
      expect(notifier.state.quotaEpisodes.containsKey('agent-3'), isFalse);
    });

    test('unknown transition is ignored', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      notifier.handleQuotaEvent(
        agentId: 'agent-4',
        to: 'unknown_state',
      );

      expect(notifier.state.quotaEpisodes, isEmpty);
    });

    test('clears existing episode for same agent on new event', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      // First event
      notifier.handleQuotaEvent(
        agentId: 'agent-5',
        to: 'quota_wait',
        unblockAt: '2026-08-31T10:00:00Z',
      );
      var ep = notifier.state.quotaEpisodes['agent-5']!;
      expect(ep.quotaWaitUntilEpoch, isNotNull);
      expect(ep.quotaBlocked, isFalse);

      // Second event for same agent
      notifier.handleQuotaEvent(
        agentId: 'agent-5',
        to: 'blocked',
        unblockAt: '2026-08-31T20:00:00Z',
      );
      ep = notifier.state.quotaEpisodes['agent-5']!;
      expect(ep.quotaBlocked, isTrue);
      // Unblock time should be updated
      expect(ep.quotaWaitUntilEpoch, isNot(null));
    });
  });

  group('AgentProgress with quota payload', () {
    test('parses quota_wait event', () {
      final json = {
        'type': 'agent_progress',
        'agent_id': 'agent-1',
        'message': 'quota wait',
        'tier': 1,
        'to': 'quota_wait',
        'unblock_at': '2026-08-31T12:00:00Z',
        'escalation': 'warn',
        'timestamp': '2026-08-31T10:00:00Z',
      };
      final progress = AgentProgress.fromJson(json);
      expect(progress.quota, isNotNull);
      expect(progress.quota!.agentId, 'agent-1');
      expect(progress.quota!.to, 'quota_wait');
      expect(progress.quota!.unblockAt, '2026-08-31T12:00:00Z');
      expect(progress.quota!.escalation, 'warn');
    });

    test('parses blocked event', () {
      final json = {
        'type': 'agent_progress',
        'agent_id': 'agent-2',
        'message': 'blocked',
        'tier': 1,
        'to': 'blocked',
        'unblock_at': '2026-08-31T20:00:00Z',
        'escalation': 'blocked',
        'timestamp': '2026-08-31T10:00:00Z',
      };
      final progress = AgentProgress.fromJson(json);
      expect(progress.quota, isNotNull);
      expect(progress.quota!.to, 'blocked');
    });

    test('parses running/clear event', () {
      final json = {
        'type': 'agent_progress',
        'agent_id': 'agent-3',
        'message': 'running again',
        'tier': 1,
        'to': 'running',
        'timestamp': '2026-08-31T11:00:00Z',
      };
      final progress = AgentProgress.fromJson(json);
      expect(progress.quota, isNotNull);
      expect(progress.quota!.to, 'running');
    });

    test('no quota when to is absent', () {
      final json = {
        'type': 'agent_progress',
        'agent_id': 'agent-4',
        'message': 'thinking',
        'tier': 1,
        'timestamp': '2026-08-31T10:00:00Z',
      };
      final progress = AgentProgress.fromJson(json);
      expect(progress.quota, isNull);
    });

    test('malformed quota payload does not crash', () {
      final json = {
        'type': 'agent_progress',
        'agent_id': 'agent-5',
        'message': 'some message',
        'tier': 1,
        'timestamp': '2026-08-31T10:00:00Z',
      };
      // Should not throw
      expect(() => AgentProgress.fromJson(json), returnsNormally);
    });
  });

  group('Agent card with quota', () {
    testWidgets('shows quota badge on agent tile', (tester) async {
      final quotaState = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: DateTime.now()
            .add(const Duration(hours: 1))
            .millisecondsSinceEpoch,
      );
      final agents = [
        const Agent(id: 'agent-1', name: 'coder'),
        const Agent(id: 'agent-2', name: 'dispatcher'),
      ];

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents))
                ..state = AgentState(
                  agents: agents,
                  quotaEpisodes: {'agent-1': quotaState},
                ),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      // Should find "quota resets in" text
      expect(find.textContaining('quota resets in'), findsOneWidget);
    });

    testWidgets('shows blocked badge on agent tile', (tester) async {
      final quotaState = const AgentQuotaState(
        quotaBlocked: true,
      );
      final agents = [
        const Agent(id: 'agent-1', name: 'coder'),
      ];

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents))
                ..state = AgentState(
                  agents: agents,
                  quotaEpisodes: {'agent-1': quotaState},
                ),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      // Should find "blocked" text
      expect(find.textContaining('blocked'), findsOneWidget);
      expect(find.textContaining('action required'), findsOneWidget);
    });

    testWidgets('no quota badge when no quota state', (tester) async {
      final agents = [
        const Agent(id: 'agent-1', name: 'coder'),
      ];

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents)),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      // Should NOT find quota-related text
      expect(find.textContaining('quota resets in'), findsNothing);
      expect(find.textContaining('blocked'), findsNothing);
    });

    testWidgets('past-due shows resuming…', (tester) async {
      // Past due: unblock time in the past
      final quotaState = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: DateTime.now()
            .subtract(const Duration(hours: 1))
            .millisecondsSinceEpoch,
      );
      final agents = [
        const Agent(id: 'agent-1', name: 'coder'),
      ];

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents))
                ..state = AgentState(
                  agents: agents,
                  quotaEpisodes: {'agent-1': quotaState},
                ),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      // Should find "resuming…" text
      expect(find.textContaining('resuming'), findsOneWidget);
    });
  });
}

/// Fake SdkApiClient for tests.
class _FakeSdkClient implements SdkApiClient {
  final List<Agent> agents;

  _FakeSdkClient({this.agents = const []});

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
    return [];
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
