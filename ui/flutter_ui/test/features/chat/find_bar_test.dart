import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/chat/find_bar.dart';
import 'package:meept_ui/features/chat/find_state.dart';

void main() {
  ProviderContainer makeContainer() {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    return c;
  }

  testWidgets('FindBar renders query field, count, toggles, close',
      (tester) async {
    final container = makeContainer();
    final sessionId = 's1';
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          home: Scaffold(
            body: FindBar(
              sessionId: sessionId,
              matchCount: 3,
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    // Hint text visible.
    expect(find.text('find...'), findsOneWidget);
    // Toggle labels visible.
    expect(find.text('Aa'), findsOneWidget);
    expect(find.text('.*'), findsOneWidget);
    // Initial count: 1/3 (cursor 0).
    expect(find.text('1/3'), findsOneWidget);
    // Close icon.
    expect(find.byIcon(Icons.close), findsOneWidget);
  });

  testWidgets('Typing updates query provider', (tester) async {
    final container = makeContainer();
    const sessionId = 's2';
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          home: Scaffold(
            body: FindBar(sessionId: sessionId, matchCount: 0),
          ),
        ),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byType(TextField), 'hello');
    await tester.pump();

    expect(container.read(findQueryProvider(sessionId)), 'hello');
  });

  testWidgets('Tapping case toggle flips case-sensitive provider',
      (tester) async {
    final container = makeContainer();
    const sessionId = 's3';
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          home: Scaffold(
            body: FindBar(sessionId: sessionId, matchCount: 0),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(container.read(findCaseSensitiveProvider(sessionId)), isFalse);
    await tester.tap(find.text('Aa'));
    await tester.pump();
    expect(container.read(findCaseSensitiveProvider(sessionId)), isTrue);
  });

  testWidgets('Tapping regex toggle flips regex provider', (tester) async {
    final container = makeContainer();
    const sessionId = 's4';
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          home: Scaffold(
            body: FindBar(sessionId: sessionId, matchCount: 0),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(container.read(findRegexProvider(sessionId)), isFalse);
    await tester.tap(find.text('.*'));
    await tester.pump();
    expect(container.read(findRegexProvider(sessionId)), isTrue);
  });

  testWidgets('Close button clears visibility, query, cursor', (tester) async {
    final container = makeContainer();
    const sessionId = 's5';
    // Seed providers.
    container.read(findBarVisibleProvider(sessionId).notifier).state = true;
    container.read(findQueryProvider(sessionId).notifier).state = 'hi';
    container.read(findCursorProvider(sessionId).notifier).state = 2;

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          home: Scaffold(
            body: FindBar(sessionId: sessionId, matchCount: 5),
          ),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byIcon(Icons.close));
    await tester.pump();

    expect(container.read(findBarVisibleProvider(sessionId)), isFalse);
    expect(container.read(findQueryProvider(sessionId)), '');
    expect(container.read(findCursorProvider(sessionId)), 0);
  });

  group('computeFindMatches', () {
    test('empty query returns empty', () {
      final r = computeFindMatches(
        contents: const ['hello'],
        query: '',
        caseSensitive: false,
        regex: false,
      );
      expect(r.matches, isEmpty);
      expect(r.regexError, isNull);
    });

    test('case-insensitive substring matches', () {
      final r = computeFindMatches(
        contents: const ['hello world', 'HELLO again', 'bye'],
        query: 'hello',
        caseSensitive: false,
        regex: false,
      );
      expect(r.matches.length, 2);
      expect(r.matches[0].messageIndex, 0);
      expect(r.matches[1].messageIndex, 1);
    });

    test('case-sensitive restricts matches', () {
      final r = computeFindMatches(
        contents: const ['hello world', 'HELLO again'],
        query: 'hello',
        caseSensitive: true,
        regex: false,
      );
      expect(r.matches.length, 1);
      expect(r.matches[0].messageIndex, 0);
    });

    test('regex matches across content', () {
      final r = computeFindMatches(
        contents: const ['cat', 'cot', 'cut'],
        query: 'c[au]t',
        caseSensitive: false,
        regex: true,
      );
      expect(r.matches.length, 2);
      expect(r.matches[0].messageIndex, 0);
      expect(r.matches[1].messageIndex, 2);
    });

    test('invalid regex returns regexError and no matches', () {
      final r = computeFindMatches(
        contents: const ['x'],
        query: '(unclosed',
        caseSensitive: false,
        regex: true,
      );
      expect(r.matches, isEmpty);
      expect(r.regexError, isNotNull);
    });

    test('respects maxMatches cap', () {
      final r = computeFindMatches(
        contents: List.filled(1000, 'a'),
        query: 'a',
        caseSensitive: false,
        regex: false,
        maxMatches: 5,
      );
      expect(r.matches.length, 5);
    });
  });
}
