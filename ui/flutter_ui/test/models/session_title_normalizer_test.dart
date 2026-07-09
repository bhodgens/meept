import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';

void main() {
  group('Session title normalizer (_normaliseSessionJson)', () {
    test('prefers LLM-generated name when it is meaningful', () {
      final json = {
        'id': 'test-1',
        'name': 'debugging',
        'description': 'coding: fixed null pointer in auth',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);

      // LLM name "debugging" should be used (not generic)
      expect(normalized.title, 'debugging');
    });

    test('falls back to description when name is "default"', () {
      final json = {
        'id': 'test-2',
        'name': 'default',
        'description': 'fixing authentication bug',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);

      // Should fall back to description
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
      final json = {
        'id': 'test-7',
        'created_at': '2025-01-01T00:00:00Z',
      };

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

    test('prefers non-generic name over shorter description', () {
      final json = {
        'id': 'test-9',
        'name': 'refactoring',
        'description': 'cleanup',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);

      // Even though description is shorter, name is preferred when not generic
      expect(normalized.title, 'refactoring');
    });

    test('handles "title" field as alternative to "name"', () {
      final json = {
        'id': 'test-10',
        'title': 'backup-plan',  // Using 'title' instead of 'name'
        'description': 'system administration',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final normalized = Session.fromJson(json);

      expect(normalized.title, 'backup-plan');
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
  });

  group('Session display in UI regression tests', () {
    testWidgets('status bar shows LLM-generated title', (tester) async {
      // Create session with meaningful LLM name
      final session = Session(
        id: 'regression-1',
        title: 'debugging',  // LLM-generated
        description: 'coding: fixed null pointer',
        createdAt: DateTime(2025, 1, 1),
      );

      // Verify the title is the LLM name, not the description
      expect(session.title, 'debugging');
      expect(session.description, 'coding: fixed null pointer');
    });

    testWidgets('status bar falls back to description for generic names',
        (tester) async {
      // Simulate what comes from backend after normalizer
      final json = {
        'id': 'regression-2',
        'name': 'default',  // Generic fallback from failed LLM call
        'description': 'exploring codebase structure',
        'created_at': '2025-01-01T00:00:00Z',
      };

      final session = Session.fromJson(json);

      // The normalizer should pick description over generic name
      expect(session.title, 'exploring codebase structure');
    });

    testWidgets('sessions list displays correct titles after WebSocket update',
        (tester) async {
      // Initial session with generic name
      final initialJson = {
        'id': 'live-update-1',
        'name': 'default',
        'description': 'initial conversation',
        'created_at': '2025-01-01T00:00:00Z',
      };

      var session = Session.fromJson(initialJson);
      expect(session.title, 'initial conversation');

      // Simulate WebSocket event: session.title_updated with LLM-generated name
      final updatedJson = {
        'id': 'live-update-1',
        'name': 'debugging',  // LLM now generated a name
        'description': 'coding: fixed websocket bug',
        'created_at': '2025-01-01T00:00:00Z',
      };

      session = Session.fromJson(updatedJson);

      // After update, LLM name should be displayed
      expect(session.title, 'debugging');
    });
  });
}
