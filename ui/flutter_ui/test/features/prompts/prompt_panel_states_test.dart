// Panel-level state coverage for the prompts panel: success, 503, 404, an
// empty list, and a malformed-shape error. These assert the *visible* failure
// mode so "the panel loads nothing" can never be silent again.
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/prompts/prompt_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Stub SdkApiClient whose listPromptsRaw returns canned data or throws a
/// canned error. Records the call count so retry can be observed.
class _PromptsStubClient extends SdkApiClient {
  _PromptsStubClient({this.summaries = const [], this.error})
      : super(host: 'localhost', port: 8081);

  List<Map<String, dynamic>> summaries;
  Object? error;
  int listCalls = 0;

  @override
  Future<List<Map<String, dynamic>>> listPromptsRaw() async {
    listCalls++;
    final e = error;
    if (e != null) throw e;
    return summaries.map((s) => Map<String, dynamic>.from(s)).toList();
  }
}

class _StubWebSocket extends WebSocketService {
  _StubWebSocket() : super(host: 'localhost', port: 8081);

  @override
  Future<void> connect({String? path}) async {}
  @override
  void disconnect() {}
  @override
  void send(Map<String, dynamic> message) {}
}

Map<String, dynamic> _summary({
  String name = 'planner/decompose.md',
  String tier = 'bundled',
  String sourcePath = 'config/prompts/planner/decompose.md',
}) =>
    {'name': name, 'tier': tier, 'source_path': sourcePath};

Widget _buildTestApp(SdkApiClient client) {
  final router = GoRouter(
    initialLocation: '/tools/prompts',
    routes: [
      GoRoute(
        path: '/tools/prompts',
        builder: (_, __) => const PromptPanel(),
      ),
      GoRoute(
        path: '/',
        builder: (_, __) => const Scaffold(body: SizedBox.shrink()),
      ),
    ],
  );

  return ProviderScope(
    overrides: [
      sdkClientProvider.overrideWith((_) => client),
      websocketProvider.overrideWith((_) => _StubWebSocket()),
    ],
    child: MaterialApp.router(routerConfig: router),
  );
}

Future<void> _settle(WidgetTester tester) async {
  await tester.pump(const Duration(milliseconds: 50));
  await tester.pump(const Duration(milliseconds: 50));
}

void main() {
  testWidgets('success: entries render', (tester) async {
    final client = _PromptsStubClient(
      summaries: [
        _summary(),
        _summary(name: 'planner/interview.md', tier: 'user'),
      ],
    );
    await tester.pumpWidget(_buildTestApp(client));
    await _settle(tester);

    expect(find.text('planner/decompose.md'), findsOneWidget);
    expect(find.text('planner/interview.md'), findsOneWidget);
    expect(find.text('no prompt templates found'), findsNothing);
    expect(find.text('failed to load prompts'), findsNothing);
  });

  testWidgets('503: actionable error is visible and retry re-requests',
      (tester) async {
    final client = _PromptsStubClient(
      error: SdkApiException(
        message: 'prompt service not available',
        statusCode: 503,
      ),
    );
    await tester.pumpWidget(_buildTestApp(client));
    await _settle(tester);

    expect(find.text('failed to load prompts'), findsOneWidget);
    expect(find.textContaining('prompt service unavailable'), findsOneWidget);
    expect(find.text('retry'), findsOneWidget);
    expect(client.listCalls, 1);

    // Recover: next load succeeds, retry should render the list.
    client.error = null;
    client.summaries = [_summary()];
    await tester.tap(find.text('retry'));
    await _settle(tester);

    expect(client.listCalls, 2);
    expect(find.text('failed to load prompts'), findsNothing);
    expect(find.text('planner/decompose.md'), findsOneWidget);
  });

  testWidgets('404: route-missing guidance is visible', (tester) async {
    final client = _PromptsStubClient(
      error: SdkApiException(
        message: 'Server error: 404',
        statusCode: 404,
      ),
    );
    await tester.pumpWidget(_buildTestApp(client));
    await _settle(tester);

    expect(find.text('failed to load prompts'), findsOneWidget);
    expect(find.textContaining('prompts endpoint not found'), findsOneWidget);
    expect(find.text('retry'), findsOneWidget);
  });

  testWidgets('malformed response shape is surfaced, not silent',
      (tester) async {
    final client = _PromptsStubClient(
      error: SdkApiException(
        message: 'malformed prompts response: missing "prompts" key '
            '(got keys [])',
        statusCode: 0,
      ),
    );
    await tester.pumpWidget(_buildTestApp(client));
    await _settle(tester);

    expect(find.text('failed to load prompts'), findsOneWidget);
    expect(find.textContaining('malformed prompts response'), findsOneWidget);
    expect(find.text('no prompt templates found'), findsNothing);
  });

  testWidgets('empty: visible empty message with retry', (tester) async {
    final client = _PromptsStubClient();
    await tester.pumpWidget(_buildTestApp(client));
    await _settle(tester);

    expect(find.text('no prompt templates found'), findsOneWidget);
    expect(
      find.text('bundled and project prompts will appear here'),
      findsOneWidget,
    );
    expect(find.textContaining('could not see its prompts directory'),
        findsOneWidget);
    expect(find.text('retry'), findsOneWidget);
  });
}
