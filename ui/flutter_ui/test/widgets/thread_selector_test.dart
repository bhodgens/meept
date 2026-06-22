import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/widgets/thread_selector.dart';
import 'package:meept_ui/services/thread_service.dart';

class _FakeThreadService implements ThreadService {
  List<Thread> threadsToReturn;
  _FakeThreadService(this.threadsToReturn);

  @override
  Future<List<Thread>> listThreads(String sessionId) async => threadsToReturn;

  @override
  Future<Thread?> createThread(
    String sessionId, {
    String topicLabel = 'general',
    String? conversationId,
  }) async {
    final t = Thread(
      id: 'new-${topicLabel.hashCode}',
      sessionId: sessionId,
      topicLabel: topicLabel,
      conversationId: conversationId ?? '$sessionId-$topicLabel',
      isActive: true,
      createdAt: DateTime.now(),
      lastActivityAt: DateTime.now(),
    );
    threadsToReturn = [...threadsToReturn, t];
    return t;
  }

  @override
  Future<Thread?> setActiveThread(
    String sessionId,
    String threadId,
  ) async {
    for (final t in threadsToReturn) {
      if (t.id == threadId) {
        threadsToReturn = threadsToReturn
            .map((x) => Thread(
                  id: x.id,
                  sessionId: x.sessionId,
                  topicLabel: x.topicLabel,
                  conversationId: x.conversationId,
                  isActive: x.id == threadId,
                  createdAt: x.createdAt,
                  lastActivityAt: x.lastActivityAt,
                  summary: x.summary,
                ))
            .toList();
      }
    }
    return threadsToReturn.where((t) => t.id == threadId).firstOrNull;
  }

  @override
  Future<Thread?> getActiveThread(String sessionId) async =>
      threadsToReturn.where((t) => t.isActive).firstOrNull;

  @override
  Future<bool> deleteThread(String sessionId, String threadId) async {
    threadsToReturn = threadsToReturn.where((t) => t.id != threadId).toList();
    return true;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

void main() {
  group('ThreadSelector', () {
    testWidgets('renders "new thread" button when no threads exist', (tester) async {
      final container = ProviderContainer(overrides: [
        threadServiceProvider.overrideWithValue(_FakeThreadService(const [])),
      ]);
      addTearDown(container.dispose);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
            home: Scaffold(body: ThreadSelector(sessionId: 's1')),
          ),
        ),
      );
      await tester.pump();
      // Initial state has no threads → shows "new thread" TextButton
      expect(find.text('new thread'), findsOneWidget);
    });

    testWidgets('renders PopupMenuButton when threads exist after load', (tester) async {
      final now = DateTime.now();
      final threads = [
        Thread(
          id: 't1',
          sessionId: 's1',
          topicLabel: 'work',
          conversationId: 'c1',
          isActive: true,
          createdAt: now,
          lastActivityAt: now,
        ),
      ];
      final container = ProviderContainer(overrides: [
        threadServiceProvider.overrideWithValue(_FakeThreadService(threads)),
      ]);
      addTearDown(container.dispose);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
            home: Scaffold(body: ThreadSelector(sessionId: 's1')),
          ),
        ),
      );
      // Manually trigger load since the widget doesn't auto-load
      container.read(threadSelectorProvider('s1').notifier).load();
      await tester.pumpAndSettle();

      // With threads loaded, shows the PopupMenuButton with the active thread's label
      expect(find.byType(PopupMenuButton<String>), findsOneWidget);
      expect(find.text('work'), findsOneWidget);
    });

    testWidgets('shows multiple thread labels in popup menu after load', (tester) async {
      final now = DateTime.now();
      final threads = [
        Thread(
          id: 't1',
          sessionId: 's1',
          topicLabel: 'work',
          conversationId: 'c1',
          isActive: true,
          createdAt: now,
          lastActivityAt: now,
        ),
        Thread(
          id: 't2',
          sessionId: 's1',
          topicLabel: 'lunch',
          conversationId: 'c2',
          isActive: false,
          createdAt: now,
          lastActivityAt: now,
        ),
      ];
      final container = ProviderContainer(overrides: [
        threadServiceProvider.overrideWithValue(_FakeThreadService(threads)),
      ]);
      addTearDown(container.dispose);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
            home: Scaffold(body: ThreadSelector(sessionId: 's1')),
          ),
        ),
      );
      container.read(threadSelectorProvider('s1').notifier).load();
      await tester.pumpAndSettle();

      // Active thread label is shown on the closed popup trigger
      expect(find.text('work'), findsOneWidget);
      expect(find.text('lunch'), findsNothing); // hidden until popup opens

      // Tap to open popup
      await tester.tap(find.byType(PopupMenuButton<String>));
      await tester.pumpAndSettle();
      expect(find.text('lunch'), findsOneWidget);
    });
  });
}
