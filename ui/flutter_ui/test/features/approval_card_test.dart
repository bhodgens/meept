// 06-planning TEST-GATE T2 (F13b): GUI approval card for the strategic
// task gate, wired to the C2 HTTP twins - NOT the plans.db gate.
//
// RED today for missing-implementation reasons:
//   - SdkApiClient has approvePlan/rejectPlan (plans.db REST) but NO
//     approveTask/rejectTask for /api/v1/tasks/{id}/approve and
//     /api/v1/tasks/{id}/reject (C2 HTTP twins, 01-safety/01 T3). The
//     wire pins invoke them through `dynamic` so this file COMPILES today
//     and fails at runtime with NoSuchMethodError until the methods land
//     (the Dart analog of the Go reflection gate - a compile-RED file
//     would mask the widget pins below).
//   - PlansTab renders approval buttons ONLY for plans.db rows in
//     pending_approval; an awaiting_approval TASK (the strategic gate,
//     tasks.db) never reaches the card - the two gates are conflated.
//
// C2 pins: approve -> POST /api/v1/tasks/{id}/approve; reject ->
// POST /api/v1/tasks/{id}/reject with the reason forwarded; reject
// NEVER calls the approve endpoint (load-bearing).
//
// Wire tests use the _FakeAdapter pattern from
// test/services/sdk_client_submit_test.dart (no network). Widget tests
// use the provider-override harness pattern from
// test/features/home/session_info_overlay_test.dart.
//
// Gate tests: do not weaken. GREEN after 06-planning/02 T1.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/plans/plans_tab.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/plan_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/task_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

import '../mocks/mock_websocket_service.dart';

/// Fake HttpClientAdapter recording method+path+body and replying with a
/// canned JSON body per path suffix (same shape as the sdk_client_submit
/// gate's adapter). Lets the C2 wire pins assert the exact endpoint.
class _GateAdapter implements HttpClientAdapter {
  _GateAdapter();

  final captured = <_GateRequest>[];
  final responses = <String, Map<String, dynamic>>{};

  void on(String pathSuffix, Map<String, dynamic> body) {
    responses[pathSuffix] = body;
  }

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final path = options.uri.path;
    Map<String, dynamic>? body;
    final data = options.data;
    if (data is Map) {
      body = Map<String, dynamic>.from(data);
    }
    captured.add(_GateRequest(options.method, path, body));
    Map<String, dynamic> reply = const {};
    for (final entry in responses.entries) {
      if (path.endsWith(entry.key)) {
        reply = entry.value;
        break;
      }
    }
    return ResponseBody.fromString(
      jsonEncode(reply),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _GateRequest {
  _GateRequest(this.method, this.path, this.body);
  final String method;
  final String path;
  final Map<String, dynamic>? body;
}

SdkApiClient _gateClient(_GateAdapter adapter) {
  final client = SdkApiClient(host: 'localhost', port: 8081);
  client.dio.httpClientAdapter = adapter;
  return client;
}

/// SDK stub serving one awaiting_approval task plus an empty plans.db
/// list - the strategic gate lives in tasks.db, so the plans list is
/// empty and the card must come from the TASK side. approveTask /
/// rejectTask are declared here (they shadow the base once 02 lands) so
/// the widget pins can record the C2 calls without a network round-trip.
class _GateTaskSdkClient extends SdkApiClient {
  _GateTaskSdkClient({required this.task})
    : super(host: 'localhost', port: 65432);

  final Task task;
  final approveCalls = <String>[];
  final rejectCalls = <String, String>{};

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async => [
    task.toJson(),
  ];

  @override
  Future<List<Map<String, dynamic>>> listPlansBySession(
    String sessionId,
  ) async => const [];

  @override
  Future<List<Map<String, dynamic>>> listPlans({
    String? projectId,
    int limit = 50,
  }) async => const [];

  @override
  Future<Map<String, dynamic>> approveTask(String id) async {
    approveCalls.add(id);
    return {'task_id': id, 'status': 'approved'};
  }

  @override
  Future<Map<String, dynamic>> rejectTask(String id, {String? reason}) async {
    rejectCalls[id] = reason ?? '';
    return {'task_id': id, 'status': 'rejected'};
  }
}

Task _awaitingTask({String id = 'task-gate-gui-01'}) {
  return Task(
    id: id,
    title: 'GUI gate task',
    description: 'strategic approval gate fixture',
    status: 'awaiting_approval',
    createdAt: DateTime.utc(2026, 11, 19),
    totalJobs: 6,
  );
}

Future<void> _pumpPlansTab(
  WidgetTester tester, {
  required SdkApiClient client,
}) async {
  tester.view.physicalSize = const Size(1400, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        sdkClientProvider.overrideWith((ref) => client),
        websocketProvider.overrideWith((ref) => MockWebSocketService()),
        activeSessionProvider.overrideWith((ref) => null),
        planProvider.overrideWith((ref) => PlanNotifier(sdkClient: client)),
        taskProvider.overrideWith((ref) => TaskNotifier(sdkClient: client)),
      ],
      child: const MaterialApp(home: Scaffold(body: PlansTab())),
    ),
  );
  // initState post-frame loads plans/tasks.
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
  await tester.pump(const Duration(milliseconds: 50));
}

void main() {
  group('C2 HTTP twins (SDK wire pins)', () {
    test('approveTask POSTs /api/v1/tasks/{id}/approve', () async {
      final adapter = _GateAdapter()
        ..on('/approve', {'task_id': 'task-gate-gui-01', 'status': 'approved'});
      final client = _gateClient(adapter);

      // dynamic: the method does not exist yet (NoSuchMethodError = RED).
      final result =
          await (client as dynamic).approveTask('task-gate-gui-01')
              as Map<String, dynamic>;

      expect(result['status'], 'approved');
      final posts = adapter.captured.where((r) => r.method == 'POST').toList();
      expect(posts, isNotEmpty, reason: 'approveTask must issue a POST');
      expect(
        posts.first.path,
        '/api/v1/tasks/task-gate-gui-01/approve',
        reason: 'C2 HTTP twin path (not the plans.db /plans/{id}/approve)',
      );
    });

    test('rejectTask POSTs /api/v1/tasks/{id}/reject with reason', () async {
      final adapter = _GateAdapter()
        ..on('/reject', {'task_id': 'task-gate-gui-01', 'status': 'rejected'});
      final client = _gateClient(adapter);

      final result =
          await (client as dynamic).rejectTask(
                'task-gate-gui-01',
                reason: 'too risky to run unattended',
              )
              as Map<String, dynamic>;

      expect(result['status'], 'rejected');
      final posts = adapter.captured.where((r) => r.method == 'POST').toList();
      expect(posts, isNotEmpty, reason: 'rejectTask must issue a POST');
      expect(posts.first.path, '/api/v1/tasks/task-gate-gui-01/reject');
      expect(
        posts.first.body?['reason'],
        'too risky to run unattended',
        reason: 'C2: the full rejection reason must be forwarded',
      );
      // LOAD-BEARING: reject must never hit the approve endpoint.
      expect(
        adapter.captured.where((r) => r.path.endsWith('/approve')),
        isEmpty,
        reason: 'C2 LOAD-BEARING: reject must NEVER call approve',
      );
    });
  });

  group('GUI approval card (widget pins)', () {
    testWidgets('awaiting_approval task renders a card with approve/reject', (
      tester,
    ) async {
      final client = _GateTaskSdkClient(task: _awaitingTask());
      await _pumpPlansTab(tester, client: client);

      // The strategic-gate task must surface as an approval card even
      // though the plans.db list is empty (the two gates are distinct).
      expect(
        find.text('approve'),
        findsWidgets,
        reason:
            'F13b: awaiting_approval task must render an approve affordance '
            '(today only plans.db pending_approval rows get buttons)',
      );
      expect(
        find.text('reject'),
        findsWidgets,
        reason: 'F13b: awaiting_approval task must render a reject affordance',
      );
    });

    testWidgets('approve from the task card calls the C2 task approve', (
      tester,
    ) async {
      final client = _GateTaskSdkClient(task: _awaitingTask());
      await _pumpPlansTab(tester, client: client);

      final approve = find.text('approve');
      expect(approve, findsWidgets, reason: 'card must exist before tapping');
      await tester.tap(approve.first);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      expect(
        client.approveCalls,
        ['task-gate-gui-01'],
        reason:
            'C2: the card approve must call approveTask for the gated task '
            '(not approvePlan on the plans.db gate)',
      );
    });

    testWidgets(
      'reject from the task card forwards the reason and never approves',
      (tester) async {
        final client = _GateTaskSdkClient(task: _awaitingTask());
        await _pumpPlansTab(tester, client: client);

        final reject = find.text('reject');
        expect(reject, findsWidgets, reason: 'card must exist before tapping');
        await tester.tap(reject.first);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 50));

        // Reason dialog: type a reason and confirm.
        const reason = 'too risky to run unattended';
        final field = find.byType(TextField);
        expect(field, findsWidgets, reason: 'reject must open a reason dialog');
        await tester.enterText(field.first, reason);
        await tester.pump();
        // The dialog confirm button is the FilledButton labeled reject.
        final confirm = find.widgetWithText(FilledButton, 'reject');
        expect(confirm, findsWidgets);
        await tester.tap(confirm.last);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 50));

        expect(
          client.rejectCalls['task-gate-gui-01'],
          reason,
          reason: 'C2: reject must forward the full reason via rejectTask',
        );
        expect(
          client.approveCalls,
          isEmpty,
          reason: 'C2 LOAD-BEARING: reject must NEVER approve/execute',
        );
      },
    );
  });
}
