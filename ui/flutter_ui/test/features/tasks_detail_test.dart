// 05-navigation TEST-GATE T4 (leaf 00T-gate.md, matrix F12b; W19/N4).
//
// Pins GUI task-detail reachability: tapping a task row in TasksTab must
// select it (activeTaskProvider written) and the detail pane must show
// title/state AND the task's steps hydrated through the SDK getTask +
// steps route (sdk_client.dart:878 getTask has zero callers today;
// tasks_tab.dart:22 builds TasksList WITHOUT onTaskSelected, so
// activeTaskProvider is never written and the detail is unreachable).
//
// RED today: tap selects nothing ('select a task' placeholder stays);
// no steps are fetched or rendered. Widget tests are the agent floor;
// pixel-level claims remain operator-pending (master Q3).

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/tasks/tasks_detail.dart';
import 'package:meept_ui/features/tasks/tasks_tab.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/task_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// SDK stub serving one task with two steps. Records getTask calls so
/// the hydration seam (zero callers today) is observable.
class _TaskStubClient extends SdkApiClient {
  _TaskStubClient() : super(host: 'localhost', port: 65432);

  final getTaskCalls = <String>[];

  Map<String, dynamic> get _taskJson => {
    'id': 'task-42',
    'name': 'quartz detail task',
    'description': 'the quartz work behind the reply',
    'state': 'executing',
    'created_at': DateTime.utc(2026, 11, 19).toIso8601String(),
    'session_id': 'test-session',
    'steps': [
      {
        'id': 'step-1',
        'task_id': 'task-42',
        'description': 'quartzstepalpha',
        'state': 'completed',
        'result': 'wrote quartz.txt',
      },
      {
        'id': 'step-2',
        'task_id': 'task-42',
        'description': 'quartzstepbeta',
        'state': 'executing',
      },
    ],
  };

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async {
    return [_taskJson];
  }

  @override
  Future<Map<String, dynamic>> getTask(String id) async {
    getTaskCalls.add(id);
    return _taskJson;
  }
}

Task _task() => Task(
  id: 'task-42',
  title: 'quartz detail task',
  description: 'the quartz work behind the reply',
  status: 'executing',
  createdAt: DateTime.utc(2026, 11, 19),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GUI task detail reachability (05-navigation T4)', () {
    testWidgets('TestTaskDetailReachable: list tap opens detail with steps', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(1400, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      final client = _TaskStubClient();
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWith((_) => client)],
      );
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(home: Scaffold(body: TasksTab())),
        ),
      );
      await tester.pumpAndSettle();

      // Precondition: the list rendered the seeded task row.
      expect(find.text('quartz detail task'), findsWidgets);

      // Tap the task row: the detail must become reachable.
      await tester.tap(find.text('quartz detail task').first);
      await tester.pumpAndSettle();

      expect(
        container.read(activeTaskProvider)?.id,
        'task-42',
        reason:
            'task tap did not write activeTaskProvider '
            '(TasksList built without onTaskSelected; detail unreachable)',
      );
      expect(
        find.byType(TasksDetail),
        findsOneWidget,
        reason: 'detail pane not shown after selection',
      );
      expect(
        find.text('select a task'),
        findsNothing,
        reason: 'placeholder still shown after a task was selected',
      );

      // Steps hydrated through the SDK seam and rendered.
      expect(
        client.getTaskCalls,
        contains('task-42'),
        reason:
            'detail never fetched the task via getTask '
            '(sdk_client.dart getTask has zero callers; steps stale/absent)',
      );
      expect(
        find.textContaining('quartzstepalpha'),
        findsWidgets,
        reason: 'detail pane does not render the task steps',
      );
      expect(
        find.textContaining('quartzstepbeta'),
        findsWidgets,
        reason: 'detail pane does not render the second step',
      );

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });

    testWidgets('chip-origin selection: activeTaskProvider drives detail', (
      tester,
    ) async {
      // The reply task chip navigates by writing activeTaskProvider (the
      // same seam the list tap uses). Given a selected task, the detail
      // pane must hydrate steps even when the list never loaded it.
      tester.view.physicalSize = const Size(1400, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      final client = _TaskStubClient();
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWith((_) => client),
          activeTaskProvider.overrideWith((_) => _task()),
        ],
      );
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(home: Scaffold(body: TasksDetail())),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('quartz detail task'), findsWidgets);
      expect(
        client.getTaskCalls,
        contains('task-42'),
        reason: 'chip-selected detail did not hydrate via getTask',
      );
      expect(
        find.textContaining('quartzstepalpha'),
        findsWidgets,
        reason: 'chip-selected detail renders no steps',
      );

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });
  });
}
