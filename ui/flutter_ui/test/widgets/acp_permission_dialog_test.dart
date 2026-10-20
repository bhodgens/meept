// ACP human-permission relay dialog + host + status-bar badge
// (leaf 05, Contract E widget tests).
//
// Dialog-render/resolve-tap idiom mirrors the destructive-confirmation
// family: the dialog is state-driven off the provider, and tests mock the
// HTTP layer with an SdkApiClient subclass (create_event_dialog_test.dart
// idiom) while pushing WS frames through an injectable WebSocketService
// (job_provider_test.dart idiom).

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/core/router.dart' show rootNavigatorKey;
import 'package:meept_ui/models/acp_permission.dart';
import 'package:meept_ui/models/acp_status.dart';
import 'package:meept_ui/providers/acp_permission_provider.dart';
import 'package:meept_ui/providers/acp_status_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';
import 'package:meept_ui/widgets/acp_permission_dialog.dart';
import 'package:meept_ui/widgets/status_bar.dart';

class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  Map<String, dynamic> snapshot = {'enabled': true, 'pending': <Object>[]};
  Object? resolveError;
  final resolves = <List<String>>[];

  @override
  Future<Map<String, dynamic>> getAcpPermissions() async => snapshot;

  @override
  Future<Map<String, dynamic>> resolveAcpPermission({
    required String id,
    required String decision,
  }) async {
    final err = resolveError;
    if (err != null) throw err;
    resolves.add([id, decision]);
    return {'decision': decision, 'reason': 'answered'};
  }
}

class _InjectableWebSocket extends WebSocketService {
  _InjectableWebSocket()
    : _messages = StreamController<Map<String, dynamic>>.broadcast(sync: true),
      super(host: 'localhost', port: 8081);

  final StreamController<Map<String, dynamic>> _messages;

  @override
  Future<void> connect({String? path}) async {}

  @override
  void disconnect() {}

  @override
  bool get isConnected => false;

  void pushFrame(Map<String, dynamic> frame) => _messages.add(frame);

  @override
  Stream<Map<String, dynamic>> subscribeToAcpPermissions() => _messages.stream;
}

Map<String, dynamic> _requested({
  String id = 'perm-aaaa1111',
  String agentId = 'claude',
  String sessionId = 'sess-1',
  String summary = 'write file outside workspace',
  List<Map<String, dynamic>>? options,
}) => {
  'type': 'agent_progress',
  'source_topic': 'acp.permission.requested',
  'id': id,
  'agent_id': agentId,
  'session_id': sessionId,
  'summary': summary,
  'created_at': '2026-10-19T12:34:56Z',
  if (options != null) 'options': options,
};

// acp-always-allow leaf 01 wire shape: agent-offered options with an
// allow_always entry.
const _alwaysOptions = [
  {'optionId': 'allow-once-1', 'name': 'Allow once', 'kind': 'allow_once'},
  {
    'optionId': 'always-1',
    'name': 'Always allow in session',
    'kind': 'allow_always',
  },
  {'optionId': 'reject-1', 'name': 'Reject', 'kind': 'reject_once'},
];

Map<String, dynamic> _resolved({String id = 'perm-aaaa1111'}) => {
  'type': 'agent_progress',
  'source_topic': 'acp.permission.resolved',
  'id': id,
  'decision': 'allow',
  'reason': 'answered',
};

Widget _hostApp(_StubSdkClient client, _InjectableWebSocket ws) {
  return ProviderScope(
    overrides: [
      sdkClientProvider.overrideWith((_) => client),
      websocketProvider.overrideWith((_) => ws),
    ],
    child: MaterialApp(
      navigatorKey: rootNavigatorKey,
      home: const Scaffold(
        body: Center(child: Text('app body')),
        bottomNavigationBar: AcpPermissionDialogHost(),
      ),
    ),
  );
}

void main() {
  late _StubSdkClient client;
  late _InjectableWebSocket ws;

  setUp(() {
    client = _StubSdkClient();
    ws = _InjectableWebSocket();
  });

  group('AcpPermissionDialog rendering', () {
    testWidgets('shows agent, session, created, summary, request id short-', (
      tester,
    ) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((_) => client),
            websocketProvider.overrideWith((_) => ws),
          ],
          child: const MaterialApp(
            home: Scaffold(
              body: AcpPermissionDialog(
                request: AcpPendingPermission(
                  id: 'perm-aaaa1111bbbb',
                  agentId: 'claude',
                  sessionId: 'sess-42',
                  summary: 'run command outside workspace',
                  createdAt: '2026-10-19T12:34:56Z',
                ),
              ),
            ),
          ),
        ),
      );

      expect(find.byKey(const Key('acp-permission-dialog')), findsOneWidget);
      expect(find.text('run command outside workspace'), findsOneWidget);
      expect(find.text('agent: claude'), findsOneWidget);
      expect(find.text('session: sess-42'), findsOneWidget);
      // RFC3339 parsed and rendered (display TZ in tests is local).
      expect(find.textContaining('created: 2026-10-19'), findsOneWidget);
      // short form of the id (first 8 chars).
      expect(find.text('request: perm-aaa'), findsOneWidget);
      expect(find.text('allow'), findsOneWidget);
      expect(find.text('deny'), findsOneWidget);
      // No options on the request -> no third "always" button
      // (agent-declared capability, acp-always-allow leaf 02).
      expect(find.text('always'), findsNothing);
      expect(find.byKey(const Key('acp-permission-always')), findsNothing);
    });

    testWidgets('always button renders iff options carry allow_always', (
      tester,
    ) async {
      Widget wrap(AcpPendingPermission request) => ProviderScope(
        overrides: [
          sdkClientProvider.overrideWith((_) => client),
          websocketProvider.overrideWith((_) => ws),
        ],
        child: MaterialApp(
          home: Scaffold(body: AcpPermissionDialog(request: request)),
        ),
      );

      await tester.pumpWidget(
        wrap(
          const AcpPendingPermission(
            id: 'p1',
            agentId: 'a',
            sessionId: 's',
            summary: 'x',
            createdAt: '',
            options: [
              AcpPermissionOption(
                optionId: 'allow-once-1',
                name: 'Allow once',
                kind: 'allow_once',
              ),
              AcpPermissionOption(
                optionId: 'always-1',
                name: 'Always allow in session',
                kind: 'allow_always',
              ),
            ],
          ),
        ),
      );
      expect(find.byKey(const Key('acp-permission-always')), findsOneWidget);
      expect(find.text('always'), findsOneWidget);

      // A loosely-named kind (contains allow + always, case-insensitive)
      // counts too — mirrors resolveAlwaysOptionID's second pass.
      await tester.pumpWidget(
        wrap(
          const AcpPendingPermission(
            id: 'p2',
            agentId: 'a',
            sessionId: 's',
            summary: 'x',
            createdAt: '',
            options: [
              AcpPermissionOption(
                optionId: 'alw',
                name: 'Always',
                kind: 'Allow-Always-scoped',
              ),
            ],
          ),
        ),
      );
      expect(find.byKey(const Key('acp-permission-always')), findsOneWidget);

      // Options without any allow_always-kind entry -> no always button.
      await tester.pumpWidget(
        wrap(
          const AcpPendingPermission(
            id: 'p3',
            agentId: 'a',
            sessionId: 's',
            summary: 'x',
            createdAt: '',
            options: [
              AcpPermissionOption(
                optionId: 'allow-once-1',
                name: 'Allow once',
                kind: 'allow_once',
              ),
            ],
          ),
        ),
      );
      expect(find.byKey(const Key('acp-permission-always')), findsNothing);
    });

    testWidgets('bounded summary truncates past 240 chars with ellipsis', (
      tester,
    ) async {
      final long = 'x' * 300;
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((_) => client),
            websocketProvider.overrideWith((_) => ws),
          ],
          child: MaterialApp(
            home: Scaffold(
              body: AcpPermissionDialog(
                request: AcpPendingPermission(
                  id: 'p1',
                  agentId: 'a',
                  sessionId: 's',
                  summary: long,
                  createdAt: '',
                ),
              ),
            ),
          ),
        ),
      );
      expect(find.text('${'x' * 240}...'), findsOneWidget);
    });
  });

  group('host dialog lifecycle (state-driven)', () {
    testWidgets(
      'requested frame opens the dialog; allow tap POSTs and closes',
      (tester) async {
        await tester.pumpWidget(_hostApp(client, ws));
        await tester.pump();

        expect(find.byKey(const Key('acp-permission-dialog')), findsNothing);

        ws.pushFrame(_requested());
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 400)); // dialog route

        expect(find.byKey(const Key('acp-permission-dialog')), findsOneWidget);
        expect(find.text('write file outside workspace'), findsOneWidget);

        await tester.tap(find.byKey(const Key('acp-permission-allow')));
        await tester.pump(); // resolve POST future completes
        await tester.pump(const Duration(milliseconds: 400)); // pop anim

        expect(client.resolves, [
          ['perm-aaaa1111', 'allow'],
        ]);
        expect(find.byKey(const Key('acp-permission-dialog')), findsNothing);
      },
    );

    testWidgets('deny tap POSTs deny', (tester) async {
      await tester.pumpWidget(_hostApp(client, ws));
      await tester.pump();
      ws.pushFrame(_requested());
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      await tester.tap(find.byKey(const Key('acp-permission-deny')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(client.resolves.single, ['perm-aaaa1111', 'deny']);
      expect(find.byKey(const Key('acp-permission-dialog')), findsNothing);
    });

    testWidgets('always tap POSTs always (options-carrying frame)', (
      tester,
    ) async {
      await tester.pumpWidget(_hostApp(client, ws));
      await tester.pump();
      ws.pushFrame(_requested(options: _alwaysOptions));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(find.byKey(const Key('acp-permission-always')), findsOneWidget);
      await tester.tap(find.byKey(const Key('acp-permission-always')));
      await tester.pump(); // resolve POST future completes
      await tester.pump(const Duration(milliseconds: 400)); // pop anim

      expect(client.resolves.single, ['perm-aaaa1111', 'always']);
      expect(find.byKey(const Key('acp-permission-dialog')), findsNothing);
    });

    testWidgets('resolved frame (external answer/timeout) closes the dialog', (
      tester,
    ) async {
      await tester.pumpWidget(_hostApp(client, ws));
      await tester.pump();
      ws.pushFrame(_requested());
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byKey(const Key('acp-permission-dialog')), findsOneWidget);

      // Answered elsewhere (TUI/CLI) or timed out — no local POST.
      ws.pushFrame(_resolved());
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(client.resolves, isEmpty);
      expect(find.byKey(const Key('acp-permission-dialog')), findsNothing);
    });

    testWidgets('head swap: second request shows after the first clears', (
      tester,
    ) async {
      await tester.pumpWidget(_hostApp(client, ws));
      await tester.pump();
      ws.pushFrame(_requested(id: 'first', summary: 'first request'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.text('first request'), findsOneWidget);

      ws.pushFrame(_resolved(id: 'first'));
      ws.pushFrame(_requested(id: 'second', summary: 'second request'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(find.text('first request'), findsNothing);
      expect(find.text('second request'), findsOneWidget);
      expect(find.byKey(const Key('acp-permission-dialog')), findsOneWidget);
    });

    testWidgets('queue count visible: badge counts, dialog shows head', (
      tester,
    ) async {
      await tester.pumpWidget(_hostApp(client, ws));
      await tester.pump();
      ws.pushFrame(_requested(id: 'one', summary: 'head request'));
      ws.pushFrame(_requested(id: 'two', summary: 'queued request'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      // Dialog shows the HEAD only...
      expect(find.text('head request'), findsOneWidget);
      expect(find.text('queued request'), findsNothing);
      // ...with the stack counter in the title (documented choice: a
      // barrier dialog cannot stack visually, so the count rides the
      // title + the status-bar badge).
      expect(find.text('acp permission request (x2)'), findsOneWidget);
    });

    testWidgets('failed resolve keeps the dialog and shows the error', (
      tester,
    ) async {
      client.resolveError = Exception('http 500: relay full');
      await tester.pumpWidget(_hostApp(client, ws));
      await tester.pump();
      ws.pushFrame(_requested());
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      await tester.tap(find.byKey(const Key('acp-permission-allow')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(find.byKey(const Key('acp-permission-dialog')), findsOneWidget);
      expect(find.textContaining('resolve failed'), findsOneWidget);
    });
  });

  group('status-bar badge', () {
    late AcpPermissionNotifier notifier;

    setUp(() {
      notifier = AcpPermissionNotifier(sdkClient: client, websocket: ws);
    });

    tearDown(() {
      // The ProviderScope tears down the overridden notifier itself; only
      // dispose here if the test never pumped a scope.
      if (notifier.mounted) notifier.dispose();
    });

    Future<void> pumpBar(WidgetTester tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((_) => client),
            websocketProvider.overrideWith((_) => ws),
            acpPermissionProvider.overrideWith((_) => notifier),
            verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
            currentProjectProvider.overrideWith(
              (ref) => _NoopProjectNotifier(client),
            ),
            acpStatusProvider.overrideWith(
              (ref) async => AcpStatusView.disabled,
            ),
            activeSessionProvider.overrideWith((ref) => null),
          ],
          child: const MaterialApp(
            home: Scaffold(body: StatusBar(selectedTabIndex: 0)),
          ),
        ),
      );
      await tester.pump();
    }

    Finder findSpanText(String needle) => find.byWidgetPredicate(
      (w) => w is RichText && w.text.toPlainText().contains(needle),
    );

    testWidgets('hidden when nothing pending', (tester) async {
      await pumpBar(tester);
      expect(findSpanText('acp permissions pending'), findsNothing);
    });

    testWidgets('shows pending count as frames arrive, clears on resolve', (
      tester,
    ) async {
      await pumpBar(tester);

      ws.pushFrame(_requested(id: 'a'));
      ws.pushFrame(_requested(id: 'b'));
      await tester.pump();
      expect(findSpanText('2 acp permissions pending'), findsOneWidget);

      ws.pushFrame(_resolved(id: 'a'));
      await tester.pump();
      expect(findSpanText('1 acp permissions pending'), findsOneWidget);

      ws.pushFrame(_resolved(id: 'b'));
      await tester.pump();
      expect(findSpanText('acp permissions pending'), findsNothing);
    });
  });
}

/// Project notifier that never touches the network (status-bar hermetic
/// idiom copied from test/widgets/status_bar_test.dart).
class _NoopProjectNotifier extends CurrentProjectNotifier {
  _NoopProjectNotifier(super.client);
}
