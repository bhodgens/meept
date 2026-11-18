/// Widget tests for session rename from the session info popup header, plus
/// the shared rename dialog used by both the popup and the sidebar row.
///
/// Both rename entry points must submit through SessionNotifier.renameSession
/// (PATCH /api/v1/sessions/{id}). The sidebar row shares the exact same dialog
/// and notifier call, so the popup path plus the dialog tests cover the common
/// contract.
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/dialogs/rename_session_dialog.dart';
import 'package:meept_ui/features/home/session_info_overlay.dart';
import 'package:meept_ui/models/api_models.dart' show Session;
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// SDK stub recording rename calls and returning one session in the list so
/// the real SessionNotifier has state to update.
class _RenameSdkClient extends SdkApiClient {
  _RenameSdkClient() : super(host: 'localhost', port: 8081);

  final List<String> renamedIds = [];
  final List<String> renamedTitles = [];

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async => [
    {
      'id': 'session-rename-test',
      'name': 'bob-work-1',
      'created_at': '2026-01-01T00:00:00Z',
    },
  ];

  @override
  Future<Map<String, dynamic>> updateSessionDescription(
    String sessionId,
    String description,
  ) async {
    renamedIds.add(sessionId);
    renamedTitles.add(description);
    return {'id': sessionId, 'description': description};
  }
}

Widget _wrap(Widget child, SdkApiClient client) {
  return ProviderScope(
    overrides: [sdkClientProvider.overrideWithValue(client)],
    child: MaterialApp(home: Scaffold(body: child)),
  );
}

void main() {
  testWidgets('shared rename dialog pre-fills and trims the name', (
    tester,
  ) async {
    String? result;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () async {
                result = await showRenameSessionDialog(
                  context,
                  currentTitle: 'bob-work-1',
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    expect(find.byType(RenameSessionDialog), findsOneWidget);
    expect(find.text('bob-work-1'), findsWidgets);

    await tester.enterText(find.byType(TextField), '  my project  ');
    await tester.tap(find.text('rename'));
    await tester.pumpAndSettle();

    expect(result, 'my project');
  });

  testWidgets('rename dialog cancel returns null', (tester) async {
    String? result = 'unset';
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () async {
                result = await showRenameSessionDialog(
                  context,
                  currentTitle: 'old',
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('cancel'));
    await tester.pumpAndSettle();
    expect(result, isNull);
  });

  testWidgets('popup header edit icon renames the session', (tester) async {
    final client = _RenameSdkClient();
    await tester.pumpWidget(
      _wrap(
        SessionInfoOverlay(
          session: Session(
            id: 'session-rename-test',
            title: 'bob-work-1',
            createdAt: DateTime(2026, 1, 1),
          ),
        ),
        client,
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.edit_outlined), findsOneWidget);
    await tester.tap(find.byIcon(Icons.edit_outlined));
    await tester.pumpAndSettle();
    expect(find.byType(RenameSessionDialog), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'new name');
    await tester.tap(find.text('rename'));
    await tester.pumpAndSettle();

    expect(client.renamedIds, ['session-rename-test']);
    expect(client.renamedTitles, ['new name']);
    // Drain the transient status-message timer created by showStatusMessage.
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
  });

  testWidgets('popup header double-click on name renames the session', (
    tester,
  ) async {
    final client = _RenameSdkClient();
    await tester.pumpWidget(
      _wrap(
        SessionInfoOverlay(
          session: Session(
            id: 'session-rename-test',
            title: 'bob-work-1',
            createdAt: DateTime(2026, 1, 1),
          ),
        ),
        client,
      ),
    );
    await tester.pumpAndSettle();

    final center = tester.getCenter(find.textContaining('session: bob-work-1'));
    await tester.tapAt(center);
    await tester.pump(const Duration(milliseconds: 50));
    await tester.tapAt(center);
    await tester.pumpAndSettle();
    expect(find.byType(RenameSessionDialog), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'double click rename');
    await tester.tap(find.text('rename'));
    await tester.pumpAndSettle();

    expect(client.renamedTitles, ['double click rename']);
    // Drain the transient status-message timer created by showStatusMessage.
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
  });
}
