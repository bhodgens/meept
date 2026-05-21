import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/sessions/sessions_list.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/session_notifier.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/services/api_client.dart';

void main() {
  group('SessionsList widget', () {
    testWidgets('displays loading indicator when loading', (tester) async {
      final client = _SlowLoadClient();

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) => SessionNotifier(apiClient: client)),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SessionsList()),
          ),
        ),
      );

      // initState callback fires after addPostFrameCallback (first pump)
      // _SlowLoadClient has a 50ms initial delay, so we pump once to trigger load
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
                (ref) => SessionNotifier(apiClient: _TestApiClient([]))),
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
              final notifier = SessionNotifier(apiClient: _TestApiClient(_testSessions));
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
        updatedAt: DateTime.now(),
      );

      Session? capturedActiveSession;

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) {
              return SessionNotifier(apiClient: _TestApiClient([session]));
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
                      SizedBox(
                        height: 400,
                        child: const SessionsList(),
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
      await tester.pumpAndSettle();

      // Now shows session id '1'
      expect(find.text('active: 1'), findsOneWidget);
    });

    testWidgets('shows create session dialog when + button is pressed',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith(
                (ref) => SessionNotifier(apiClient: _TestApiClient([]))),
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

    testWidgets('delete confirmation shows when delete icon pressed',
        (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith((ref) {
              final notifier = SessionNotifier(apiClient: _TestApiClient([
                Session(
                  id: '1',
                  title: 'Delete Me',
                  createdAt: DateTime.now(),
                  updatedAt: DateTime.now(),
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

      await tester.tap(find.byIcon(Icons.delete_outline));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('delete session?'), findsOneWidget);
    });
  });

  group('SessionNotifier', () {
    test('state starts empty', () {
      final notifier = SessionNotifier(apiClient: _TestApiClient([]));
      expect(notifier.state.sessions, isEmpty);
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
    });

    test('loadSessions populates sessions', () async {
      final notifier = SessionNotifier(apiClient: _TestApiClient(_testSessions));
      await notifier.loadSessions();

      expect(notifier.state.sessions, hasLength(_testSessions.length));
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
    });

    test('loadSessions sets error on failure', () async {
      final notifier = SessionNotifier(apiClient: _ThrowingClient());
      await notifier.loadSessions();

      expect(notifier.state.sessions, isEmpty);
      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNotNull);
    });

    test('createSession appends new session', () async {
      final client = _TestApiClient(_testSessions);
      final notifier = SessionNotifier(apiClient: client);
      await notifier.loadSessions();

      final count = notifier.state.sessions.length;
      await notifier.createSession('New Session');
      expect(notifier.state.sessions.length, count + 1);
    });

    test('deleteSession removes session', () async {
      final client = _TestApiClient(_testSessions);
      final notifier = SessionNotifier(apiClient: client);
      await notifier.loadSessions();

      final firstId = _testSessions[0].id;
      await notifier.deleteSession(firstId);
      expect(notifier.state.sessions, hasLength(_testSessions.length - 1));
    });
  });
}

// ===== Test helpers =====

final _testSessions = [
  Session(
    id: '1',
    title: 'Test Session',
    createdAt: DateTime(2025, 1, 1),
    updatedAt: DateTime(2025, 1, 15),
  ),
  Session(
    id: '2',
    title: 'Another Session',
    createdAt: DateTime(2025, 2, 1),
    updatedAt: DateTime(2025, 2, 10),
  ),
];

/// Test ApiClient that returns a predefined list of sessions
class _TestApiClient extends ApiClient {
  final List<Session> _sessions;
  final List<Session> _localSessions;

  _TestApiClient(this._sessions)
      : _localSessions = [],
        super(host: 'localhost', port: 65432);

  @override
  Future<List<Session>> listSessions() async {
    return [..._sessions, ..._localSessions];
  }

  @override
  Future<Session> createSession({required String title, String? agentId}) async {
    return Session(
      id: 'new-${_localSessions.length + 1}',
      title: title,
      createdAt: DateTime.now(),
      updatedAt: DateTime.now(),
    );
  }

  @override
  Future<void> deleteSession(String id) async {
    _localSessions.removeWhere((s) => s.id == id);
  }

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> post<T>(String path,
      {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path,
      {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}

/// Client with a short delay to simulate async load
class _SlowLoadClient extends ApiClient {
  _SlowLoadClient() : super(host: 'localhost', port: 65431);

  @override
  Future<List<Session>> listSessions() async {
    // 50ms delay so loading state is visible
    await Future.delayed(const Duration(milliseconds: 50));
    return [];
  }

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> post<T>(String path,
      {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path,
      {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}

/// Client that throws on listSessions
class _ThrowingClient extends ApiClient {
  _ThrowingClient() : super(host: 'localhost', port: 65433);

  @override
  Future<List<Session>> listSessions() async {
    throw Exception('connection refused');
  }

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> post<T>(String path,
      {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path,
      {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}
