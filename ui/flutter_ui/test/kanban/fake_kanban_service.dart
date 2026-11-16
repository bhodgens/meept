// Fake KanbanService for board-view widget tests (leaf 05/05). The bridge
// itself is frozen (05/04 shipped), so the fake is a subclass with every
// network-touching method overridden — no Dio, no socket, no dart:io.
// boardEvents() is backed by a controllable broadcast controller so tests
// can script the C8 interim frame stream exactly.

import 'dart:async';

import 'package:meept_ui/features/kanban/kanban_models.dart';
import 'package:meept_ui/features/kanban/kanban_service.dart';

class FakeKanbanService extends KanbanService {
  FakeKanbanService() : super(host: '127.0.0.1', port: 8790);

  List<KanbanBoard> boards = const [KanbanBoard(id: 'b1', name: 'default')];
  List<KanbanColumn> columns = const [];
  List<KanbanCard> cards = const [];
  List<KanbanCardEvent> timeline = const [];

  /// Scripted failures (thrown from the matching call when non-null).
  Object? moveError;
  Object? takeoverError;
  Object? commentError;

  /// Call capture for assertions.
  final List<(String cardId, String columnId)> moves = [];
  final List<(String cardId, String? toColumnId)> takeovers = [];
  final List<(String cardId, String body)> comments = [];
  final List<String> timelineRequests = [];

  /// Reload accounting: every full board fetch bumps this.
  int boardLoads = 0;

  final _events = StreamController<KanbanBoardEvent>.broadcast();

  void emit(KanbanBoardEvent event) => _events.add(event);

  @override
  Stream<KanbanBoardEvent> boardEvents() => _events.stream;

  @override
  Future<List<KanbanBoard>> listBoards() async {
    boardLoads++;
    return boards;
  }

  @override
  Future<List<KanbanColumn>> listColumns(String boardId) async => columns;

  @override
  Future<List<KanbanCard>> listCards({
    String? boardId,
    String? columnId,
    int? limit,
  }) async => cards;

  @override
  Future<void> moveCard(
    String cardId,
    String columnId, {
    int? order,
    String? actor,
  }) async {
    moves.add((cardId, columnId));
    if (moveError != null) throw moveError!;
    cards = [
      for (final c in cards)
        if (c.id == cardId)
          KanbanCard(
            id: c.id,
            boardId: c.boardId,
            columnId: columnId,
            title: c.title,
            ref: c.ref,
            labels: c.labels,
            goalId: c.goalId,
            employeeId: c.employeeId,
            planId: c.planId,
            sessionId: c.sessionId,
            triggerRef: c.triggerRef,
            order: c.order,
          )
        else
          c,
    ];
  }

  @override
  Future<void> takeoverCard(
    String cardId, {
    String? toColumnId,
    String? actor,
  }) async {
    takeovers.add((cardId, toColumnId));
    if (takeoverError != null) throw takeoverError!;
  }

  @override
  Future<List<KanbanCardEvent>> cardTimeline(String cardId) async {
    timelineRequests.add(cardId);
    return timeline;
  }

  @override
  Future<KanbanCardEvent> commentCard(String cardId, String text) async {
    comments.add((cardId, text));
    if (commentError != null) throw commentError!;
    return KanbanCardEvent(
      id: 'e-comment',
      cardId: cardId,
      kind: 'comment',
      actor: 'human',
      body: text,
      createdAt: DateTime(2026, 11, 15, 10),
    );
  }

  @override
  Future<void> dispose() async {
    await _events.close();
  }
}
