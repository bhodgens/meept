import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/changes/changes_panel.dart';
import 'package:meept_ui/models/api_models.dart' show Session;
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stub SdkApiClient that overrides the changes-review methods.
class _ChangesStubClient extends SdkApiClient {
  _ChangesStubClient({
    List<Map<String, dynamic>>? pending,
    List<Map<String, dynamic>>? journal,
  })  : _pending = pending ?? [],
        _journal = journal ?? [],
        super(host: 'localhost', port: 8081);

  final List<Map<String, dynamic>> _pending;
  final List<Map<String, dynamic>> _journal;

  final acceptedIds = <String>[];
  final rejectedIds = <String>[];
  final revertedIds = <String>[];
  String? listPendingSessionId;
  String? listJournalSessionId;
  int? listJournalLimit;

  /// When set, the matching mutation throws this exception (simulating a
  /// 409 drift / 400 size-cap response from the daemon).
  SdkApiException? acceptError;
  SdkApiException? revertError;

  @override
  Future<List<Map<String, dynamic>>> listPendingChanges(
    String sessionId,
  ) async {
    listPendingSessionId = sessionId;
    return List<Map<String, dynamic>>.from(
      _pending.map((p) => Map<String, dynamic>.from(p)),
    );
  }

  @override
  Future<Map<String, dynamic>> acceptPendingChange(String id) async {
    final err = acceptError;
    if (err != null) throw err;
    acceptedIds.add(id);
    _pending.removeWhere((p) => p['id'] == id);
    return {'status': 'applied'};
  }

  @override
  Future<Map<String, dynamic>> rejectPendingChange(String id) async {
    rejectedIds.add(id);
    _pending.removeWhere((p) => p['id'] == id);
    return {'status': 'rejected'};
  }

  @override
  Future<List<Map<String, dynamic>>> listChangesJournal({
    String? sessionId,
    int? limit,
  }) async {
    listJournalSessionId = sessionId;
    listJournalLimit = limit;
    return List<Map<String, dynamic>>.from(
      _journal.map((j) => Map<String, dynamic>.from(j)),
    );
  }

  @override
  Future<Map<String, dynamic>> revertJournalEntry(String id) async {
    final err = revertError;
    if (err != null) throw err;
    revertedIds.add(id);
    return {'restored_path': '/tmp/restored.dart'};
  }
}

Map<String, dynamic> _pendingChange({
  String id = 'chg-1',
  String filePath = 'src/main.go',
  String diff = '@@ -1,3 +1,4 @@\n+fmt.Println("hello")',
}) {
  return {
    'id': id,
    'file_path': filePath,
    'diff': diff,
    'created_at': '2026-08-25T10:00:00Z',
    'expires_at': '2026-08-25T10:30:00Z',
  };
}

Map<String, dynamic> _journalEntry({
  String id = 'j-1',
  String filePath = 'src/main.go',
}) {
  return {
    'id': id,
    'session_id': 'session-1',
    'file_path': filePath,
    'post_sha': 'deadbeefcafe',
    'applied_at': '2026-08-25T10:05:00Z',
    'change_ids': ['chg-1'],
    'pre_image_size': 2048,
  };
}

final Session _session = Session(
  id: 'session-1',
  title: 'test session',
  createdAt: DateTime.utc(2026, 8, 25),
);

Widget _buildTestApp(_ChangesStubClient client, {Session? session}) {
  return ProviderScope(
    overrides: [
      sdkClientProvider.overrideWith((_) => client),
      if (session != null)
        activeSessionProvider.overrideWith((_) => session),
    ],
    child: const MaterialApp(
      home: Scaffold(body: ChangesPanel()),
    ),
  );
}

void main() {
  testWidgets('shows no-session placeholder when no active session', (
    tester,
  ) async {
    final client = _ChangesStubClient();
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(
      find.text('select a session to review pending changes'),
      findsOneWidget,
    );
    // No session means no pending listing was issued.
    expect(client.listPendingSessionId, isNull);
  });

  testWidgets('renders pending change cards with file path and diff', (
    tester,
  ) async {
    final client = _ChangesStubClient(pending: [_pendingChange()]);
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.listPendingSessionId, 'session-1');
    // File path is rendered lowercase (repo-wide convention).
    expect(find.text('src/main.go'), findsOneWidget);
    // Plain mono diff body is rendered.
    expect(find.textContaining('fmt.Println'), findsOneWidget);
    expect(find.text('accept'), findsOneWidget);
    expect(find.text('reject'), findsOneWidget);
  });

  testWidgets('renders empty state when no pending changes', (tester) async {
    final client = _ChangesStubClient(pending: []);
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('no pending changes'), findsOneWidget);
  });

  testWidgets('accept button calls acceptPendingChange with change id', (
    tester,
  ) async {
    final client = _ChangesStubClient(pending: [_pendingChange(id: 'chg-9')]);
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    await tester.tap(find.text('accept'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.acceptedIds, ['chg-9']);
    // Card disappears after refresh.
    expect(find.text('src/main.go'), findsNothing);
  });

  testWidgets('reject button calls rejectPendingChange with change id', (
    tester,
  ) async {
    final client = _ChangesStubClient(pending: [_pendingChange(id: 'chg-7')]);
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    await tester.tap(find.text('reject'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.rejectedIds, ['chg-7']);
    expect(client.acceptedIds, isEmpty);
  });

  testWidgets('accept 409 drift error shows daemon error message', (
    tester,
  ) async {
    final client = _ChangesStubClient(pending: [_pendingChange()])
      ..acceptError = SdkApiException(
        message: 'drift detected: file modified since staging',
        statusCode: 409,
      );
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    await tester.tap(find.text('accept'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.acceptedIds, isEmpty);
    expect(
      find.textContaining('drift detected: file modified since staging'),
      findsOneWidget,
    );
  });

  testWidgets('journal tab renders entries and revert calls service', (
    tester,
  ) async {
    final client = _ChangesStubClient(journal: [_journalEntry(id: 'j-42')]);
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Journal was scoped to the active session.
    expect(client.listJournalSessionId, 'session-1');

    // Switch to the journal tab and let the tab animation complete.
    await tester.tap(find.text('journal'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.text('src/main.go'), findsOneWidget);
    expect(find.text('revert'), findsOneWidget);
    // Metadata rendered (short sha + pre-image size).
    expect(find.textContaining('deadbeef'), findsOneWidget);
    expect(find.textContaining('2.0kb pre-image'), findsOneWidget);

    await tester.tap(find.text('revert'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.revertedIds, ['j-42']);
  });

  testWidgets('revert 400 size-cap error surfaces daemon message', (
    tester,
  ) async {
    final client = _ChangesStubClient(journal: [_journalEntry()])
      ..revertError = SdkApiException(
        message: 'pre-image exceeds size cap',
        statusCode: 400,
      );
    await tester.pumpWidget(_buildTestApp(client, session: _session));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    await tester.tap(find.text('journal'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.text('revert'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.revertedIds, isEmpty);
    expect(
      find.textContaining('pre-image exceeds size cap'),
      findsOneWidget,
    );
  });
}
