import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/sessions/sessions_list.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/session_notifier.dart';
import 'package:meept_ui/services/sdk_client.dart';

void main() {
  testWidgets('archived session renders with reduced opacity and sorts after active',
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

    // Return sessions in the order the backend would (active before archived,
    // matching the server's ORDER BY clause). loadSessions() does not re-sort
    // locally — it trusts the server ordering.
    final client = _ArchiveTestClient([active, archived]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionProvider.overrideWith(
              (ref) => SessionNotifier(sdkClient: client)),
        ],
        child: const MaterialApp(
          home: Scaffold(body: SessionsList()),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final archivedTile = find.byKey(const ValueKey('session-tile-arc1'));
    expect(archivedTile, findsOneWidget);

    final activeTile = find.byKey(const ValueKey('session-tile-act1'));
    expect(activeTile, findsOneWidget);

    // Active session should render above archived (lower Y coordinate).
    expect(
      tester.getCenter(activeTile).dy,
      lessThan(tester.getCenter(archivedTile).dy),
    );

    // Archived tile should be wrapped in an Opacity widget < 1.0.
    // Opacity is the parent of the InkWell that carries the ValueKey, so
    // search ancestors rather than descendants.
    final opacityFinder = find.ancestor(
      of: archivedTile,
      matching: find.byType(Opacity),
    );
    expect(opacityFinder, findsOneWidget);
    final opacity = tester.widget<Opacity>(opacityFinder).opacity;
    expect(opacity, lessThan(1.0));
  });

  testWidgets('archive icon tap shows archive confirmation dialog',
      (tester) async {
    final session = Session(
      id: 'x1',
      title: 'target',
      createdAt: DateTime.now(),
    );
    final client = _ArchiveTestClient([session]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionProvider.overrideWith(
              (ref) => SessionNotifier(sdkClient: client)),
        ],
        child: const MaterialApp(
          home: Scaffold(body: SessionsList()),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.archive_outlined), findsOneWidget);
    await tester.tap(find.byIcon(Icons.archive_outlined));
    // InkWell delays onTap when onDoubleTap is present; pump past the double-tap window
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();

    expect(find.text('archive session?'), findsOneWidget);
  });
}

class _ArchiveTestClient extends SdkApiClient {
  final List<Session> sessions;
  _ArchiveTestClient(this.sessions) : super(host: 'localhost', port: 65440);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    return sessions.map((s) => s.toJson()).toList();
  }

  @override
  Future<void> archiveSession(String sessionId, {required bool archived}) async {
    // No-op — notifier flips the flag locally.
  }
}
