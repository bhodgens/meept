import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/session_history.dart';

void main() {
  // The store is a process-wide singleton, so each test uses a unique session
  // ID to avoid cross-test contamination.
  var counter = 0;
  String sid() => 'session-${counter++}';

  group('SessionHistoryStore', () {
    test('previous returns null when there is no history', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      expect(store.previous(id, 'draft'), isNull);
    });

    test('add then previous recalls the most recent entry', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, 'first');
      store.add(id, 'second');

      expect(store.previous(id, 'current draft'), 'second');
      expect(store.previous(id, 'current draft'), 'first');
    });

    test('previous stashes the draft and next restores it', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, 'one');
      store.add(id, 'two');

      // Walk up: two -> one
      expect(store.previous(id, 'my draft'), 'two');
      expect(store.previous(id, 'my draft'), 'one');
      // At the oldest; further up stays at oldest.
      expect(store.previous(id, 'my draft'), 'one');
      // Walk back down: two, then the stashed draft.
      expect(store.next(id), 'two');
      expect(store.next(id), 'my draft');
      // Past the newest entry: no longer browsing.
      expect(store.next(id), isNull);
    });

    test('next returns null when not browsing', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, 'entry');
      expect(store.next(id), isNull);
    });

    test('ignores empty and whitespace-only input', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, '');
      store.add(id, '   ');
      expect(store.previous(id, 'draft'), isNull);
    });

    test('collapses consecutive duplicates', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, 'same');
      store.add(id, 'same');
      store.add(id, 'same');

      expect(store.previous(id, 'draft'), 'same');
      // No older distinct entry exists.
      expect(store.previous(id, 'draft'), 'same');
    });

    test('trims entries but keeps non-consecutive duplicates', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, 'a');
      store.add(id, 'b');
      store.add(id, 'a'); // non-consecutive duplicate is kept

      expect(store.previous(id, ''), 'a');
      expect(store.previous(id, ''), 'b');
      expect(store.previous(id, ''), 'a');
    });

    test('history is isolated per session', () {
      final store = SessionHistoryStore.instance;
      final a = sid();
      final b = sid();
      store.add(a, 'from-a');
      store.add(b, 'from-b');

      expect(store.previous(a, ''), 'from-a');
      expect(store.previous(b, ''), 'from-b');
    });

    test('resetCursor abandons navigation but keeps entries', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      store.add(id, 'one');
      store.add(id, 'two');

      expect(store.previous(id, 'draft'), 'two');
      store.resetCursor(id);
      // After reset, previous starts from the newest again.
      expect(store.previous(id, 'draft'), 'two');
    });

    test('caps history at the max size', () {
      final store = SessionHistoryStore.instance;
      final id = sid();
      for (var i = 0; i < 150; i++) {
        store.add(id, 'msg-$i');
      }

      // Walk back 100 times (the max size)
      final first = store.previous(id, '');
      expect(first, 'msg-149'); // Most recent

      String? last;
      for (var i = 0; i < 99; i++) {
        last = store.previous(id, '');
      }
      expect(last, 'msg-50'); // Oldest surviving entry

      // Calling previous again returns the same oldest entry (stays at boundary)
      expect(store.previous(id, ''), 'msg-50');
    });
  });
}
