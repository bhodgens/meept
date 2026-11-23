// Widget tests for the review-verdict grades section (leaf 03-flutter-panel,
// contract C6 of model-rating-metrics-plan).
//
// The grades tables render from GET /api/v1/metrics/reviews payloads served
// by a stub SdkApiClient override of `sdkClientProvider` (same pattern as
// metrics_panel_test.dart) — no network is touched. `getReviewMetrics()` is
// a public method on SdkApiClient, so the stub serves canned payloads per
// group_by and records requests for the wire-shape assertions.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/metrics/metrics_panel.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Serves fixed live-metrics + review-grades payloads and records every
/// reviews request (method + query) for contract assertions.
class _ReviewsStubClient extends SdkApiClient {
  _ReviewsStubClient(this._reviewsByGroupBy)
    : super(host: 'localhost', port: 8081);

  final Map<String, Map<String, dynamic>> _reviewsByGroupBy;
  final List<Map<String, dynamic>> reviewRequests = [];
  int liveMetricsCount = 0;

  @override
  Future<Map<String, dynamic>> getLiveMetrics() async {
    liveMetricsCount++;
    return {
      'timestamp': '2024-01-01T10:00:00Z',
      'active_agents': 1,
      'requests_per_sec': 1.0,
      'queue_depth': 0,
      'total_jobs': 1,
      'running_jobs': 0,
      'pending_jobs': 0,
    };
  }

  @override
  Future<Map<String, dynamic>> getReviewMetrics({
    String window = '30d',
    String groupBy = 'model',
  }) async {
    reviewRequests.add({'window': window, 'group_by': groupBy});
    return {
      for (final entry
          in (_reviewsByGroupBy[groupBy] ?? <String, dynamic>{}).entries)
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

/// Wire shape copied from contract C6 / C5: snake_case keys exactly as the
/// Go endpoint emits them (internal/comm/http/reviews_endpoint.go).
Map<String, dynamic> _gradesPayload({
  required String groupBy,
  required List<Map<String, dynamic>> rows,
}) => {
  'window': '30d',
  'group_by': groupBy,
  'rows': rows,
  'workflow_excluded': 2,
};

Widget _app(_ReviewsStubClient client) {
  return ProviderScope(
    overrides: [
      sdkClientProvider.overrideWith((_) => client),
      websocketProvider.overrideWith((_) => _StubWebSocket()),
    ],
    child: const MaterialApp(home: Scaffold(body: MetricsPanel())),
  );
}

/// Pump the widget and let the initial async fetches settle.
Future<void> _settle(WidgetTester tester, _ReviewsStubClient client) async {
  await tester.pumpWidget(_app(client));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 20));
  await tester.pump(const Duration(milliseconds: 20));
}

void main() {
  group('MetricsPanel grades section (C6)', () {
    testWidgets('renders grades-by-model and grades-by-judge tables from '
        'wire-shape payloads', (tester) async {
      final client = _ReviewsStubClient({
        'model': _gradesPayload(
          groupBy: 'model',
          rows: [
            {
              'group': 'provider/glm-x',
              'verdicts': 4,
              'defects': 2,
              'pass_rate': 0.5,
              'label': 'GLM X',
            },
            {
              'group': 'local/llama',
              'verdicts': 1,
              'defects': 0,
              'pass_rate': 1.0,
              'label': '',
            },
          ],
        ),
        'judge': _gradesPayload(
          groupBy: 'judge',
          rows: [
            {
              'group': 'code-reviewer',
              'verdicts': 5,
              'defects': 2,
              'pass_rate': 0.6,
              'label': 'code reviewer',
            },
          ],
        ),
      });
      await _settle(tester, client);

      // Two lowercase-titled sections.
      expect(find.text('grades by model'), findsOneWidget);
      expect(find.text('grades by judge'), findsOneWidget);

      // Column headers (lowercase).
      expect(find.text('group'), findsWidgets);
      expect(find.text('verdicts'), findsWidgets);
      expect(find.text('defects'), findsWidgets);
      expect(find.text('pass rate'), findsWidgets);

      // By-model rows use the daemon label when present, else the group.
      expect(find.text('GLM X'), findsOneWidget);
      expect(find.text('local/llama'), findsOneWidget);
      // By-judge row (judge labels, never reviewer).
      expect(find.text('code reviewer'), findsOneWidget);

      // Formatted values: pass rates as percentages, '-' for zero rates.
      expect(find.text('50%'), findsOneWidget);
      expect(find.text('100%'), findsOneWidget);
      expect(find.text('60%'), findsOneWidget);
      expect(find.text('2'), findsWidgets); // defects counts

      // Both fetches hit the endpoint with the frozen query keys.
      expect(
        client.reviewRequests.any(
          (r) => r['group_by'] == 'model' && r['window'] == '30d',
        ),
        isTrue,
      );
      expect(
        client.reviewRequests.any(
          (r) => r['group_by'] == 'judge' && r['window'] == '30d',
        ),
        isTrue,
      );

      // The usage sections are untouched by the new grades sections.
      expect(find.text('model usage'), findsOneWidget);
      expect(find.text('agent usage'), findsOneWidget);

      expect(tester.takeException(), isNull);
    });

    testWidgets('empty rows render the lowercase empty note, not blanks', (
      tester,
    ) async {
      final client = _ReviewsStubClient({
        'model': _gradesPayload(groupBy: 'model', rows: []),
        'judge': _gradesPayload(groupBy: 'judge', rows: []),
      });
      await _settle(tester, client);

      expect(find.text('grades by model'), findsOneWidget);
      expect(find.text('grades by judge'), findsOneWidget);
      expect(find.text('no reviews recorded in this window'), findsNWidgets(2));
      expect(tester.takeException(), isNull);
    });

    testWidgets('a failed grades fetch degrades to the empty note and '
        'keeps the live tiles', (tester) async {
      final client = _FailingReviewsClient();
      await _settle(tester, client);

      expect(find.text('active agents'), findsOneWidget);
      expect(find.text('grades by model'), findsOneWidget);
      expect(find.text('grades by judge'), findsOneWidget);
      expect(find.text('no reviews recorded in this window'), findsNWidgets(2));
      expect(tester.takeException(), isNull);
    });

    testWidgets('refresh control re-fetches the grades too', (tester) async {
      final client = _ReviewsStubClient({
        'model': _gradesPayload(groupBy: 'model', rows: []),
        'judge': _gradesPayload(groupBy: 'judge', rows: []),
      });
      await _settle(tester, client);
      final modelFetches = client.reviewRequests
          .where((r) => r['group_by'] == 'model')
          .length;
      expect(modelFetches, 1);

      await tester.tap(find.byTooltip('refresh metrics'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 20));
      await tester.pump(const Duration(milliseconds: 20));

      final modelFetchesAfter = client.reviewRequests
          .where((r) => r['group_by'] == 'model')
          .length;
      expect(modelFetchesAfter, greaterThan(modelFetches));
      expect(tester.takeException(), isNull);
    });
  });

  group('review grades JSON parsing (C6 key parity)', () {
    test('ReviewGradeRow.fromJson maps snake_case keys 1:1', () {
      final row = ReviewGradeRow.fromJson({
        'group': 'provider/glm-x',
        'verdicts': 4,
        'defects': 2,
        'pass_rate': 0.5,
        'label': 'GLM X',
      });
      expect(row.group, 'provider/glm-x');
      expect(row.verdicts, 4);
      expect(row.defects, 2);
      expect(row.passRate, 0.5);
      expect(row.label, 'GLM X');
      expect(row.displayName, 'GLM X');
    });

    test('ReviewGrades.fromJson decodes the full envelope', () {
      final grades = ReviewGrades.fromJson({
        'window': '24h',
        'group_by': 'judge',
        'rows': [
          {
            'group': 'code-reviewer',
            'verdicts': 5,
            'defects': 2,
            'pass_rate': 0.6,
          },
        ],
        'workflow_excluded': 7,
      });
      expect(grades.window, '24h');
      expect(grades.groupBy, 'judge');
      expect(grades.rows, hasLength(1));
      expect(grades.workflowExcluded, 7);
    });

    test('ReviewGradeRow.parseList distinguishes absent from empty', () {
      expect(ReviewGradeRow.parseList(null), isNull);
      expect(ReviewGradeRow.parseList(<dynamic>[]), isEmpty);
      expect(ReviewGradeRow.parseList('nope'), isNull);
    });

    test('ReviewGradeRow.parseList skips malformed rows', () {
      final parsed = ReviewGradeRow.parseList([
        {'group': 'a/b', 'verdicts': 3, 'defects': 1, 'pass_rate': '0.5'},
        {'verdicts': 1}, // no group -> skipped
        'garbage', // not a map -> skipped
      ]);
      expect(parsed, hasLength(1));
      expect(parsed!.single.group, 'a/b');
      expect(parsed.single.verdicts, 3);
      expect(parsed.single.defects, 1);
      expect(parsed.single.passRate, 0.5);
    });

    test(
      'ReviewGrades.fromJson tolerates absent rows (nil provider shape)',
      () {
        final grades = ReviewGrades.fromJson({
          'window': 'all',
          'group_by': 'rubric',
          'workflow_excluded': 0,
        });
        expect(grades.rows, isEmpty);
      },
    );
  });
}

/// Stub client whose grades fetch throws (nil-provider path is a 200 with
/// empty rows on the real daemon, but a network failure must degrade the
/// same way on the panel).
class _FailingReviewsClient extends _ReviewsStubClient {
  _FailingReviewsClient() : super(const {});

  @override
  Future<Map<String, dynamic>> getReviewMetrics({
    String window = '30d',
    String groupBy = 'model',
  }) async {
    throw Exception('grades endpoint unreachable');
  }
}
