// 05-navigation TEST-GATE T4 (leaf 00T-gate.md, matrix F12b; master C5).
//
// Pins the GUI why-this-route popover: a correlated reply offers a
// route-evidence affordance; opening it fetches the C5 dispatch-trace
// HTTP route (GET /api/v1/sessions/{id}/dispatch-trace) and renders the
// intent/method/confidence rows (rows carry turn_id + populated
// input_summary per C5). Today no popover widget exists and the SDK has
// no getDispatchTrace method.
//
// RED today: no 'route' affordance on the bubble; no dispatch-trace
// request is ever issued. The HTTP seam is observed through a local Dio
// adapter (existing test pattern), so the gate compiles at HEAD.

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Answers the C5 dispatch-trace route with one frozen-shape row and
/// records every request.
class _TraceAdapter implements HttpClientAdapter {
  final requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<List<int>>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    if (options.path.contains('dispatch-trace')) {
      return ResponseBody.fromString(
        '{"entries":[{"session_id":"test-session","turn_id":"turn-1",'
        '"input_summary":"dispatch the quartz work","intent_type":"code",'
        '"agent_id":"executor","confidence":0.95,"classifier_method":"llm",'
        '"handler_case":"task_dispatch","task_id":"task-42"}],"count":1}',
        200,
        headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        },
      );
    }
    return ResponseBody.fromString(
      '{}',
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

ChatMessage _reply() => ChatMessage.fromJson({
  'id': 'turn_turn-1',
  'role': 'assistant',
  'content': 'quartz reply text',
  'timestamp': DateTime.now().toIso8601String(),
  'session_id': 'test-session',
  'task_id': 'task-42',
  'intent_type': 'code',
  'handler_case': 'task_dispatch',
});

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GUI dispatch popover (05-navigation T4, C5)', () {
    testWidgets('TestDispatchPopover: popover fetches dispatch-trace rows', (
      tester,
    ) async {
      final adapter = _TraceAdapter();
      final client = SdkApiClient(host: 'localhost', port: 65432);
      client.dio.httpClientAdapter = adapter;
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWith((_) => client)],
      );
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
            home: Scaffold(
              body: SingleChildScrollView(
                child: ChatMessageBubble(message: _reply()),
              ),
            ),
          ),
        ),
      );
      await tester.pump();

      // The why-this-route affordance must exist on a correlated reply.
      final routeFinder = find.textContaining('route');
      expect(
        routeFinder,
        findsWidgets,
        reason:
            'reply bubble has no why-this-route affordance '
            '(C5 popover unwired; W22/N7)',
      );

      await tester.tap(routeFinder.first);
      await tester.pumpAndSettle();

      // C5 route: GET /api/v1/sessions/{id}/dispatch-trace.
      final traceReqs = adapter.requests
          .where((r) => r.path.contains('dispatch-trace'))
          .toList();
      expect(
        traceReqs,
        isNotEmpty,
        reason:
            'popover issued no dispatch-trace request '
            '(SDK has no getDispatchTrace; C5 route unused)',
      );
      expect(
        traceReqs.any((r) => r.path.contains('test-session')),
        isTrue,
        reason: 'dispatch-trace request not keyed on the reply session',
      );

      // Evidence rows render: intent / method / confidence.
      expect(
        find.textContaining('task_dispatch'),
        findsWidgets,
        reason: 'popover shows no handler_case row',
      );
      expect(
        find.textContaining('llm'),
        findsWidgets,
        reason: 'popover shows no classifier_method row',
      );
      expect(
        find.textContaining('0.95'),
        findsWidgets,
        reason: 'popover shows no confidence row',
      );

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });

    testWidgets('C5 rows: turn_id + populated input_summary render', (
      tester,
    ) async {
      // C5 freezes turn_id and a POPULATED input_summary on every row;
      // the popover must surface the turn correlation, not just counts.
      final adapter = _TraceAdapter();
      final client = SdkApiClient(host: 'localhost', port: 65432);
      client.dio.httpClientAdapter = adapter;
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWith((_) => client)],
      );
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
            home: Scaffold(
              body: SingleChildScrollView(
                child: ChatMessageBubble(message: _reply()),
              ),
            ),
          ),
        ),
      );
      await tester.pump();

      final routeFinder = find.textContaining('route');
      expect(
        routeFinder,
        findsWidgets,
        reason: 'no why-this-route affordance (C5 popover unwired)',
      );
      await tester.tap(routeFinder.first);
      await tester.pumpAndSettle();

      expect(
        find.textContaining('dispatch the quartz work'),
        findsWidgets,
        reason: 'popover row missing populated input_summary (C5)',
      );

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });
  });
}
