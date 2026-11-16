// Widget tests for the Kanban board view (plan tree kanban-board 05/05).
// The provider seam (kanbanServiceProvider) is overridden with
// FakeKanbanService — no network, no socket, no dart:io. Covers: column +
// card rendering with the TUI wip legend, tap-to-move args, C10 lease /
// WIP / forbidden snackbars, the snackbar takeover affordance passing the
// intended target column, card detail (timeline oldest-first + comment),
// and the debounced single reload for a burst of boardEvents frames.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/kanban/board_view.dart';
import 'package:meept_ui/features/kanban/kanban_errors.dart';
import 'package:meept_ui/features/kanban/kanban_models.dart';
import 'package:meept_ui/features/kanban/kanban_provider.dart';

import 'fake_kanban_service.dart';

const _todo = KanbanColumn(
  id: 'c-todo',
  boardId: 'b1',
  name: 'Todo',
  position: 0,
  wip: 0,
);
const _doing = KanbanColumn(
  id: 'c-doing',
  boardId: 'b1',
  name: 'Doing',
  position: 1,
  wip: 2,
);
const _done = KanbanColumn(
  id: 'c-done',
  boardId: 'b1',
  name: 'Done',
  position: 2,
  wip: 0,
);

KanbanCard _card(
  String id,
  String columnId, {
  String title = 'tune the engine',
  String employeeId = '',
  List<String> labels = const [],
  String refKind = '',
  int order = 0,
}) => KanbanCard(
  id: id,
  boardId: 'b1',
  columnId: columnId,
  title: title,
  ref: refKind.isEmpty
      ? const KanbanWorkRef()
      : KanbanWorkRef(kind: refKind, id: 'r-$id'),
  labels: labels,
  employeeId: employeeId,
  order: order,
);

Widget _host(FakeKanbanService service) => ProviderScope(
  overrides: [kanbanServiceProvider.overrideWithValue(service)],
  child: const MaterialApp(home: Scaffold(body: KanbanBoardView())),
);

/// Pump the view until the initial load settles (fake futures are
/// microtask-immediate, but the view chains listBoards → listColumns →
/// listCards, and _refresh awaits them).
Future<void> _pumpBoard(WidgetTester tester) async {
  await tester.pumpAndSettle();
}

void main() {
  late FakeKanbanService service;

  setUp(() {
    service = FakeKanbanService()
      ..columns = const [_todo, _doing, _done]
      ..cards = [
        _card('k1', 'c-todo', title: 'write the brief', refKind: 'goal'),
        _card(
          'k2',
          'c-doing',
          title: 'ship the bridge',
          employeeId: 'emp-7',
          labels: const ['urgent'],
        ),
      ];
  });

  group('board rendering', () {
    testWidgets('columns and cards render with the wip legend', (tester) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);

      // header + lowercase legend matching TUI semantics:
      // "doing (1/2 wip)" for the column holding 1 of 2 cards.
      expect(find.text('kanban board'), findsOneWidget);
      expect(find.text('todo (1)'), findsOneWidget);
      expect(find.text('doing (1/2 wip)'), findsOneWidget);
      expect(find.text('done (0)'), findsOneWidget);

      // card content: title, ref kind, employee, labels
      expect(find.text('write the brief'), findsOneWidget);
      expect(find.text('goal'), findsOneWidget);
      expect(find.text('ship the bridge'), findsOneWidget);
      expect(find.text('emp emp-7'), findsOneWidget);
      expect(find.text('urgent'), findsOneWidget);

      // columns scroll horizontally: the off-screen done column is there
      final board = tester.widget<ListView>(
        find
            .descendant(
              of: find.byType(KanbanBoardView),
              matching: find.byType(ListView),
            )
            .first,
      );
      expect(board.scrollDirection, Axis.horizontal);
    });

    testWidgets('load failure surfaces a lowercase error + retry', (
      tester,
    ) async {
      service.boardLoads = 0;
      final broken = _ThrowingService();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [kanbanServiceProvider.overrideWithValue(broken)],
          child: const MaterialApp(home: Scaffold(body: KanbanBoardView())),
        ),
      );
      await _pumpBoard(tester);
      expect(
        find.textContaining('kanban error: sidecar offline'),
        findsOneWidget,
      );
      expect(find.text('retry'), findsOneWidget);
    });
  });

  group('tap-to-move', () {
    testWidgets('move menu calls moveCard with card + target column', (
      tester,
    ) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);

      await tester.tap(find.byKey(const ValueKey('kanban-move-k1')));
      await tester.pumpAndSettle();
      // menu lists the OTHER columns, lowercase
      expect(find.text('doing'), findsOneWidget);
      expect(find.text('done'), findsOneWidget);
      expect(find.text('todo'), findsNothing); // never its own column

      await tester.tap(find.text('done'));
      await _pumpBoard(tester);

      expect(service.moves, [('k1', 'c-done')]);
      expect(
        find.textContaining('moved "write the brief" to done'),
        findsOneWidget,
      );
    });

    testWidgets('lease-held refusal shows holder + age + takeover hint', (
      tester,
    ) async {
      service.moveError = KanbanLeaseHeldException(
        'lease held by ep-42 since 2026-11-15T09:00:00Z',
        holder: 'ep-42',
        since: DateTime.now().subtract(const Duration(minutes: 12)),
      );
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);

      await tester.tap(find.byKey(const ValueKey('kanban-move-k2')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('done'));
      await _pumpBoard(tester);

      expect(find.textContaining('lease held by ep-42 ('), findsOneWidget);
      expect(find.textContaining('12m'), findsOneWidget);
      expect(find.textContaining('takeover overrides'), findsOneWidget);
      // the takeover affordance lives in the snackbar action
      expect(find.text('takeover'), findsOneWidget);

      // pressing takeover targets the intended column
      await tester.tap(find.text('takeover'));
      await _pumpBoard(tester);
      expect(service.takeovers, [('k2', 'c-done')]);
      expect(
        find.textContaining('took over and moved to done'),
        findsOneWidget,
      );
    });

    testWidgets('wip-full refusal names column + limit', (tester) async {
      service.moveError = const KanbanWipFullException(
        'WIP limit reached for column c-doing: 2',
        columnId: 'c-doing',
        limit: 2,
      );
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);

      await tester.tap(find.byKey(const ValueKey('kanban-move-k1')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('doing'));
      await _pumpBoard(tester);

      expect(
        find.textContaining('wip full: column doing limit 2'),
        findsOneWidget,
      );
    });

    testWidgets('forbidden refusal names the grant', (tester) async {
      service.moveError = const KanbanForbiddenException(
        'missing grant board.mutate',
        grant: 'board.mutate',
      );
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);

      await tester.tap(find.byKey(const ValueKey('kanban-move-k1')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('done'));
      await _pumpBoard(tester);

      expect(
        find.textContaining(
          'forbidden: token is missing the board.mutate grant',
        ),
        findsOneWidget,
      );
    });
  });

  group('reactive refresh (C8 interim boardEvents)', () {
    testWidgets('a burst of 5 frames coalesces into ONE reload', (
      tester,
    ) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);
      final loadsBefore = service.boardLoads;

      for (var i = 0; i < 5; i++) {
        service.emit(const KanbanBoardEvent({'type': 'agent_progress'}));
      }
      // debounce not yet elapsed: still exactly the initial loads
      await tester.pump(const Duration(milliseconds: 100));
      expect(service.boardLoads, loadsBefore);

      // after the ~250 ms window, exactly one reload fires
      await tester.pump(const Duration(milliseconds: 250));
      expect(service.boardLoads, loadsBefore + 1);
      await _pumpBoard(tester);
      expect(service.boardLoads, loadsBefore + 1);
    });

    testWidgets('frames for another board are ignored', (tester) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);
      final loadsBefore = service.boardLoads;

      service.emit(const KanbanBoardEvent({'board_id': 'other-board'}));
      await tester.pump(const Duration(milliseconds: 400));
      expect(service.boardLoads, loadsBefore);
    });

    testWidgets('linkError frames show the note, no reload', (tester) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);
      final loadsBefore = service.boardLoads;

      service.emit(
        const KanbanBoardEvent(
          {},
          linkError: 'board events reconnecting (attempt 1/3)',
        ),
      );
      await tester.pump();
      await tester.pump();
      expect(find.textContaining('board events reconnecting'), findsOneWidget);
      await tester.pump(const Duration(milliseconds: 400));
      expect(service.boardLoads, loadsBefore);
    });
  });

  group('card detail', () {
    setUp(() {
      service.timeline = [
        KanbanCardEvent(
          id: 'e2',
          cardId: 'k1',
          kind: 'moved',
          actor: 'emp-7',
          body: '',
          createdAt: DateTime(2026, 11, 15, 9),
        ),
        KanbanCardEvent(
          id: 'e1',
          cardId: 'k1',
          kind: 'comment',
          actor: 'human',
          body: 'looks good',
          createdAt: DateTime(2026, 11, 15, 8),
        ),
      ];
    });

    testWidgets('tap opens detail with timeline oldest-first + comment box', (
      tester,
    ) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);

      await tester.tap(find.byKey(const ValueKey('kanban-card-k1')));
      await _pumpBoard(tester);

      expect(service.timelineRequests, ['k1']);
      // comment input + send exist
      expect(
        find.byKey(const ValueKey('kanban-comment-input')),
        findsOneWidget,
      );
      expect(find.text('comment'), findsOneWidget);

      // timeline rows: the older comment row precedes the newer moved row
      final rows = find.descendant(
        of: find.byKey(const ValueKey('kanban-timeline')),
        matching: find.byType(Text),
      );
      expect(rows, findsNWidgets(2));
      final firstRowRect = tester.getTopLeft(rows.at(0));
      final secondRowRect = tester.getTopLeft(rows.at(1));
      expect(firstRowRect.dy, lessThan(secondRowRect.dy));
      // oldest row content: 08:00 comment "looks good"
      expect(tester.widget<Text>(rows.at(0)).data, contains('looks good'));
    });

    testWidgets('comment posts through the service and lands in the timeline', (
      tester,
    ) async {
      await tester.pumpWidget(_host(service));
      await _pumpBoard(tester);
      await tester.tap(find.byKey(const ValueKey('kanban-card-k1')));
      await _pumpBoard(tester);

      await tester.enterText(
        find.byKey(const ValueKey('kanban-comment-input')),
        'ship it',
      );
      await tester.tap(find.byKey(const ValueKey('kanban-comment-send')));
      await _pumpBoard(tester);

      expect(service.comments, [('k1', 'ship it')]);
      // the new comment appears as a third timeline row (newest last)
      final rows = find.descendant(
        of: find.byKey(const ValueKey('kanban-timeline')),
        matching: find.byType(Text),
      );
      expect(rows, findsNWidgets(3));
      expect(tester.widget<Text>(rows.at(2)).data, contains('ship it'));
    });
  });
}

/// Fails every read so the error path renders.
class _ThrowingService extends FakeKanbanService {
  @override
  Future<List<KanbanBoard>> listBoards() async {
    throw const KanbanConnectionException('sidecar offline (127.0.0.1:8790)');
  }
}
