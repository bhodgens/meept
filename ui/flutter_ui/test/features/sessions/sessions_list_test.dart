import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/home/home_screen.dart' show HomeTab;
import 'package:meept_ui/features/sessions/sessions_list.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/tab_activation_provider.dart';
import 'package:meept_ui/providers/status_message_provider.dart';
import 'package:meept_ui/services/session_notifier.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/services/sdk_client.dart';

void main() {
  group('SessionsList widget', () {
    testWidgets('displays loading indicator when loading', (tester) async {
      final client = _SlowLoadSdkClient();

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) => SessionNotifier(sdkClient: client)),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );

      // initState callback fires after addPostFrameCallback (first pump)
      // _SlowLoadSdkClient has a 50ms initial delay, so we pump once to trigger load
      await tester.pump();
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Advance past the 50ms delay + settle
      await tester.pumpAndSettle();
    });

    testWidgets('displays "no sessions" when load succeeds but list is empty',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith(
                (ref) => SessionNotifier(sdkClient: _TestSdkClient([]))),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );

      await tester.pumpAndSettle();

      expect(find.text('no sessions'), findsOneWidget);
    });

    testWidgets('displays session tiles when sessions exist',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) {
              final notifier = SessionNotifier(sdkClient: _TestSdkClient(_testSessions));
              return notifier;
            }),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );

      await tester.pumpAndSettle();

      expect(find.text('test session'), findsOneWidget);
      expect(find.text('another session'), findsOneWidget);
    });

    testWidgets('selects session on tap, verifies activeSessionProvider updated',
        (tester) async {
      final session = Session(
        id: '1',
        title: 'Test Session',
        createdAt: DateTime.now(),
      );

      Session? capturedActiveSession;

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) {
              return SessionNotifier(sdkClient: _TestSdkClient([session]));
            }),
            activeSessionProvider.overrideWith((ref) => null),
          ],
          child: Consumer(
            builder: (context, ref, _) {
              capturedActiveSession = ref.watch(activeSessionProvider);
              return MaterialApp(
                home: Scaffold(
                  body: Column(
                    children: [
                      const SizedBox(
                        height: 400,
                        child: SessionsList(),
                      ),
                      Text(
                        'active: ${capturedActiveSession?.id ?? "none"}',
                      ),
                    ],
                  ),
                ),
              );
            },
          ),
        ),
      );

      await tester.pumpAndSettle();

      // Initially shows "none"
      expect(find.text('active: none'), findsOneWidget);

      // Tap on the session tile text to select it
      await tester.tap(find.text('test session'));
      // InkWell delays onTap when onDoubleTap is present; pump past the double-tap window
      await tester.pump(const Duration(milliseconds: 350));
      await tester.pumpAndSettle();

      // Now shows session id '1'
      expect(find.text('active: 1'), findsOneWidget);
    });

    testWidgets(
        'double-tap sets tabActivationProvider to chat and active session',
        (tester) async {
      final session = Session(
        id: 'dbl1',
        title: 'double tap me',
        createdAt: DateTime.now(),
      );

      // Use a ProviderContainer so we can read providers directly after the
      // widget tree is torn down.
      final container = ProviderContainer(
        overrides: [
          sessionProvider.overrideWith((ref) =>
              SessionNotifier(sdkClient: _TestSdkClient([session]))),
        ],
      );
      addTearDown(container.dispose);

      // GoRouter so `context.go('/')` in onDoubleTap doesn't throw.
      final router = GoRouter(
        initialLocation: '/sessions',
        routes: [
          GoRoute(
            path: '/sessions',
            builder: (_, __) =>
                const Scaffold(body: SizedBox(width: 400, child: SessionsList())),
          ),
          GoRoute(
            path: '/',
            builder: (_, __) => const Scaffold(body: SizedBox.shrink()),
          ),
        ],
      );

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(routerConfig: router),
        ),
      );
      await tester.pumpAndSettle();

      // Double-tap the session title
      await tester.tap(find.text('double tap me'));
      await tester.pump(const Duration(milliseconds: 50));
      await tester.tap(find.text('double tap me'));
      await tester.pumpAndSettle();

      expect(container.read(tabActivationProvider), HomeTab.chat);
      expect(container.read(activeSessionProvider)?.id, 'dbl1');
    });

    testWidgets('shows create session dialog when + button is pressed',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith(
                (ref) => SessionNotifier(sdkClient: _TestSdkClient([]))),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );

      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.add));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('create session'), findsOneWidget);
      expect(find.widgetWithText(TextButton, 'cancel'), findsOneWidget);
      expect(find.widgetWithText(FilledButton, 'create'), findsOneWidget);
    });

    testWidgets('archive confirmation shows when archive icon pressed',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) {
              final notifier = SessionNotifier(sdkClient: _TestSdkClient([
                Session(
                  id: '1',
                  title: 'Archive Me',
                  createdAt: DateTime.now(),
                ),
              ]));
              return notifier;
            }),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );

      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.archive_outlined));
      // InkWell delays onTap when onDoubleTap is present; pump past the double-tap window
      await tester.pump(const Duration(milliseconds: 350));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('archive session?'), findsOneWidget);
    });

    // Regression: archive status message must NOT fire synchronously before
    // the RPC resolves. If the RPC fails, the user should see an error
    // status, never a premature "archived: X". Parity with TUI's
    // SessionArchivedMsg-based async-status pattern.
    testWidgets('archive failure does not show premature success status',
        (tester) async {
      final container = ProviderContainer(
        overrides: [
          sessionProvider.overrideWith((ref) => SessionNotifier(
              sdkClient: _ArchiveThrowingSdkClient())),
        ],
      );
      addTearDown(container.dispose);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Open archive dialog and tap "archive".
      await tester.tap(find.byIcon(Icons.archive_outlined));
      await tester.pump(const Duration(milliseconds: 350));
      await tester.pumpAndSettle();

      // Before tapping, no status message should be set.
      expect(container.read(statusMessageProvider), isNull);

      await tester.tap(find.widgetWithText(FilledButton, 'archive'));
      await tester.pumpAndSettle();

      // After RPC fails, status must reflect failure, NOT "archived: ...".
      final status = container.read(statusMessageProvider);
      expect(status, isNotNull);
      expect(status!.startsWith('archived:'), isFalse,
          reason: 'status must not report success when RPC failed');
      expect(status.contains('failed'), isTrue);

      // Advance past the 2.5s auto-clear Timer so the test framework's
      // "no pending timers" assertion doesn't fire.
      await tester.pump(const Duration(seconds: 3));
    });

    // Regression: delete status message must NOT fire synchronously before
    // the RPC resolves. Parity with TUI SessionDeletedMsg fix.
    testWidgets('delete failure does not show premature success status',
        (tester) async {
      // Use a client that succeeds on listSessions but throws on delete.
      final container = ProviderContainer(
        overrides: [
          sessionProvider.overrideWith((ref) => SessionNotifier(
              sdkClient: _DeleteThrowingSdkClient())),
        ],
      );
      addTearDown(container.dispose);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Long-press to open context menu, then tap "delete permanently".
      await tester.longPress(find.text('archive me'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('delete permanently'));
      await tester.pumpAndSettle();

      expect(container.read(statusMessageProvider), isNull);

      await tester.tap(find.widgetWithText(FilledButton, 'delete'));
      await tester.pumpAndSettle();

      final status = container.read(statusMessageProvider);
      expect(status, isNotNull);
      expect(status!.startsWith('deleted:'), isFalse,
          reason: 'status must not report success when RPC failed');
      expect(status.contains('failed'), isTrue);

      // Advance past the 2.5s auto-clear Timer.
      await tester.pump(const Duration(seconds: 3));
    });
  });

  group('SessionNotifier', () {
    test('state starts empty', () {
      final notifier = SessionNotifier(sdkClient: _TestSdkClient([]));
      expect(notifier.state.sessions, isEmpty);
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
    });

    test('loadSessions populates sessions', () async {
      final notifier = SessionNotifier(sdkClient: _TestSdkClient(_testSessions));
      await notifier.loadSessions();

      expect(notifier.state.sessions, hasLength(_testSessions.length));
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
    });

    test('loadSessions sets error on failure', () async {
      final notifier = SessionNotifier(sdkClient: _ThrowingSdkClient());
      await notifier.loadSessions();

      expect(notifier.state.sessions, isEmpty);
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNotNull);
    });

    test('createSession appends new session', () async {
      final client = _TestSdkClient(_testSessions);
      final notifier = SessionNotifier(sdkClient: client);
      await notifier.loadSessions();

      final count = notifier.state.sessions.length;
      await notifier.createSession('New Session');
      expect(notifier.state.sessions.length, count + 1);
    });

    test('deleteSession removes session', () async {
      final client = _TestSdkClient(_testSessions);
      final notifier = SessionNotifier(sdkClient: client);
      await notifier.loadSessions();

      final firstId = _testSessions[0].id;
      await notifier.deleteSession(firstId);
      expect(notifier.state.sessions, hasLength(_testSessions.length - 1));
    });

    // Regression: stale error must be cleared on every success path.
    // SessionState.copyWith uses an _unset sentinel, so omitting `error:`
    // preserves any prior error — once a banner shows it stays stuck.
    test('deleteSession clears prior error on success', () async {
      final client = _TestSdkClient(_testSessions);
      final notifier = SessionNotifier(sdkClient: client);
      // Seed an error via a failing load.
      final throwing = SessionNotifier(sdkClient: _ThrowingSdkClient());
      await throwing.loadSessions();
      expect(throwing.state.error, isNotNull);
      // Simulate error carrying over by copying state into `notifier`.
      notifier.state = notifier.state.copyWith(error: throwing.state.error);
      expect(notifier.state.error, isNotNull);

      await notifier.deleteSession(_testSessions[0].id);
      expect(notifier.state.error, isNull);
    });

    test('archiveSession clears prior error on success', () async {
      final client = _TestSdkClient(_testSessions);
      final notifier = SessionNotifier(sdkClient: client);
      await notifier.loadSessions();
      // Seed an error.
      notifier.state = notifier.state.copyWith(error: 'prior failure');
      expect(notifier.state.error, isNotNull);

      await notifier.archiveSession(_testSessions[0].id);
      expect(notifier.state.error, isNull);
    });

    test('unarchiveSession clears prior error on success', () async {
      final client = _TestSdkClient(_testSessions);
      final notifier = SessionNotifier(sdkClient: client);
      await notifier.loadSessions();
      // Seed an error.
      notifier.state = notifier.state.copyWith(error: 'prior failure');
      expect(notifier.state.error, isNotNull);

      await notifier.unarchiveSession(_testSessions[0].id);
      expect(notifier.state.error, isNull);
    });
  });
}

// ===== Test helpers =====

final _testSessions = [
  Session(
    id: '1',
    title: 'Test Session',
    createdAt: DateTime(2025, 1, 1),
    lastActivity: DateTime(2025, 1, 15),
  ),
  Session(
    id: '2',
    title: 'Another Session',
    createdAt: DateTime(2025, 2, 1),
    lastActivity: DateTime(2025, 2, 10),
  ),
];

/// Test SdkApiClient that returns a predefined list of sessions.
/// `_sessions` is returned verbatim; `_localSessions` is mutated by
/// create/delete so the create-then-list flow has deterministic behavior.
class _TestSdkClient extends SdkApiClient {
  final List<Session> _sessions;
  final List<Session> _localSessions = [];

  _TestSdkClient(this._sessions) : super(host: 'localhost', port: 65432);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    return [..._sessions, ..._localSessions].map((s) => s.toJson()).toList();
  }

  @override
  Future<Map<String, dynamic>> createSession({
    required String title,
    String? agentId,
  }) async {
    final session = Session(
      id: 'new-${_localSessions.length + 1}',
      title: title,
      createdAt: DateTime.now(),
    );
    _localSessions.add(session);
    return session.toJson();
  }

  @override
  Future<void> deleteSession(String id) async {
    _localSessions.removeWhere((s) => s.id == id);
    // Also allow deletion from the seed list (tests assert length shrink).
    // No-op for seed list here because _sessions is final; tests that need
    // delete-from-seed semantics build a client with the seed and rely on
    // the notifier filtering locally.
  }

  @override
  Future<void> archiveSession(String sessionId, {required bool archived}) async {
    // No-op: the notifier flips the flag locally. Tests only assert state.
  }
}

/// Client with a short delay to simulate async load.
class _SlowLoadSdkClient extends SdkApiClient {
  _SlowLoadSdkClient() : super(host: 'localhost', port: 65431);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    // 50ms delay so loading state is visible
    await Future.delayed(const Duration(milliseconds: 50));
    return [];
  }
}

/// Client that throws on listSessions.
class _ThrowingSdkClient extends SdkApiClient {
  _ThrowingSdkClient() : super(host: 'localhost', port: 65433);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    throw Exception('connection refused');
  }
}

/// Client that throws on archiveSession — used to verify the UI does NOT
/// report success prematurely when the RPC fails (parity with TUI).
class _ArchiveThrowingSdkClient extends SdkApiClient {
  _ArchiveThrowingSdkClient()
      : super(host: 'localhost', port: 65434);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    return [
      Session(id: '1', title: 'archive me', createdAt: DateTime(2025, 1, 1))
          .toJson(),
    ];
  }

  @override
  Future<void> archiveSession(String sessionId, {required bool archived}) async {
    throw Exception('archive rpc failed');
  }
}

/// Client that throws on deleteSession — used to verify the UI does NOT
/// report success prematurely when the RPC fails (parity with TUI).
class _DeleteThrowingSdkClient extends SdkApiClient {
  _DeleteThrowingSdkClient()
      : super(host: 'localhost', port: 65435);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    return [
      Session(id: '1', title: 'archive me', createdAt: DateTime(2025, 1, 1))
          .toJson(),
    ];
  }

  @override
  Future<void> deleteSession(String id) async {
    throw Exception('delete rpc failed');
  }
}
