// Widget tests for the populated metrics panel.
//
// The panel reads [metricsProvider]; the tests override the upstream
// `sdkClientProvider` / `websocketProvider` so no network call is made.
// `getLiveMetrics()` is a public method on SdkApiClient, so a stub client
// can serve a canned /api/v1/metrics/live payload.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/metrics/metrics_panel.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Serves a fixed live-metrics payload and counts fetches (for refresh).
class _MetricsStubClient extends SdkApiClient {
  _MetricsStubClient(this._payload) : super(host: 'localhost', port: 8081);

  final Map<String, dynamic> _payload;
  int fetchCount = 0;

  @override
  Future<Map<String, dynamic>> getLiveMetrics() async {
    fetchCount++;
    // Deep-ish copy so a caller cannot mutate the fixture.
    return {
      for (final entry in _payload.entries)
        entry.key: entry.value is List
            ? List<dynamic>.from(entry.value as List)
            : entry.value,
    };
  }
}

/// Reports a live connection so the notifier subscribes to WS instead of
/// starting the 10s poll timer (keeps the test free of pending timers).
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

Map<String, dynamic> _baseSnapshot() => {
  'timestamp': '2024-01-01T10:00:00Z',
  'active_agents': 3,
  'requests_per_sec': 2.5,
  'queue_depth': 5,
  'total_jobs': 20,
  'running_jobs': 2,
  'pending_jobs': 5,
};

Map<String, dynamic> _fullPayload() => {
  ..._baseSnapshot(),
  'models': [
    {
      'id': 'openai/gpt-x',
      'calls': 12,
      'tokens_in': 3400,
      'tokens_out': 900,
      'avg_latency_ms': 812.5,
    },
    {
      'id': 'local/llama',
      'calls': 4,
      'tokens_in': 100,
      'tokens_out': 50,
      'avg_latency_ms': 20.0,
    },
  ],
  'agents': [
    {'id': 'chat', 'state': 'idle', 'tasks_completed': 4, 'tasks_failed': 0},
    {'id': 'planner', 'state': 'busy', 'tasks_completed': 1, 'tasks_failed': 2},
  ],
  'totals': {'calls': 16, 'tokens_in': 3500, 'tokens_out': 950},
};

Widget _app(_MetricsStubClient client) {
  return ProviderScope(
    overrides: [
      sdkClientProvider.overrideWith((_) => client),
      websocketProvider.overrideWith((_) => _StubWebSocket()),
    ],
    child: const MaterialApp(home: Scaffold(body: MetricsPanel())),
  );
}

/// Pump the widget and let the initial async fetch settle.
Future<void> _settle(WidgetTester tester, _MetricsStubClient client) async {
  await tester.pumpWidget(_app(client));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 20));
}

void main() {
  group('MetricsPanel populates usage', () {
    testWidgets('renders the tiles plus both usage tables', (tester) async {
      final client = _MetricsStubClient(_fullPayload());
      await _settle(tester, client);

      // Existing six tiles.
      expect(find.text('active agents'), findsOneWidget);
      expect(find.text('queue depth'), findsOneWidget);
      expect(find.text('running'), findsOneWidget);
      expect(find.text('pending'), findsOneWidget);
      expect(find.text('total jobs'), findsOneWidget);
      expect(find.text('req/sec'), findsOneWidget);

      // Totals row.
      expect(find.text('totals'), findsOneWidget);
      expect(find.text('total calls'), findsOneWidget);
      expect(find.text('3,500'), findsOneWidget); // tokens in total

      // Model usage table.
      expect(find.text('model usage'), findsOneWidget);
      expect(find.text('openai/gpt-x'), findsOneWidget);
      expect(find.text('local/llama'), findsOneWidget);
      expect(find.text('812.5ms'), findsOneWidget);

      // Agent usage table.
      expect(find.text('agent usage'), findsOneWidget);
      expect(find.text('chat'), findsOneWidget);
      expect(find.text('planner'), findsOneWidget);
      expect(find.text('completed'), findsOneWidget);
      expect(find.text('failed'), findsOneWidget);

      // Live indicator + refresh action + updated stamp.
      expect(find.text('live'), findsOneWidget);
      expect(find.byTooltip('refresh metrics'), findsOneWidget);
      expect(find.textContaining('updated '), findsOneWidget);

      expect(tester.takeException(), isNull);
    });

    testWidgets('refresh control re-fetches metrics', (tester) async {
      final client = _MetricsStubClient(_fullPayload());
      await _settle(tester, client);
      expect(client.fetchCount, 1);

      await tester.tap(find.byTooltip('refresh metrics'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 20));

      expect(client.fetchCount, 2);
      expect(tester.takeException(), isNull);
    });
  });

  group('MetricsPanel degrades gracefully', () {
    testWidgets('missing models/agents/totals still renders the tiles', (
      tester,
    ) async {
      final client = _MetricsStubClient(_baseSnapshot());
      await _settle(tester, client);

      // Existing tiles survive - no whole-panel red error.
      expect(find.text('active agents'), findsOneWidget);
      expect(find.text('queue depth'), findsOneWidget);
      expect(find.text('req/sec'), findsOneWidget);

      // A lowercase note stands in for each missing section.
      expect(
        find.text('model usage not reported by this daemon'),
        findsOneWidget,
      );
      expect(
        find.text('agent usage not reported by this daemon'),
        findsOneWidget,
      );
      expect(
        find.text('usage totals not reported by this daemon'),
        findsOneWidget,
      );

      expect(tester.takeException(), isNull);
    });

    testWidgets('empty usage lists show empty messages, not blanks', (
      tester,
    ) async {
      final client = _MetricsStubClient({
        ..._baseSnapshot(),
        'models': <Map<String, dynamic>>[],
        'agents': <Map<String, dynamic>>[],
        'totals': {'calls': 0, 'tokens_in': 0, 'tokens_out': 0},
      });
      await _settle(tester, client);

      expect(find.text('no model usage in the last 24h'), findsOneWidget);
      expect(find.text('no agents reported'), findsOneWidget);
      // Totals were present, so the totals tiles render.
      expect(find.text('total calls'), findsOneWidget);

      expect(tester.takeException(), isNull);
    });
  });

  group('MetricsPanel layout', () {
    testWidgets('no overflow at a small viewport', (tester) async {
      tester.view.physicalSize = const Size(320, 400);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      final client = _MetricsStubClient({
        ..._baseSnapshot(),
        'models': [
          for (var i = 0; i < 6; i++)
            {
              'id': 'provider/model-$i',
              'calls': 10 + i,
              'tokens_in': 1000 * (i + 1),
              'tokens_out': 200 * (i + 1),
              'avg_latency_ms': 100.0 + i,
            },
        ],
        'agents': [
          for (var i = 0; i < 5; i++)
            {
              'id': 'agent-$i',
              'state': 'idle',
              'tasks_completed': i,
              'tasks_failed': 0,
            },
        ],
        'totals': {'calls': 75, 'tokens_in': 21000, 'tokens_out': 4200},
      });
      await _settle(tester, client);

      // The body must be scrollable and free of RenderFlex overflow.
      expect(find.byType(SingleChildScrollView), findsWidgets);
      expect(tester.takeException(), isNull);

      // Long tables live below the fold; scrolling still reaches them.
      await tester.drag(
        find.byType(SingleChildScrollView).first,
        const Offset(0, -400),
      );
      await tester.pump();
      expect(tester.takeException(), isNull);
      expect(find.text('agent-4'), findsOneWidget);
    });
  });

  group('usage JSON parsing', () {
    test('ModelUsage.parseList distinguishes absent from empty', () {
      expect(ModelUsage.parseList(null), isNull);
      expect(ModelUsage.parseList(<dynamic>[]), isEmpty);
      expect(ModelUsage.parseList('nope'), isNull);
    });

    test('ModelUsage.parseList skips malformed rows', () {
      final parsed = ModelUsage.parseList([
        {
          'id': 'a/b',
          'calls': 3,
          'tokens_in': 1,
          'tokens_out': 2,
          'avg_latency_ms': 5,
        },
        {'calls': 1}, // no id -> skipped
        'garbage', // not a map -> skipped
        {'id': 'c/d', 'calls': '7', 'avg_latency_ms': '9.5'},
      ]);
      expect(parsed, hasLength(2));
      expect(parsed!.first.id, 'a/b');
      expect(parsed.first.avgLatencyMs, 5);
      expect(parsed.last.calls, 7);
      expect(parsed.last.avgLatencyMs, 9.5);
    });

    test('AgentUsage.parseList distinguishes absent from empty', () {
      expect(AgentUsage.parseList(null), isNull);
      expect(AgentUsage.parseList(<dynamic>[]), isEmpty);
      final parsed = AgentUsage.parseList([
        {
          'id': 'chat',
          'state': 'idle',
          'tasks_completed': 4,
          'tasks_failed': 0,
        },
      ]);
      expect(parsed, hasLength(1));
      expect(parsed!.single.tasksCompleted, 4);
    });

    test('MetricsTotals.parse returns null when absent, zeros when empty', () {
      expect(MetricsTotals.parse(null), isNull);
      expect(MetricsTotals.parse(42), isNull);
      final zeros = MetricsTotals.parse(<String, dynamic>{});
      expect(zeros, isNotNull);
      expect(zeros!.calls, 0);
      final value = MetricsTotals.parse({
        'calls': 16,
        'tokens_in': 3500,
        'tokens_out': 950,
      });
      expect(value!.tokensIn, 3500);
    });
  });
}
