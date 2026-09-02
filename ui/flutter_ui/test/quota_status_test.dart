import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/agents/agents_tab.dart';
import 'package:meept_ui/features/agents/quota_status.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/agent_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/utils/format_duration.dart';

void main() {
  group('formatDuration', () {
    test('returns resuming… for negative durations', () {
      // Dart normalises Duration(hours: -1) to positive, so construct
      // a truly negative duration via milliseconds.
      expect(formatDuration(const Duration(milliseconds: -1)), equals('resuming…'));
      expect(formatDuration(const Duration(hours: -1, minutes: -30)), equals('resuming…'));
      expect(formatDuration(const Duration(days: -1)), equals('resuming…'));
    });

    test('formats hours+minutes >= 1h', () {
      expect(
        formatDuration(const Duration(hours: 3, minutes: 12)),
        equals('3h 12m'),
      );
      expect(formatDuration(const Duration(hours: 1)), equals('1h'));
      expect(formatDuration(const Duration(hours: 2)), equals('2h'));
      expect(formatDuration(const Duration(hours: 5, minutes: 0)), equals('5h'));
      expect(
        formatDuration(const Duration(hours: 12, minutes: 30)),
        equals('12h 30m'),
      );
    });

    test('formats minutes >= 1m', () {
      expect(formatDuration(const Duration(minutes: 45)), equals('45m'));
      expect(formatDuration(const Duration(minutes: 1)), equals('1m'));
      expect(formatDuration(const Duration(minutes: 59)), equals('59m'));
    });

    test('truncates sub-minute remains, never rounds up (parity)', () {
      expect(formatDuration(const Duration(seconds: 90)), equals('1m'));
      expect(formatDuration(const Duration(seconds: 30)), equals('0m'));
      expect(formatDuration(const Duration(seconds: 59)), equals('0m'));
    });

    test('boundary cases', () {
      // Exactly 1 minute
      expect(formatDuration(const Duration(minutes: 1)), equals('1m'));
      // Just under 1 hour
      expect(
        formatDuration(const Duration(minutes: 59, seconds: 59)),
        equals('59m'),
      );
      // Exactly 1 hour
      expect(formatDuration(const Duration(hours: 1)), equals('1h'));
      // Just under 1 hour via seconds
      expect(
        formatDuration(const Duration(seconds: 3599)),
        equals('59m'),
      );
    });
  });

  group('AgentQuotaState', () {
    test('default values', () {
      const state = AgentQuotaState(quotaBlocked: false);
      expect(state.quotaBlocked, isFalse);
      expect(state.quotaWaitUntilEpoch, isNull);
      expect(state.fallbackModel, isNull);
      expect(state.escalation, isNull);
    });

    test('copyWith preserves unprovided fields', () {
      const original = AgentQuotaState(
        quotaBlocked: true,
        quotaWaitUntilEpoch: 123456789,
        fallbackModel: 'glm-4.7',
        escalation: 'warn',
      );
      final copy = original.copyWith(quotaBlocked: false);
      expect(copy.quotaBlocked, isFalse);
      expect(copy.quotaWaitUntilEpoch, 123456789);
      expect(copy.fallbackModel, 'glm-4.7');
      expect(copy.escalation, 'warn');
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

    test('quota_wait stores fallback model when present', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      notifier.handleQuotaEvent(
        agentId: 'agent-fb',
        to: 'quota_wait',
        unblockAt: '2026-08-31T12:00:00Z',
        fallbackModel: 'glm-4.7',
      );

      final ep = notifier.state.quotaEpisodes['agent-fb']!;
      expect(ep.fallbackModel, 'glm-4.7');
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

    test('to="" tier escalation updates existing episode unblock time', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      // Enter quota wait (escalation is "" on initial entry).
      notifier.handleQuotaEvent(
        agentId: 'agent-esc',
        to: 'quota_wait',
        unblockAt: '2026-08-31T12:00:00Z',
      );
      final before = DateTime.parse('2026-08-31T12:00:00Z')
          .millisecondsSinceEpoch;
      var ep = notifier.state.quotaEpisodes['agent-esc']!;
      expect(ep.quotaWaitUntilEpoch, before);
      expect(ep.quotaBlocked, isFalse);

      // 12h warn tier fires with to == "" and an extended unblock time.
      notifier.handleQuotaEvent(
        agentId: 'agent-esc',
        to: '',
        unblockAt: '2026-08-31T13:30:00Z',
        escalation: 'warn',
      );
      ep = notifier.state.quotaEpisodes['agent-esc']!;
      expect(ep.quotaWaitUntilEpoch,
          DateTime.parse('2026-08-31T13:30:00Z').millisecondsSinceEpoch);
      // Episode persists — only the wait time/tier refreshed.
      expect(ep.quotaBlocked, isFalse);
      expect(ep.escalation, 'warn');
    });

    test('to="" with no existing episode is a no-op', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      notifier.handleQuotaEvent(
        agentId: 'agent-ghost',
        to: '',
        unblockAt: '2026-08-31T15:00:00Z',
        escalation: 'action_recommended',
      );

      expect(notifier.state.quotaEpisodes, isEmpty);
    });

    test('quota_wait stores escalation "" and later to="" warn stores "warn"', () async {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      await notifier.loadAgents();

      // Initial entry: escalation is "" (absent) — stored as null.
      notifier.handleQuotaEvent(
        agentId: 'agent-tier',
        to: 'quota_wait',
        unblockAt: '2026-08-31T12:00:00Z',
        escalation: '',
      );
      var ep = notifier.state.quotaEpisodes['agent-tier']!;
      expect(ep.escalation, isNull);

      // Later tier firing carries "warn".
      notifier.handleQuotaEvent(
        agentId: 'agent-tier',
        to: '',
        escalation: 'warn',
      );
      ep = notifier.state.quotaEpisodes['agent-tier']!;
      expect(ep.escalation, 'warn');
      // Unblock time kept from the original event (unblockAt was null here).
      expect(ep.quotaWaitUntilEpoch,
          DateTime.parse('2026-08-31T12:00:00Z').millisecondsSinceEpoch);
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
        'fallback_model': 'glm-4.7',
        'timestamp': '2026-08-31T10:00:00Z',
      };
      final progress = AgentProgress.fromJson(json);
      expect(progress.quota, isNotNull);
      expect(progress.quota!.agentId, 'agent-1');
      expect(progress.quota!.to, 'quota_wait');
      expect(progress.quota!.unblockAt, '2026-08-31T12:00:00Z');
      expect(progress.quota!.escalation, 'warn');
      expect(progress.quota!.fallbackModel, 'glm-4.7');
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
      expect(progress.quota!.fallbackModel, isNull);
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

  group('quotaCountdownText', () {
    test('formats remaining wait time in parity format', () {
      final nowMs = DateTime.now().millisecondsSinceEpoch;
      final state = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: nowMs + (3 * 60 + 12) * 60 * 1000,
      );
      expect(quotaCountdownText(state), equals('quota resets in 3h 12m'));
    });

    test('single hour and 45m', () {
      final nowMs = DateTime.now().millisecondsSinceEpoch;
      expect(
        quotaCountdownText(AgentQuotaState(
          quotaBlocked: false,
          quotaWaitUntilEpoch: nowMs + 60 * 60 * 1000,
        )),
        equals('quota resets in 1h'),
      );
      expect(
        quotaCountdownText(AgentQuotaState(
          quotaBlocked: false,
          quotaWaitUntilEpoch: nowMs + 45 * 60 * 1000,
        )),
        equals('quota resets in 45m'),
      );
    });

    test('past-due renders resets soon', () {
      final nowMs = DateTime.now().millisecondsSinceEpoch;
      final state = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: nowMs - 60 * 60 * 1000,
      );
      expect(quotaCountdownText(state), equals('resets soon'));
    });

    test('null wait time and blocked yield null', () {
      expect(
        quotaCountdownText(const AgentQuotaState(quotaBlocked: false)),
        isNull,
      );
      expect(
        quotaCountdownText(const AgentQuotaState(quotaBlocked: true)),
        isNull,
      );
    });
  });

  group('quotaDetailLines', () {
    test('two lines when fallback model is present', () {
      final lines = quotaDetailLines('claude-opus-4', 'glm-4.7', 1756627200000);
      expect(lines.length, 2);
      expect(lines[0], contains('primary: claude-opus-4 (blocked until '));
      expect(lines[1], equals('active: glm-4.7'));
    });

    test('empty when no fallback model', () {
      expect(quotaDetailLines('claude-opus-4', null, null), isEmpty);
      expect(quotaDetailLines('claude-opus-4', '', 1756627200000), isEmpty);
    });

    test('missing unblock time degrades to unknown', () {
      final lines = quotaDetailLines('claude-opus-4', 'glm-4.7', null);
      expect(lines[0], contains('(blocked until unknown)'));
    });
  });

  // Tree 03 leaf 04 Task 4: wait-label parity with the TUI's QuotaWaitLabel
  // (internal/tui/quota_status.go). Strings must stay byte-identical.
  group('quotaWaitLabel (leaf 04 parity)', () {
    // 2026-09-02 14:05 local — matches the TUI test's fixture.
    final unblock = DateTime(2026, 9, 2, 14, 5);

    int epochOf(DateTime t) => t.millisecondsSinceEpoch;

    test('quota class renders reset label', () {
      final state = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: epochOf(unblock),
        waitClass: 'quota',
      );
      expect(quotaWaitLabel(state), equals('quota_wait · reset 14:05'));
    });

    test('absent class (legacy event) defaults to reset label', () {
      final state = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: epochOf(unblock),
      );
      expect(quotaWaitLabel(state), equals('quota_wait · reset 14:05'));
    });

    test('throttle class renders throttle retry label', () {
      final state = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch: epochOf(unblock),
        waitClass: 'throttle',
      );
      expect(
        quotaWaitLabel(state),
        equals('quota_wait · throttle retry 14:05'),
      );
    });

    test('past-due throttle wait stays absolute (no relative math)', () {
      final state = AgentQuotaState(
        quotaBlocked: false,
        quotaWaitUntilEpoch:
            epochOf(DateTime(2026, 9, 1, 9, 1)),
        waitClass: 'throttle',
      );
      expect(
        quotaWaitLabel(state),
        equals('quota_wait · throttle retry 09:01'),
      );
    });

    test('blocked and no-wait states yield null', () {
      expect(
        quotaWaitLabel(const AgentQuotaState(quotaBlocked: true)),
        isNull,
      );
      expect(
        quotaWaitLabel(const AgentQuotaState(quotaBlocked: false)),
        isNull,
      );
      expect(quotaWaitLabel(null), isNull);
    });

    test('AgentQuotaPayload.fromJson maps class → waitClass', () {
      final payload = AgentQuotaPayload.fromJson({
        'agent_id': 'agent-1',
        'to': 'quota_wait',
        'reason': 'throttle_wait',
        'class': 'throttle',
        'resume_at': '2026-09-02T15:00:00Z',
      });
      expect(payload.waitClass, equals('throttle'));
    });

    test('handleQuotaEvent stores waitClass on the episode', () {
      final notifier = AgentNotifier(sdkClient: _FakeSdkClient());
      notifier.handleQuotaEvent(
        agentId: 'agent-wc',
        to: 'quota_wait',
        unblockAt: '2026-09-02T14:05:00Z',
        waitClass: 'throttle',
      );
      final ep = notifier.state.quotaEpisodes['agent-wc']!;
      expect(ep.waitClass, equals('throttle'));
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

      // Leaf 04 label: "quota_wait · reset HH:MM" on the tile.
      expect(find.textContaining('quota_wait · reset '), findsOneWidget);
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
      expect(find.textContaining('quota_wait · '), findsNothing);
      expect(find.textContaining('blocked'), findsNothing);
    });

    testWidgets('past-due shows absolute wait label', (tester) async {
      // Past due: unblock time in the past — leaf 04 renders the absolute
      // HH:MM regardless (no relative "resets soon" anymore).
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

      // Should find "quota_wait · reset HH:MM" text (past-due still
      // renders the absolute wait label)
      expect(find.textContaining('quota_wait · reset '), findsOneWidget);
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
