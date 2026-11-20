// 05-navigation TEST-GATE T4 (leaf 00T-gate.md, matrix F12b; master C4).
//
// Pins the GUI tool-trace expand: a completed reply bubble expands to
// show the PERSISTED tool calls fetched via session messages with
// include_tool_calls=true (C4 wire shape: tool_calls
// []{id,name,args_summary,result_summary,at}). Today the bubble has no
// expand affordance and getMessagesPage sends no includeToolCalls param.
//
// RED today: no 'trace' affordance exists on the bubble and no SDK
// request carries include_tool_calls. The C4 shape is pinned through a
// local Dio adapter (existing dio dep; no new test infra) so the HTTP
// query param is observable, plus the rendered rows.

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Records every request path+query the SDK issues and answers the
/// messages route with the C4 include_tool_calls shape.
class _RecordingAdapter implements HttpClientAdapter {
  final requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<List<int>>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    if (options.path.contains('/messages')) {
      return ResponseBody.fromString(
        '{"messages":[{"id":7,"role":"assistant","content":"quartz reply text",'
        '"tool_calls":[{"id":"tc-1","name":"file_write",'
        '"args_summary":"quartz.txt","result_summary":"wrote 11 bytes",'
        '"at":"2026-11-19T10:00:00Z"}]}],"total":1}',
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

/// SDK client wired to the recording adapter (native test platform).
/// The public `dio` getter is the existing test seam (kanban_service_test
/// and list_prompts_raw_test use the same pattern).
SdkApiClient _recordingClient(_RecordingAdapter adapter) {
  final client = SdkApiClient(host: 'localhost', port: 65432);
  client.dio.httpClientAdapter = adapter;
  return client;
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

  group('GUI trace expand (05-navigation T4, C4)', () {
    testWidgets('TestTraceExpand: expand shows persisted tool calls', (
      tester,
    ) async {
      final adapter = _RecordingAdapter();
      final client = _recordingClient(adapter);
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

      // The expand affordance must exist on a correlated reply.
      final traceFinder = find.textContaining('trace');
      expect(
        traceFinder,
        findsWidgets,
        reason:
            'reply bubble has no trace-expand affordance '
            '(C4 read surface unwired; chat_message_bubble.dart)',
      );

      await tester.tap(traceFinder.first);
      await tester.pumpAndSettle();

      // The fetch must request include_tool_calls=true (C4 param).
      final messagesReqs = adapter.requests
          .where((r) => r.path.contains('/messages'))
          .toList();
      expect(
        messagesReqs,
        isNotEmpty,
        reason: 'trace expand fetched no session messages',
      );
      expect(
        messagesReqs.any(
          (r) =>
              r.queryParameters['include_tool_calls'] == true ||
              r.queryParameters['include_tool_calls'] == 'true',
        ),
        isTrue,
        reason:
            'messages fetch missing include_tool_calls=true '
            '(C4; getMessagesPage has no includeToolCalls param)',
      );

      // Persisted call rows render: name + args/result summaries.
      expect(
        find.textContaining('file_write'),
        findsWidgets,
        reason: 'expanded trace shows no persisted tool-call name',
      );
      expect(
        find.textContaining('quartz.txt'),
        findsWidgets,
        reason: 'expanded trace shows no args_summary',
      );

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });

    test('C4 wire shape: tool_calls rows parse with the frozen keys', () {
      // Shape pin independent of the widget: the C4 row keys must parse
      // into the message model's trace surface (additive, never renamed).
      final msg = ChatMessage.fromBackendMessage({
        'id': 7,
        'role': 'assistant',
        'content': 'quartz reply text',
        'timestamp': '2026-11-19T10:00:00Z',
        'tool_calls': [
          {
            'id': 'tc-1',
            'name': 'file_write',
            'args_summary': 'quartz.txt',
            'result_summary': 'wrote 11 bytes',
            'at': '2026-11-19T10:00:00Z',
          },
        ],
      });
      final json = msg.toJson();
      final calls = json['tool_calls'];
      expect(calls, isA<List<dynamic>>());
      final rows = calls as List<dynamic>;
      expect(rows, hasLength(1));
      final row = rows.single;
      expect(
        row,
        isA<Map<String, dynamic>>(),
        reason:
            'C4 tool_calls rows must stay structured objects '
            '{id,name,args_summary,result_summary,at} - today the model '
            'stringifies them (api_models.dart fromBackendMessage)',
      );
      final map = row as Map<String, dynamic>;
      expect(map['name'], 'file_write');
      expect(map['args_summary'], 'quartz.txt');
      expect(map['result_summary'], 'wrote 11 bytes');
    });
  });
}
