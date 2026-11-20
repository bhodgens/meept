// 06-planning TEST-GATE T2 (F13b): GUI seal surface for compiler drafts.
//
// RED today for missing-implementation reasons:
//   - SdkApiClient has NO getDraft/sealTask callers for the plan.draft /
//     plan.seal RPCs (internal/rpc/plan_seal.go) nor their HTTP twins
//     (GET /api/v1/tasks/{id}/draft, POST /api/v1/tasks/{id}/seal - absent
//     daemon-side too; 06-planning/02 T2 adds them). The wire pins invoke
//     through `dynamic` so this file COMPILES today and fails at runtime
//     with NoSuchMethodError until the methods land.
//   - No seal widget exists: PlansTab shows phases/actions for plans.db
//     rows but never the draft markdown, a seal button, or compile
//     problems inline. A raw scaffold draft must WARN before seal and
//     never auto-seal.
//
// Contract pins (plan_seal.go handleSeal): a compile with problems
// returns {status:"problems", problems:[{line,message}]} - problems are
// a RESULT, rendered inline (line + message), the draft stays a draft.
// A clean seal returns {status:"sealed", hash, mode, warnings}.
//
// Gate tests: do not weaken. GREEN after 06-planning/02 T2.

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

/// Recording adapter (same shape as approval_card_test.dart's gate
/// adapter): canned JSON per path suffix, records method+path+body.
class _SealAdapter implements HttpClientAdapter {
  _SealAdapter();

  final captured = <_SealRequest>[];
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
    captured.add(_SealRequest(options.method, path, body));
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

class _SealRequest {
  _SealRequest(this.method, this.path, this.body);
  final String method;
  final String path;
  final Map<String, dynamic>? body;
}

SdkApiClient _sealClient(_SealAdapter adapter) {
  final client = SdkApiClient(host: 'localhost', port: 8081);
  client.dio.httpClientAdapter = adapter;
  return client;
}

const _draftMarkdown =
    '# Plan: Seal gate fixture\n\n## Meta\n\n- task_id: task-seal-gui-01\n'
    '- version: 1\n- status: draft\n\n## Goal\n\nShip the seal surface.\n\n'
    '## Decisions\n\n## Open Questions\n\n## Phases\n';

/// SDK stub serving one compiler-draft task (planning state, draft in
/// metadata) and empty plans.db rows. getDraft/sealTask record calls so
/// the widget pins can assert the seal flow without a network.
class _SealTaskSdkClient extends SdkApiClient {
  _SealTaskSdkClient({required this.task, this.problems = const []})
    : super(host: 'localhost', port: 65432);

  final Task task;
  final List<Map<String, dynamic>> problems;
  final draftReads = <String>[];
  final sealCalls = <String>[];

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
  Future<Map<String, dynamic>> getDraft(String id) async {
    draftReads.add(id);
    return {
      'task_id': id,
      'status': 'draft',
      'markdown': _draftMarkdown,
      'version': 1,
    };
  }

  @override
  Future<Map<String, dynamic>> sealTask(String id) async {
    sealCalls.add(id);
    if (problems.isNotEmpty) {
      return {
        'task_id': id,
        'status': 'problems',
        'problems': problems,
        'warnings': const [],
      };
    }
    return {
      'task_id': id,
      'status': 'sealed',
      'hash': 'abc123',
      'mode': 'flat',
      'warnings': const [],
    };
  }
}

Task _draftTask({String id = 'task-seal-gui-01'}) {
  return Task(
    id: id,
    title: 'Seal gate task',
    description: 'compiler draft fixture',
    status: 'planning',
    createdAt: DateTime.utc(2026, 11, 19),
    metadata: const {
      'plan_draft': {'markdown': _draftMarkdown, 'version': 1},
    },
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
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
  await tester.pump(const Duration(milliseconds: 50));
}

void main() {
  group('draft/seal SDK wire pins', () {
    test(
      'getDraft GETs /api/v1/tasks/{id}/draft and returns markdown',
      () async {
        final adapter = _SealAdapter()
          ..on('/draft', {
            'task_id': 'task-seal-gui-01',
            'status': 'draft',
            'markdown': _draftMarkdown,
            'version': 1,
          });
        final client = _sealClient(adapter);

        // dynamic: method absent today (NoSuchMethodError = RED).
        final result =
            await (client as dynamic).getDraft('task-seal-gui-01')
                as Map<String, dynamic>;

        expect(result['markdown'], _draftMarkdown);
        final gets = adapter.captured.where((r) => r.method == 'GET').toList();
        expect(gets, isNotEmpty, reason: 'getDraft must issue a GET');
        expect(gets.first.path, '/api/v1/tasks/task-seal-gui-01/draft');
      },
    );

    test('sealTask POSTs /api/v1/tasks/{id}/seal', () async {
      final adapter = _SealAdapter()
        ..on('/seal', {
          'task_id': 'task-seal-gui-01',
          'status': 'sealed',
          'hash': 'abc123',
          'mode': 'flat',
          'warnings': const [],
        });
      final client = _sealClient(adapter);

      final result =
          await (client as dynamic).sealTask('task-seal-gui-01')
              as Map<String, dynamic>;

      expect(result['status'], 'sealed');
      final posts = adapter.captured.where((r) => r.method == 'POST').toList();
      expect(posts, isNotEmpty, reason: 'sealTask must issue a POST');
      expect(posts.first.path, '/api/v1/tasks/task-seal-gui-01/seal');
    });
  });

  group('seal surface (widget pins)', () {
    testWidgets('compiler draft shows draft markdown + seal action', (
      tester,
    ) async {
      final client = _SealTaskSdkClient(task: _draftTask());
      await _pumpPlansTab(tester, client: client);

      // The draft surface must render the draft markdown (or its goal)
      // and a seal affordance keyed by the task.
      expect(
        find.textContaining('Ship the seal surface'),
        findsWidgets,
        reason:
            'F13b: the seal surface must show the draft markdown '
            '(today PlansTab never renders a compiler draft)',
      );
      expect(
        find.text('seal'),
        findsWidgets,
        reason: 'F13b: the seal surface must offer a seal action',
      );
    });

    testWidgets('seal returns compile problems inline (line + message)', (
      tester,
    ) async {
      final client = _SealTaskSdkClient(
        task: _draftTask(),
        problems: const [
          {'line': 12, 'message': 'phase "Build" names no decision artifact'},
        ],
      );
      await _pumpPlansTab(tester, client: client);

      final seal = find.text('seal');
      expect(seal, findsWidgets, reason: 'seal surface must exist first');
      await tester.tap(seal.first);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      expect(client.sealCalls, [
        'task-seal-gui-01',
      ], reason: 'seal must call sealTask for the draft task');
      // Problems are a RESULT rendered inline: line number + message.
      expect(
        find.textContaining('names no decision artifact'),
        findsWidgets,
        reason:
            'F13b: compile problems must render inline (message), not vanish '
            'into an error toast',
      );
      expect(
        find.textContaining('12'),
        findsWidgets,
        reason: 'F13b: compile problems must show the line number inline',
      );
    });

    testWidgets('raw scaffold warns before seal and never auto-seals', (
      tester,
    ) async {
      // A freshly seeded scaffold (empty Decisions/Phases) must surface a
      // warning and require an explicit user seal - never auto-seal.
      final client = _SealTaskSdkClient(task: _draftTask());
      await _pumpPlansTab(tester, client: client);

      expect(
        client.sealCalls,
        isEmpty,
        reason:
            'F13b LOAD-BEARING: a raw scaffold draft must NEVER auto-seal; '
            'sealing requires an explicit user action',
      );
      expect(
        find.textContaining('scaffold'),
        findsWidgets,
        reason:
            'F13b: a raw scaffold draft must warn the user before seal '
            '(empty Decisions/Phases = not ready to compile)',
      );
    });
  });
}
