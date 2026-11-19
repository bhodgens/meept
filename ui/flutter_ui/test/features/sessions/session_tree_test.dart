import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/sessions/session_tree.dart';
import 'package:meept_ui/features/sessions/sessions_list.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/session_notifier.dart';

import '../../mocks/mock_websocket_service.dart';

void main() {
  group('C2 bucketFor', () {
    test('returns projectId when non-empty', () {
      final session = Session(
        id: '1',
        title: 'a',
        createdAt: DateTime.utc(2026, 1, 1),
        projectId: 'proj-123',
        projectPath: '/tmp/other',
      );
      expect(bucketFor(session), 'proj-123');
    });

    test('falls back to projectPath when projectId is empty', () {
      final session = Session(
        id: '2',
        title: 'b',
        createdAt: DateTime.utc(2026, 1, 1),
        projectId: '',
        projectPath: '/tmp/my-project',
      );
      expect(bucketFor(session), '/tmp/my-project');
    });

    test('returns unbound when both are empty', () {
      final session = Session(
        id: '3',
        title: 'c',
        createdAt: DateTime.utc(2026, 1, 1),
      );
      expect(bucketFor(session), 'unbound');
    });

    test('returns unbound when both are null', () {
      final session = Session(
        id: '4',
        title: 'd',
        createdAt: DateTime.utc(2026, 1, 1),
        projectId: null,
        projectPath: null,
      );
      expect(bucketFor(session), unboundBucket);
    });

    test('groupSessionsByBucket preserves insertion order', () {
      final sessions = [
        Session(
          id: 'a1',
          title: 'a1',
          createdAt: DateTime.utc(2026, 1, 1),
          projectId: 'proj-a',
        ),
        Session(
          id: 'b1',
          title: 'b1',
          createdAt: DateTime.utc(2026, 1, 1),
          projectId: 'proj-b',
        ),
        Session(
          id: 'a2',
          title: 'a2',
          createdAt: DateTime.utc(2026, 1, 1),
          projectId: 'proj-a',
        ),
      ];
      final grouped = groupSessionsByBucket(sessions);
      expect(grouped.keys.toList(), ['proj-a', 'proj-b']);
      expect(grouped['proj-a']!.map((s) => s.id).toList(), ['a1', 'a2']);
      expect(grouped['proj-b']!.map((s) => s.id).toList(), ['b1']);
    });
  });

  group('sessions tree UI', () {
    testWidgets('groups render as expandable project buckets', (tester) async {
      final bound = Session(
        id: 'bound-1',
        title: 'Bound Session',
        createdAt: DateTime.utc(2026, 1, 1),
        projectId: 'proj-a',
      );
      final free = Session(
        id: 'free-1',
        title: 'Free Session',
        createdAt: DateTime.utc(2026, 1, 1),
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith(
              (ref) => SessionNotifier(
                sdkClient: _TreeTestSdkClient([bound, free]),
                websocket: MockWebSocketService(),
              ),
            ),
          ],
          child: const MaterialApp(
            home: Scaffold(body: SizedBox(height: 600, child: SessionsList())),
          ),
        ),
      );
      // initState post-frame callback loads sessions via the stub.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));
      await tester.pump(const Duration(milliseconds: 50));

      // Two project headers, one per C2 bucket.
      expect(
        find.byKey(const ValueKey('session-bucket-proj-a')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('session-bucket-unbound')),
        findsOneWidget,
      );
      // Children start expanded and are visible.
      expect(find.text('bound session'), findsOneWidget);
      expect(find.text('free session'), findsOneWidget);

      // Collapse the proj-a group via its header; its child hides, the other
      // bucket's child stays visible.
      await tester.tap(
        find.byKey(const ValueKey('session-bucket-label-proj-a')),
      );
      await tester.pump();
      await tester.pumpAndSettle();

      expect(
        find.text('bound session'),
        findsNothing,
        reason: 'collapsing proj-a hides its session tile',
      );
      expect(
        find.text('free session'),
        findsOneWidget,
        reason: 'collapsing one group must not affect another',
      );

      // Re-expand restores the child.
      await tester.tap(
        find.byKey(const ValueKey('session-bucket-label-proj-a')),
      );
      await tester.pumpAndSettle();
      expect(find.text('bound session'), findsOneWidget);
    });
  });
}

/// Stub [SdkApiClient] returning only the seeded sessions, mirroring the real
/// `listSessions({int? limit})` signature so the stub cannot silently drift.
class _TreeTestSdkClient extends SdkApiClient {
  _TreeTestSdkClient(this._sessions) : super(host: 'localhost', port: 65432);

  final List<Session> _sessions;

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async =>
      _sessions.map((s) => s.toJson()).toList();
}
