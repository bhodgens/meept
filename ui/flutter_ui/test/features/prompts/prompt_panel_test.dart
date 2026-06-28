import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/prompts/prompt_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Stub SdkApiClient that overrides the prompt methods.
class _PromptsStubClient extends SdkApiClient {
  _PromptsStubClient(this._summaries, {this.detailOverride})
      : super(host: 'localhost', port: 8081);

  final List<Map<String, dynamic>> _summaries;
  final Map<String, Map<String, dynamic>>? detailOverride;

  int validateCallCount = 0;
  int deleteCallCount = 0;
  int putCallCount = 0;
  String? lastValidateName;
  String? lastDeletedPath;
  String? lastPutPath;
  String? lastPutContent;

  @override
  Future<List<Map<String, dynamic>>> listPromptsRaw() async {
    return List<Map<String, dynamic>>.from(
      _summaries.map((p) => Map<String, dynamic>.from(p)),
    );
  }

  @override
  Future<Map<String, dynamic>> getPromptRaw(String path) async {
    if (detailOverride != null && detailOverride!.containsKey(path)) {
      return Map<String, dynamic>.from(detailOverride![path]!);
    }
    // Fall back to the summary entry + empty content.
    final match = _summaries.firstWhere(
      (s) => s['name'] == path,
      orElse: () => {'name': path, 'tier': 'bundled', 'source_path': ''},
    );
    return Map<String, dynamic>.from(match)..['content'] = '';
  }

  @override
  Future<Map<String, dynamic>> putPromptRaw(
      String path, String content) async {
    putCallCount++;
    lastPutPath = path;
    lastPutContent = content;
    return {'keystatus': 'saved', 'path': '/home/u/.meept/prompts/$path'};
  }

  @override
  Future<void> deletePromptRaw(String path) async {
    deleteCallCount++;
    lastDeletedPath = path;
    _summaries.removeWhere((s) => s['name'] == path);
  }

  @override
  Future<Map<String, dynamic>> validatePromptRaw(String? name) async {
    validateCallCount++;
    lastValidateName = name;
    if (name != null && name.isNotEmpty) {
      return {'name': name, 'valid': true};
    }
    return {'valid': true, 'errors': <Map<String, dynamic>>[], 'checked': 0};
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
  // Provide a GoRouter so `context.go('/')` in _closePanel works.
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

void main() {
  testWidgets('renders placeholder when no prompts', (tester) async {
    final client = _PromptsStubClient([]);
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('no prompt templates found'), findsOneWidget);
    expect(
        find.text('bundled and project prompts will appear here'),
        findsOneWidget);
  });

  testWidgets('renders prompt entries when present', (tester) async {
    final client = _PromptsStubClient([
      _summary(),
      _summary(name: 'planner/interview.md', tier: 'user'),
    ]);
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Both names should appear (lower-cased per CLAUDE.md convention).
    expect(find.text('planner/decompose.md'), findsOneWidget);
    expect(find.text('planner/interview.md'), findsOneWidget);

    // Tier badges render.
    expect(find.text('bundled'), findsOneWidget);
    expect(find.text('user'), findsOneWidget);
  });

  testWidgets('tapping a prompt opens detail view', (tester) async {
    final client = _PromptsStubClient(
      [_summary()],
      detailOverride: {
        'planner/decompose.md': {
          'name': 'planner/decompose.md',
          'tier': 'bundled',
          'source_path': 'config/prompts/planner/decompose.md',
          'modified': '2026-06-27T10:00:00Z',
          'content': 'PLAN: {{.Input}}',
        },
      },
    );
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Tap on the list entry.
    await tester.tap(find.text('planner/decompose.md'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));

    // Detail view shows the content and validate / copy buttons.
    expect(find.text('PLAN: {{.Input}}'), findsOneWidget);
    expect(find.text('validate'), findsOneWidget);
    expect(find.text('copy to override'), findsOneWidget);
  });

  testWidgets(
      'delete override button only appears for user tier (detail view)',
      (tester) async {
    final client = _PromptsStubClient(
      [
        _summary(name: 'user/x.md', tier: 'user'),
        _summary(name: 'bundled/y.md', tier: 'bundled'),
      ],
      detailOverride: {
        'user/x.md': {
          'name': 'user/x.md',
          'tier': 'user',
          'source_path': '/home/u/.meept/prompts/user/x.md',
          'modified': '2026-06-27T10:00:00Z',
          'content': 'override',
        },
        'bundled/y.md': {
          'name': 'bundled/y.md',
          'tier': 'bundled',
          'source_path': 'config/prompts/bundled/y.md',
          'modified': '2026-06-27T10:00:00Z',
          'content': 'bundled',
        },
      },
    );
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Open user-tier prompt — delete should be visible.
    await tester.tap(find.text('user/x.md'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));
    expect(find.text('delete override'), findsOneWidget);

    // Back to list.
    await tester.tap(find.byTooltip('back'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));

    // Open bundled-tier prompt — delete should NOT be visible.
    await tester.tap(find.text('bundled/y.md'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));
    expect(find.text('delete override'), findsNothing);
  });

  testWidgets('validate button calls validate endpoint', (tester) async {
    final client = _PromptsStubClient(
      [_summary()],
      detailOverride: {
        'planner/decompose.md': {
          'name': 'planner/decompose.md',
          'tier': 'bundled',
          'source_path': '',
          'modified': '2026-06-27T10:00:00Z',
          'content': 'X',
        },
      },
    );
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Open detail.
    await tester.tap(find.text('planner/decompose.md'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));

    // Tap validate.
    await tester.tap(find.text('validate'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.validateCallCount, 1);
    expect(client.lastValidateName, 'planner/decompose.md');
    // Success snackbar text.
    expect(find.textContaining('valid:'), findsOneWidget);
  });

  testWidgets('copy-to-override calls PUT endpoint', (tester) async {
    final client = _PromptsStubClient(
      [_summary()],
      detailOverride: {
        'planner/decompose.md': {
          'name': 'planner/decompose.md',
          'tier': 'bundled',
          'source_path': '',
          'modified': '2026-06-27T10:00:00Z',
          'content': 'BODY',
        },
      },
    );
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Open detail.
    await tester.tap(find.text('planner/decompose.md'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));

    // Tap copy-to-override.
    await tester.tap(find.text('copy to override'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.putCallCount, 1);
    expect(client.lastPutPath, 'planner/decompose.md');
    expect(client.lastPutContent, 'BODY');
    // Success snackbar.
    expect(find.textContaining('copied to user override'), findsOneWidget);
  });

  testWidgets('delete override calls DELETE and pops back to list',
      (tester) async {
    final client = _PromptsStubClient(
      [_summary(name: 'user/x.md', tier: 'user')],
      detailOverride: {
        'user/x.md': {
          'name': 'user/x.md',
          'tier': 'user',
          'source_path': '/home/u/.meept/prompts/user/x.md',
          'modified': '2026-06-27T10:00:00Z',
          'content': 'X',
        },
      },
    );
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Sanity: list shows the user-tier entry.
    expect(find.text('user/x.md'), findsOneWidget);

    // Open detail.
    await tester.tap(find.text('user/x.md'));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));

    // Tap delete.
    await tester.tap(find.text('delete override'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pumpAndSettle(const Duration(milliseconds: 200));

    expect(client.deleteCallCount, 1);
    expect(client.lastDeletedPath, 'user/x.md');

    // We should be back on the list route, which now refreshes to empty.
    expect(find.text('no prompt templates found'), findsOneWidget);
  });

  testWidgets('shows error state when list fails', (tester) async {
    final client = _PromptsErrorClient();
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('failed to load prompts'), findsOneWidget);
    expect(find.text('retry'), findsOneWidget);
  });
}

/// Client that always throws on listPromptsRaw to exercise the error path.
class _PromptsErrorClient extends SdkApiClient {
  _PromptsErrorClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> listPromptsRaw() async {
    throw SdkApiException(
      message: 'daemon unreachable',
      statusCode: 0,
    );
  }
}
