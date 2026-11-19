import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';

/// H5 display-precedence pins: description (the user/summary field, written
/// by rename and by the summarizer/refresher) WINS over the auto-generated
/// single-word name. TUI parity: internal/tui/models/sessions.go renders
/// Description first, Name only when Description is empty. The old
/// name-first normalizer made every user rename revert on reload.
void main() {
  group('Session title normalizer (_normaliseSessionJson)', () {
    test('prefers description over a meaningful LLM-generated name', () {
      final json = {
        'id': 'test-1',
        'name': 'debugging',
        'description': 'coding: fixed null pointer in auth',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      // H5: the summary/user field is the display title, not the auto label.
      expect(normalized.title, 'coding: fixed null pointer in auth');
    });

    test('falls back to description when name is "default"', () {
      final json = {
        'id': 'test-2',
        'name': 'default',
        'description': 'fixing authentication bug',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'fixing authentication bug');
    });

    test('falls back to description when name is "Untitled"', () {
      final json = {
        'id': 'test-3',
        'name': 'Untitled',
        'description': 'research on quantum computing',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'research on quantum computing');
    });

    test('falls back to description when name is "chat"', () {
      final json = {
        'id': 'test-4',
        'name': 'chat',
        'description': 'task: plan sprint roadmap',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'task: plan sprint roadmap');
    });

    test('falls back to description when name is empty', () {
      final json = {
        'id': 'test-5',
        'name': '',
        'description': 'personal: health question',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'personal: health question');
    });

    test('uses name when description is missing', () {
      final json = {
        'id': 'test-6',
        'name': 'research',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'research');
    });

    test('uses "Untitled" when both name and description are missing', () {
      final json = {'id': 'test-7', 'created_at': '2025-01-01T00:00:00Z'};
      final normalized = Session.fromJson(json);
      expect(normalized.title, 'Untitled');
    });

    test('uses description when name is generic and description exists', () {
      final json = {
        'id': 'test-8',
        'name': 'default',
        'description': 'creative: write short story',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'creative: write short story');
    });

    test('prefers description even when the name is longer', () {
      final json = {
        'id': 'test-9',
        'name': 'refactoring',
        'description': 'cleanup',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      // H5: precedence is field-based, not length-based — description wins.
      expect(normalized.title, 'cleanup');
    });

    test('handles "title" field as alternative to "name"', () {
      final json = {
        'id': 'test-10',
        'title': 'backup-plan', // Using 'title' instead of 'name'
        'description': 'system administration',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'system administration');
    });

    test('generic title field falls back to description', () {
      final json = {
        'id': 'test-11',
        'title': 'chat',
        'description': 'coding: add unit tests',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'coding: add unit tests');
    });

    test('empty description keeps the name (generic-name fallback)', () {
      final json = {
        'id': 'test-12',
        'name': 'debugging',
        'description': '',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'debugging');
    });

    test('whitespace-only description keeps the name', () {
      final json = {
        'id': 'test-13',
        'name': 'debugging',
        'description': '   ',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'debugging');
    });

    test('empty name and empty description land on Untitled', () {
      final json = {
        'id': 'test-14',
        'name': '',
        'description': '',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);
      expect(normalized.title, 'Untitled');
    });
  });

  group('Session display in UI regression tests', () {
    test('direct construction keeps the given title (no re-normalisation)', () {
      // copyWith paths (rename, live updates) construct titles directly;
      // only fromJson applies the display precedence.
      final session = Session(
        id: 'regression-1',
        title: 'debugging',
        description: 'coding: fixed null pointer',
        createdAt: DateTime(2025, 1, 1),
      );

      expect(session.title, 'debugging');
      expect(session.description, 'coding: fixed null pointer');
    });

    test('status bar falls back to description for generic names', () {
      // Simulate what comes from backend after normalizer
      final json = {
        'id': 'regression-2',
        'name': 'default', // Generic fallback from failed LLM call
        'description': 'exploring codebase structure',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final session = Session.fromJson(json);
      expect(session.title, 'exploring codebase structure');
    });

    test('sessions list displays summary titles after WebSocket update', () {
      // Initial session with generic name
      final initialJson = {
        'id': 'live-update-1',
        'name': 'default',
        'description': 'initial conversation',
        'created_at': '2025-01-01T00:00:00Z',
      };

      var session = Session.fromJson(initialJson);
      expect(session.title, 'initial conversation');

      // Simulate WebSocket event: session.title_updated with a refreshed
      // LLM name AND summary. H5: the summary is the display title.
      final updatedJson = {
        'id': 'live-update-1',
        'name': 'debugging',
        'description': 'coding: fixed websocket bug',
        'created_at': '2025-01-01T00:00:00Z',
      };

      session = Session.fromJson(updatedJson);
      expect(session.title, 'coding: fixed websocket bug');
    });

    test('reload-persistence: rename survives reparse of backend JSON', () {
      // H5 core pin: renameSession PATCHes description only — the backend
      // keeps the old auto-generated name. Reparsing the backend row must
      // keep the user's rename as the display title.
      const backendRow = {
        'id': 'rename-persist-1',
        'name': 'debugging', // stale auto label the daemon still carries
        'created_at': '2025-01-01T00:00:00Z',
      };

      // Before rename: no description -> the auto name is the title.
      final before = Session.fromJson(Map<String, dynamic>.from(backendRow));
      expect(before.title, 'debugging');

      // User renames -> daemon stores it in description (PATCH body).
      final afterRename = {...backendRow, 'description': 'my renamed session'};

      // Reload: reparse the backend JSON. The rename must NOT revert.
      final reloaded = Session.fromJson(afterRename);
      expect(reloaded.title, 'my renamed session');
      expect(reloaded.description, 'my renamed session');

      // A second reload (e.g. after the periodic refresher rewrote the
      // auto name again) still prefers the description.
      final refreshed = {
        ...afterRename,
        'name': 'planning', // refresher regenerated the label
      };
      expect(Session.fromJson(refreshed).title, 'my renamed session');
    });
  });
}
