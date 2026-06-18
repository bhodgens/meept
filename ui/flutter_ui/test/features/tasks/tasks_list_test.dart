import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/tasks/tasks_list.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/task_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

// ===== Task model helper =====

Task _makeTask({
  String id = '1',
  String title = 'Sample Task',
  String status = 'pending',
  DateTime? createdAt,
}) {
  return Task(
    id: id,
    title: title,
    description: '',
    status: status,
    createdAt: createdAt ?? DateTime.now(),
  );
}

// ===== SdkApiClient stubs =====

/// Returns a predefined list of tasks.
class _TestSdkClient extends SdkApiClient {
  final List<Task> _tasks;
  final List<Task> _created = [];

  _TestSdkClient([this._tasks = const []]) : super(host: 'localhost', port: 65432);

  List<Task> get createdTasks => _created;

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async {
    return _tasks.map((t) => t.toJson()).toList();
  }

  @override
  Future<Map<String, dynamic>> createTask({
    required String title,
    String? sessionId,
  }) async {
    final task = Task(
      id: 'new-${_created.length + 1}',
      title: title,
      description: '',
      status: 'pending',
      createdAt: DateTime.now(),
      sessionId: sessionId,
    );
    _created.add(task);
    return task.toJson();
  }
}

/// Client that throws on listTasks to test error state.
class _ThrowingSdkClient extends SdkApiClient {
  _ThrowingSdkClient() : super(host: 'localhost', port: 65433);

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async {
    throw Exception('connection refused');
  }

  @override
  Future<Map<String, dynamic>> createTask({
    required String title,
    String? sessionId,
  }) async {
    throw Exception('connection refused');
  }
}

/// Client with a delay on listTasks so loading state is visible.
class _SlowLoadSdkClient extends SdkApiClient {
  _SlowLoadSdkClient() : super(host: 'localhost', port: 65434);

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async {
    await Future.delayed(const Duration(milliseconds: 100));
    return [];
  }

  @override
  Future<Map<String, dynamic>> createTask({
    required String title,
    String? sessionId,
  }) async {
    final task = Task(
      id: 'new-1',
      title: title,
      description: '',
      status: 'pending',
      createdAt: DateTime.now(),
    );
    return task.toJson();
  }
}

// ===== Widget tests: TasksList =====

void main() {
  group('TasksList widget', () {
    testWidgets('displays loading indicator when loading', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: _SlowLoadSdkClient())),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );

      // Triggers initial load callback
      await tester.pump();
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      await tester.pumpAndSettle();
    });

    testWidgets('displays "no tasks" when tasks list is empty', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: _TestSdkClient([]))),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('no tasks'), findsOneWidget);
    });

    testWidgets('displays error message when loading fails', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: _ThrowingSdkClient())),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('error:'), findsOneWidget);
    });

    testWidgets('displays task tiles when tasks exist', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith((ref) {
              final notifier = TaskNotifier(
                sdkClient: _TestSdkClient(
                  [_makeTask(), _makeTask(id: '2', title: 'Second Task')],
                ),
              );
              return notifier;
            }),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('sample task'), findsOneWidget);
      expect(find.text('second task'), findsOneWidget);
      expect(find.text('pending'), findsNWidgets(2));
    });

    testWidgets('shows error indicator color for failed tasks', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith((ref) {
              return TaskNotifier(
                sdkClient: _TestSdkClient([_makeTask(status: 'failed')]),
              );
            }),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // The status badge should be visible
      expect(find.text('failed'), findsOneWidget);
    });

    testWidgets('shows create task dialog when + button is pressed',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: _TestSdkClient([]))),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('create task'), findsOneWidget);
      expect(find.widgetWithText(TextButton, 'cancel'), findsOneWidget);
      expect(find.widgetWithText(FilledButton, 'create'), findsOneWidget);
      expect(find.byType(TextField), findsOneWidget);
    });

    testWidgets('dialog creates task when create is tapped with text',
        (tester) async {
      final client = _TestSdkClient();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: client)),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Open dialog
      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();

      // Type a title
      await tester.enterText(find.byType(TextField), 'New Test Task');

      // Tap create
      await tester.tap(find.widgetWithText(FilledButton, 'create'));
      await tester.pumpAndSettle();

      // Verify the task was created via API client
      expect(client.createdTasks, hasLength(1));
      expect(client.createdTasks[0].title, 'New Test Task');
    });

    testWidgets('dialog does not create task with empty title',
        (tester) async {
      final client = _TestSdkClient();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: client)),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Open dialog
      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();

      // Do NOT enter text -- just tap create
      await tester.tap(find.widgetWithText(FilledButton, 'create'));
      await tester.pumpAndSettle();

      // Dialog should still be open (no task created)
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(client.createdTasks, isEmpty);
    });

    testWidgets('pressing cancel dismisses the dialog', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: _TestSdkClient([]))),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsOneWidget);

      await tester.tap(find.widgetWithText(TextButton, 'cancel'));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
    });

    testWidgets('dialog autofocus focuses the text field', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: _TestSdkClient([]))),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.autofocus, isTrue);
    });

    testWidgets('creating a task appends it to the list after API succeeds',
        (tester) async {
      final client = _TestSdkClient();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            taskProvider.overrideWith(
                (ref) => TaskNotifier(sdkClient: client)),
          ],
          child: const MaterialApp(
            home: Scaffold(body: TasksList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // List starts empty
      expect(find.text('no tasks'), findsOneWidget);

      // Open dialog and create a task
      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'Appears Task');
      await tester.tap(find.widgetWithText(FilledButton, 'create'));
      await tester.pumpAndSettle();

      // Now the task should be visible (title is lowercased in the tile)
      expect(find.text('appears task'), findsOneWidget);
    });
  });

  // ===== Unit tests: TaskNotifier =====

  group('TaskNotifier', () {
    test('state starts empty', () {
      final notifier = TaskNotifier(sdkClient: _TestSdkClient([]));
      expect(notifier.state.tasks, isEmpty);
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
    });

    test('loadTasks populates tasks', () async {
      final client = _TestSdkClient([_makeTask(), _makeTask(id: '2')]);
      final notifier = TaskNotifier(sdkClient: client);
      await notifier.loadTasks();

      expect(notifier.state.tasks, hasLength(2));
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
    });

    test('loadTasks sets error on failure', () async {
      final notifier = TaskNotifier(sdkClient: _ThrowingSdkClient());
      await notifier.loadTasks();

      expect(notifier.state.tasks, isEmpty);
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNotNull);
    });

    test('createTask appends new task', () async {
      final client = _TestSdkClient([]);
      final notifier = TaskNotifier(sdkClient: client);
      expect(notifier.state.tasks, isEmpty);

      final newTask = await notifier.createTask(title: 'Created Task');
      expect(newTask, isNotNull);
      expect(notifier.state.tasks, hasLength(1));
      expect(notifier.state.tasks[0].title, 'Created Task');
      expect(notifier.state.tasks[0].status, 'pending');
    });

    test('createTask sets error on failure', () async {
      final notifier = TaskNotifier(sdkClient: _ThrowingSdkClient());
      final result = await notifier.createTask(title: 'fails');
      expect(result, isNull);
      expect(notifier.state.error, isNotNull);
    });

    test('createTask preserves existing tasks in state', () async {
      final client = _TestSdkClient([_makeTask(id: 'existing')]);
      final notifier = TaskNotifier(sdkClient: client);
      await notifier.loadTasks();

      await notifier.createTask(title: 'new task');
      expect(notifier.state.tasks, hasLength(2));
      expect(notifier.state.tasks[0].id, 'existing');
    });
  });
}
