import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/sessions/sessions_list.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/status_message_provider.dart';
import 'package:meept_ui/services/session_notifier.dart';
import 'package:meept_ui/services/sdk_client.dart';
import '../../mocks/mock_websocket_service.dart';

void main() {
  testWidgets(
    'archived session renders with reduced opacity and sorts after active',
    (tester) async {
      final archived = Session(
        id: 'arc1',
        title: 'arc me',
        createdAt: DateTime.now(),
        lastActivity: DateTime.now(),
        archived: true,
      );
      final active = Session(
        id: 'act1',
        title: 'active one',
        createdAt: DateTime.now(),
        lastActivity: DateTime.now(),
      );

      final client = _ArchiveTestClient([active, archived]);
      final websocket = MockWebSocketService();

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith(
              (ref) => SessionNotifier(sdkClient: client, websocket: websocket),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: SessionsList())),
        ),
      );
      await tester.pumpAndSettle();

      final archivedTile = find.byKey(const ValueKey('session-tile-arc1'));
      expect(archivedTile, findsOneWidget);

      final activeTile = find.byKey(const ValueKey('session-tile-act1'));
      expect(activeTile, findsOneWidget);

      expect(
        tester.getCenter(activeTile).dy,
        lessThan(tester.getCenter(archivedTile).dy),
      );

      final opacityFinder = find.ancestor(
        of: archivedTile,
        matching: find.byType(Opacity),
      );
      expect(opacityFinder, findsOneWidget);
      final opacity = tester.widget<Opacity>(opacityFinder).opacity;
      expect(opacity, lessThan(1.0));
    },
  );

  testWidgets('archive icon tap shows archive confirmation dialog', (
    tester,
  ) async {
    final session = Session(
      id: 'x1',
      title: 'target',
      createdAt: DateTime.now(),
    );
    final client = _ArchiveTestClient([session]);
    final websocket = MockWebSocketService();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionProvider.overrideWith(
            (ref) => SessionNotifier(sdkClient: client, websocket: websocket),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: SessionsList())),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.archive_outlined), findsOneWidget);
    await tester.tap(find.byIcon(Icons.archive_outlined));
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();

    expect(find.text('archive session?'), findsOneWidget);
  });

  testWidgets('archived row shows an unarchive icon; tapping calls '
      'unarchiveSession', (tester) async {
    final archived = Session(
      id: 'arc1',
      title: 'arc me',
      createdAt: DateTime.now(),
      archived: true,
    );
    final client = _ArchiveTestClient([archived]);
    final websocket = MockWebSocketService();
    final container = ProviderContainer(
      overrides: [
        sessionProvider.overrideWith(
          (ref) => SessionNotifier(sdkClient: client, websocket: websocket),
        ),
      ],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: Scaffold(body: SessionsList())),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.unarchive), findsOneWidget);
    expect(find.byIcon(Icons.archive_outlined), findsNothing);

    // InkWell delays onTap when onDoubleTap is registered; pump past the
    // double-tap window (same pattern as the archive-icon test above).
    await tester.tap(find.byIcon(Icons.unarchive));
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();

    expect(client.archiveCalls, ['arc1:false']);
    expect(container.read(statusMessageProvider), 'unarchived: arc me');
    // Flush the 2500ms status-message auto-clear timer so the test binding
    // does not flag a pending timer at teardown.
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('context menu offers rename; submitting renames via the '
      'description endpoint', (tester) async {
    final session = Session(
      id: 's1',
      title: 'old title',
      createdAt: DateTime.now(),
    );
    final client = _ArchiveTestClient([session]);
    final websocket = MockWebSocketService();
    final container = ProviderContainer(
      overrides: [
        sessionProvider.overrideWith(
          (ref) => SessionNotifier(sdkClient: client, websocket: websocket),
        ),
      ],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: Scaffold(body: SessionsList())),
      ),
    );
    await tester.pumpAndSettle();

    await tester.longPress(find.byKey(const ValueKey('session-tile-s1')));
    await tester.pumpAndSettle();
    expect(find.text('rename'), findsOneWidget);
    expect(find.text('archive'), findsOneWidget);

    await tester.tap(find.text('rename'));
    await tester.pumpAndSettle();

    expect(find.text('rename session'), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'fresh name');
    await tester.tap(find.text('rename').last);
    await tester.pumpAndSettle();

    expect(client.descriptionUpdates['s1'], 'fresh name');
    expect(container.read(statusMessageProvider), 'renamed: fresh name');
    // Flush the 2500ms status-message auto-clear timer.
    await tester.pump(const Duration(seconds: 3));
  });
}

class _ArchiveTestClient extends SdkApiClient {
  final List<Session> sessions;
  final List<String> archiveCalls = [];
  final Map<String, String> descriptionUpdates = {};
  _ArchiveTestClient(this.sessions) : super(host: 'localhost', port: 65440);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    return sessions.map((s) => s.toJson()).toList();
  }

  @override
  Future<void> archiveSession(
    String sessionId, {
    required bool archived,
  }) async {
    archiveCalls.add('$sessionId:$archived');
    // Mirror the daemon: flip the flag in the fixture list.
    final idx = sessions.indexWhere((s) => s.id == sessionId);
    if (idx >= 0) {
      sessions[idx] = sessions[idx].copyWith(archived: archived);
    }
  }

  @override
  Future<Map<String, dynamic>> updateSessionDescription(
    String sessionId,
    String description,
  ) async {
    descriptionUpdates[sessionId] = description;
    final idx = sessions.indexWhere((s) => s.id == sessionId);
    if (idx >= 0) {
      sessions[idx] = sessions[idx].copyWith(
        title: description,
        description: description,
      );
    }
    return {};
  }
}
