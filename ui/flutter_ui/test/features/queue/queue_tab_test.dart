import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/queue/queue_detail.dart';
import 'package:meept_ui/features/queue/queue_list.dart';
import 'package:meept_ui/features/queue/queue_tab.dart';
import 'package:meept_ui/services/sdk_client.dart';

// ===== SdkApiClient stubs =====

Map<String, dynamic> _jobJson({
  String id = 'job-1',
  String state = 'pending',
  String error = '',
  int retryCount = 0,
}) {
  return {
    'id': id,
    'task_id': 'task-1',
    'type': 'one_off',
    'priority': 3,
    'state': state,
    'payload': {'prompt': 'hello world'},
    'max_retries': 3,
    'retry_count': retryCount,
    'error': error,
    'created_at': '2026-10-15T10:00:00Z',
  };
}

/// Client returning a fixed jobs list and stats payload.
///
/// Stats use the Go field-name keys (`ByState`/`DeadCount`) exactly as
/// `*queue.QueueStats` marshals (no json tags on the struct).
class _QueueSdkClient extends SdkApiClient {
  final List<Map<String, dynamic>> jobs;

  _QueueSdkClient(this.jobs) : super(host: 'localhost', port: 65450);

  @override
  Future<List<Map<String, dynamic>>> listJobs({String? agentId}) async => jobs;

  @override
  Future<Map<String, dynamic>> getQueueStats() async {
    return {
      'ByState': {'pending': 2, 'processing': 1, 'completed': 5},
      'DeadCount': 0,
    };
  }
}

/// Client that throws on every call.
class _FailingQueueSdkClient extends SdkApiClient {
  _FailingQueueSdkClient() : super(host: 'localhost', port: 65451);

  @override
  Future<List<Map<String, dynamic>>> listJobs({String? agentId}) async {
    throw Exception('connection refused');
  }

  @override
  Future<Map<String, dynamic>> getQueueStats() async {
    throw Exception('connection refused');
  }
}

Widget _wrap(List<Override> overrides) {
  return ProviderScope(
    overrides: overrides,
    child: const MaterialApp(home: Scaffold(body: QueueTab())),
  );
}

void main() {
  group('QueueTab widget', () {
    testWidgets('renders stats header counts from queue stats', (tester) async {
      await tester.pumpWidget(
        _wrap([
          queueViewProvider.overrideWith(
            (ref) => QueueViewController(_QueueSdkClient([])),
          ),
        ]),
      );
      await tester.pumpAndSettle();

      expect(find.text('depth: 3'), findsOneWidget);
      expect(find.text('pending: 2'), findsOneWidget);
      expect(find.text('processing: 1'), findsOneWidget);
      expect(find.text('completed: 5'), findsOneWidget);
      // Dead count of 0 is hidden, matching the TUI stats panel.
      expect(find.textContaining('dead:'), findsNothing);
    });

    testWidgets('renders jobs list with id, state and retries', (tester) async {
      await tester.pumpWidget(
        _wrap([
          queueViewProvider.overrideWith(
            (ref) => QueueViewController(
              _QueueSdkClient([
                _jobJson(id: 'aaaaaaaaaaaaaaaaaaaaaaaaa', state: 'pending'),
                _jobJson(
                  id: 'job-2',
                  state: 'failed',
                  error: 'boom',
                  retryCount: 2,
                ),
              ]),
            ),
          ),
        ]),
      );
      await tester.pumpAndSettle();

      // Long ids are truncated like the TUI (17 chars + '...').
      expect(find.text('aaaaaaaaaaaaaaaaa...'), findsOneWidget);
      expect(find.text('job-2'), findsOneWidget);
      expect(find.text('pending'), findsWidgets);
      expect(find.text('failed'), findsWidgets);
      expect(find.text('retries: 2/3'), findsOneWidget);
    });

    testWidgets('tapping a job opens the detail pane', (tester) async {
      await tester.pumpWidget(
        _wrap([
          queueViewProvider.overrideWith(
            (ref) =>
                QueueViewController(_QueueSdkClient([_jobJson(id: 'job-9')])),
          ),
        ]),
      );
      await tester.pumpAndSettle();

      expect(find.byType(QueueDetail), findsOneWidget);
      expect(find.text('select a job'), findsOneWidget);

      await tester.tap(find.text('job-9'));
      await tester.pumpAndSettle();

      expect(find.text('job-9'), findsNWidgets(2)); // list row + detail title
      expect(find.text('payload'), findsOneWidget);
      expect(find.textContaining('hello world'), findsOneWidget);
    });

    testWidgets('shows empty state when no jobs', (tester) async {
      await tester.pumpWidget(
        _wrap([
          queueViewProvider.overrideWith(
            (ref) => QueueViewController(_QueueSdkClient([])),
          ),
        ]),
      );
      await tester.pumpAndSettle();

      expect(find.byType(QueueList), findsOneWidget);
      expect(find.text('no jobs'), findsOneWidget);
    });

    testWidgets('shows error state when fetch fails', (tester) async {
      await tester.pumpWidget(
        _wrap([
          queueViewProvider.overrideWith(
            (ref) => QueueViewController(_FailingQueueSdkClient()),
          ),
        ]),
      );
      await tester.pumpAndSettle();

      expect(find.textContaining('error:'), findsOneWidget);
    });

    testWidgets('filter chips narrow the jobs list', (tester) async {
      await tester.pumpWidget(
        _wrap([
          queueViewProvider.overrideWith(
            (ref) => QueueViewController(
              _QueueSdkClient([
                _jobJson(id: 'job-p', state: 'pending'),
                _jobJson(id: 'job-f', state: 'failed'),
              ]),
            ),
          ),
        ]),
      );
      await tester.pumpAndSettle();

      expect(find.text('job-p'), findsOneWidget);
      expect(find.text('job-f'), findsOneWidget);

      await tester.tap(find.widgetWithText(ChoiceChip, 'failed'));
      await tester.pumpAndSettle();

      expect(find.text('job-f'), findsOneWidget);
      expect(find.text('job-p'), findsNothing);
    });
  });

  group('QueueJobView parsing', () {
    test('priority names map like the TUI', () {
      expect(
        QueueJobView.fromJson(_jobJson()..['priority'] = 1).priorityName,
        'low',
      );
      expect(
        QueueJobView.fromJson(_jobJson()..['priority'] = 4).priorityName,
        'urgent',
      );
      expect(
        QueueJobView.fromJson(_jobJson()..['priority'] = 9).priorityName,
        '9',
      );
    });

    test('long ids truncate to the TUI column width', () {
      final job = QueueJobView.fromJson(
        _jobJson(id: '0123456789012345678901234'),
      );
      expect(job.shortId, '01234567890123456...');
      expect(job.shortId.length, 20);
    });
  });
}
