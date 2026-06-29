import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/sessions/sessions_detail.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stub [SdkApiClient] that returns canned [Session] JSON for `getSession`
/// and empty arrays for the related-item endpoints.
///
/// Subclasses override the endpoint methods directly so no network call is
/// made; the base constructor's host/port are unused but required.
class _StubClient extends SdkApiClient {
  final Map<String, Session> sessions;

  _StubClient(this.sessions) : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getSession(String id) async {
    final s = sessions[id];
    if (s == null) throw Exception('session not found: $id');
    return s.toJson();
  }

  @override
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async => [];

  @override
  Future<List<Map<String, dynamic>>> listPlansBySession(String sessionId) =>
      Future.value([]);

  @override
  Future<List<Map<String, dynamic>>> listPlans({
    String? projectId,
    int limit = 50,
  }) =>
      Future.value([]);
}

void main() {
  group('SessionsDetailPane sessionDetailFamily wiring', () {
    testWidgets(
        'when sessionId provided, pane consumes sessionDetailFamily '
        'and replaces fallback once cache resolves', (tester) async {
      final fallback = Session(
        id: 's1',
        title: 'fallback-title',
        createdAt: DateTime(2025, 1, 1),
      );
      final fresh = Session(
        id: 's1',
        title: 'fresh-from-cache',
        createdAt: DateTime(2025, 1, 2),
      );
      final client = _StubClient({'s1': fresh});

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWithValue(client),
          ],
          child: MaterialApp(
            home: Scaffold(
              body: Row(
                children: [
                  SessionsDetailPane(
                    session: fallback,
                    sessionId: 's1',
                  ),
                ],
              ),
            ),
          ),
        ),
      );

      // After the family resolves: the cached title replaces the fallback.
      // We don't assert the intermediate loading state because the stub's
      // async getSession may resolve synchronously before the first frame.
      await tester.pumpAndSettle();
      expect(find.textContaining('fresh-from-cache'), findsWidgets);
      // The fallback title must NOT be visible — proves the family was consumed.
      expect(find.textContaining('fallback-title'), findsNothing);
    });

    testWidgets('without sessionId, pane uses the passed session directly',
        (tester) async {
      final session = Session(
        id: 's2',
        title: 'direct-session',
        createdAt: DateTime(2025, 1, 1),
      );
      final client = _StubClient({});

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWithValue(client),
          ],
          child: MaterialApp(
            home: Scaffold(
              body: Row(
                children: [
                  SessionsDetailPane(session: session),
                ],
              ),
            ),
          ),
        ),
      );

      await tester.pumpAndSettle();
      expect(find.textContaining('direct-session'), findsWidgets);
    });
  });
}
