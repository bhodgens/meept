// KanbanService bridge tests (leaf 05/04). Fake Dio adapter records every
// request; a fake event connector stands in for the WS relay so no socket
// is touched (dart:io-free, kIsWeb-safe by construction).

import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/kanban/kanban_models.dart';
import 'package:meept_ui/features/kanban/kanban_service.dart';

/// Records requests and replays scripted answers, mirroring the
/// _FakeAdapter precedent in test/services/sdk_client_submit_test.dart.
class _ScriptedAdapter implements HttpClientAdapter {
  _ScriptedAdapter();

  /// Queue of (status, body) answers; when empty, replies 200 + {}.
  final List<(int, Object)> answers = [];
  final List<RequestOptions> captured = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    captured.add(options);
    final (status, body) = answers.isEmpty
        ? (200, <String, dynamic>{})
        : answers.removeAt(0);
    return ResponseBody.fromString(
      body is String ? body : jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _FakeSocket implements KanbanEventSocket {
  _FakeSocket(this.uri, this.protocols);

  final Uri uri;
  final List<String> protocols;
  final controller = StreamController<dynamic>.broadcast();

  @override
  Stream<dynamic> get messages => controller.stream;

  @override
  Future<void> close() async {
    await controller.close();
  }
}

KanbanService _service(_ScriptedAdapter adapter, {String? token = 'tok-1'}) {
  final dio = Dio(
    BaseOptions(
      baseUrl: 'http://127.0.0.1:8790',
      validateStatus: (status) => status != null && status < 500,
      headers: {if (token != null) 'Authorization': 'Bearer $token'},
    ),
  )..httpClientAdapter = adapter;
  return KanbanService(
    host: '127.0.0.1',
    port: 8790,
    bearerToken: token,
    dio: dio,
  );
}

void main() {
  late _ScriptedAdapter adapter;
  late KanbanService service;

  setUp(() {
    adapter = _ScriptedAdapter();
    service = _service(adapter);
  });

  group('restRoutes parity (gateway.go:78-92)', () {
    test('reads are GET with query-string params', () async {
      adapter.answers.add((
        200,
        {
          'boards': [
            {'ID': 'b1', 'Name': 'default'},
          ],
        },
      ));
      await service.listBoards();
      expect(adapter.captured.single.method, 'GET');
      expect(adapter.captured.single.uri.path, '/api/v1/board/list');

      adapter.captured.clear();
      adapter.answers.add((200, {'cards': <Object>[]}));
      await service.listCards(boardId: 'b1', columnId: 'c2', limit: 10);
      final q = adapter.captured.single.queryParameters;
      expect(q['board_id'], 'b1');
      expect(q['column_id'], 'c2');
      expect(q['limit'], '10');

      adapter.captured.clear();
      adapter.answers.add((200, {'ID': 'card-9', 'Title': 't'}));
      await service.getCard('card-9');
      expect(adapter.captured.single.method, 'GET');
      expect(adapter.captured.single.uri.path, '/api/v1/card/get');
      expect(adapter.captured.single.queryParameters['card_id'], 'card-9');

      adapter.captured.clear();
      adapter.answers.add((200, {'board_id': 'b1'}));
      await service.listColumns('b1');
      expect(adapter.captured.single.uri.path, '/api/v1/column/list');
      expect(adapter.captured.single.queryParameters['board_id'], 'b1');

      adapter.captured.clear();
      adapter.answers.add((200, []));
      await service.cardTimeline('card-9');
      expect(adapter.captured.single.uri.path, '/api/v1/card/timeline');
      expect(adapter.captured.single.queryParameters['card_id'], 'card-9');
    });

    test('mutations are POST with JSON bodies', () async {
      adapter.answers.add((200, {'ID': 'b2', 'Name': 'ops'}));
      await service.createBoard('ops');
      var req = adapter.captured.single;
      expect(req.method, 'POST');
      expect(req.uri.path, '/api/v1/board/create');
      expect(req.data, {'name': 'ops'});

      adapter.captured.clear();
      adapter.answers.add((200, {}));
      await service.setColumnWip('c1', 3);
      req = adapter.captured.single;
      expect(req.uri.path, '/api/v1/column/set_wip');
      expect(req.data, {'column_id': 'c1', 'wip': 3});

      adapter.captured.clear();
      adapter.answers.add((200, {'ID': 'c1'}));
      await service.createCard(
        boardId: 'b1',
        columnId: 'c1',
        title: 'fix bug',
        labels: const ['p0'],
      );
      req = adapter.captured.single;
      expect(req.uri.path, '/api/v1/card/create');
      expect(req.data['board_id'], 'b1');
      expect(req.data['column_id'], 'c1');
      expect(req.data['title'], 'fix bug');
      expect(req.data['labels'], ['p0']);

      adapter.captured.clear();
      adapter.answers.add((200, {}));
      await service.moveCard('card-1', 'col-2', order: 4);
      req = adapter.captured.single;
      expect(req.uri.path, '/api/v1/card/move');
      expect(req.data, {'card_id': 'card-1', 'column_id': 'col-2', 'order': 4});

      adapter.captured.clear();
      adapter.answers.add((200, {}));
      await service.takeoverCard('card-1', toColumnId: 'col-3');
      req = adapter.captured.single;
      expect(req.uri.path, '/api/v1/card/takeover');
      expect(req.data['card_id'], 'card-1');
      expect(req.data['to_column_id'], 'col-3');

      adapter.captured.clear();
      adapter.answers.add((200, {}));
      await service.assignCard('card-1', 'emp-7');
      req = adapter.captured.single;
      expect(req.uri.path, '/api/v1/card/assign');
      expect(req.data, {'card_id': 'card-1', 'employee_id': 'emp-7'});

      adapter.captured.clear();
      adapter.answers.add((200, {'ID': 'e1'}));
      await service.commentCard('card-1', 'looks good');
      req = adapter.captured.single;
      expect(req.uri.path, '/api/v1/card/comment');
      expect(req.data['card_id'], 'card-1');
      expect(req.data['body'], 'looks good');

      adapter.captured.clear();
      adapter.answers.add((
        200,
        {'Created': 1, 'Moved': 2, 'Fixed': 3, 'Deleted': 4},
      ));
      await service.reconcileNow();
      req = adapter.captured.single;
      expect(req.method, 'POST');
      expect(req.uri.path, '/api/v1/reconcile/now');
    });

    test(
      'bearer token is sent as Authorization header on every call',
      () async {
        adapter.answers.add((200, {}));
        await service.listBoards();
        expect(
          adapter.captured.single.headers['Authorization'],
          'Bearer tok-1',
        );
      },
    );
  });

  group('model JSON decoding (C1 exact keys)', () {
    test('card round-trips with Go-default PascalCase and snake_case', () {
      final pascal = KanbanCard.fromJson({
        'ID': 'card-1',
        'BoardID': 'board-1',
        'ColumnID': 'col-2',
        'Title': 'write bridge',
        'Ref': {'Kind': 'goal', 'ID': 'goal-9'},
        'Labels': ['urgent', 'gui'],
        'GoalID': 'goal-9',
        'EmployeeID': 'emp-1',
        'PlanID': 'plan-3',
        'SessionID': 'sess-5',
        'TriggerRef': 'intake:repo#12',
        'Order': 7,
      });
      expect(pascal.id, 'card-1');
      expect(pascal.columnId, 'col-2');
      expect(pascal.ref.kind, 'goal');
      expect(pascal.ref.id, 'goal-9');
      expect(pascal.labels, ['urgent', 'gui']);
      expect(pascal.goalId, 'goal-9');
      expect(pascal.employeeId, 'emp-1');
      expect(pascal.planId, 'plan-3');
      expect(pascal.sessionId, 'sess-5');
      expect(pascal.triggerRef, 'intake:repo#12');
      expect(pascal.order, 7);

      final snake = KanbanCard.fromJson(pascal.toJson());
      expect(snake.id, pascal.id);
      expect(snake.boardId, pascal.boardId);
      expect(snake.columnId, pascal.columnId);
      expect(snake.ref, isA<KanbanWorkRef>());
      expect(snake.ref.kind, 'goal');
      expect(snake.labels, pascal.labels);
      expect(snake.goalId, pascal.goalId);
      expect(snake.employeeId, pascal.employeeId);
      expect(snake.planId, pascal.planId);
      expect(snake.sessionId, pascal.sessionId);
      expect(snake.triggerRef, pascal.triggerRef);
      expect(snake.order, pascal.order);
    });

    test('state event uses the tagged lowercase keys verbatim', () {
      final ev = KanbanStateEvent.fromJson({
        'kind': 'goal',
        'id': 'goal-1',
        'from': 'active',
        'to': 'done',
        'reason': 'gate passed',
        'ts': '2026-11-15T10:30:00Z',
      });
      expect(ev.kind, 'goal');
      expect(ev.from, 'active');
      expect(ev.to, 'done');
      expect(ev.ts, DateTime.parse('2026-11-15T10:30:00Z'));
      final back = KanbanStateEvent.fromJson(ev.toJson());
      expect(back.reason, 'gate passed');
      expect(back.ts, ev.ts);
    });

    test('card event (timeline row) decodes both spellings', () {
      final ev = KanbanCardEvent.fromJson({
        'ID': 'evt-1',
        'CardID': 'card-1',
        'Kind': 'comment',
        'Actor': 'user',
        'Body': 'note',
        'CreatedAt': '2026-11-15T10:00:00Z',
      });
      expect(ev.id, 'evt-1');
      expect(ev.cardId, 'card-1');
      expect(ev.kind, 'comment');
      expect(ev.createdAt, isNotNull);
      final snake = KanbanCardEvent.fromJson({
        'id': 'evt-2',
        'card_id': 'card-2',
        'kind': 'moved',
        'actor': 'agent',
        'body': '',
        'created_at': '2026-11-15T11:00:00Z',
      });
      expect(snake.cardId, 'card-2');
      expect(snake.createdAt, isNotNull);
    });

    test('column/board/card-moved/board-changed/reconcile stats decode', () {
      final col = KanbanColumn.fromJson({
        'ID': 'c1',
        'BoardID': 'b1',
        'Name': 'doing',
        'Position': 2,
        'WIP': 5,
      });
      expect(col.position, 2);
      expect(col.wip, 5);

      final board = KanbanBoard.fromJson({'ID': 'b1', 'Name': 'default'});
      expect(board.name, 'default');

      final moved = KanbanCardMoved.fromJson({
        'CardID': 'card-1',
        'FromColumnID': 'c1',
        'ToColumnID': 'c2',
        'Actor': 'user',
        'LeaseBlocked': true,
      });
      expect(moved.leaseBlocked, isTrue);
      expect(moved.toColumnId, 'c2');

      final changed = KanbanBoardChanged.fromJson({
        'board_id': 'b1',
        'note': 'wip warn',
      });
      expect(changed.boardId, 'b1');
      expect(changed.note, 'wip warn');

      final stats = KanbanReconcileStats.fromJson({
        'Created': 1,
        'Moved': 2,
        'Fixed': 3,
        'Deleted': 4,
      });
      expect(stats.total, 10);
    });

    test('service unwraps bare-array card/list replies too', () async {
      adapter.answers.add((
        200,
        [
          {'ID': 'a'},
          {'ID': 'b'},
        ],
      ));
      final cards = await service.listCards(boardId: 'b1');
      expect(cards.map((c) => c.id), ['a', 'b']);
    });
  });

  group('error mapping table', () {
    Future<Object?> callExpecting(int status, Object body) async {
      adapter.answers.add((status, body));
      try {
        await service.listBoards();
        return null;
      } on KanbanApiException catch (e) {
        return e;
      }
    }

    test('401 -> KanbanUnauthorizedException', () async {
      final e = await callExpecting(401, {'error': 'unknown bearer token'});
      expect(e, isA<KanbanUnauthorizedException>());
      expect((e as KanbanApiException).message, 'unknown bearer token');
    });

    test('403 -> KanbanForbiddenException names the grant', () async {
      final e =
          await callExpecting(403, {'error': 'missing grant board.takeover'})
              as KanbanForbiddenException;
      expect(e.grant, 'board.takeover');
    });

    test('400 -> KanbanBadRequestException', () async {
      final e = await callExpecting(400, {
        'error': 'decode request body: bad json',
      });
      expect(e, isA<KanbanBadRequestException>());
    });

    test(
      '404 -> KanbanNotFoundException (gateway + upstream phrasings)',
      () async {
        final e = await callExpecting(404, {'error': 'unknown api path'});
        expect(e, isA<KanbanNotFoundException>());
      },
    );

    test(
      '502 lease-held message -> structured KanbanLeaseHeldException',
      () async {
        final e =
            await callExpecting(502, {
                  'rpc': 'kanban.card.move',
                  'error':
                      'kanban: move: lease held by episode-42 since 2026-11-15T09:00:00Z',
                })
                as KanbanLeaseHeldException;
        expect(e.holder, 'episode-42');
        expect(e.since, isNotNull);
        expect(e.age, isNotNull);
        expect(e.age!.inSeconds, greaterThan(0));
      },
    );

    test('502 wip-full message -> structured KanbanWipFullException', () async {
      final e =
          await callExpecting(502, {
                'rpc': 'kanban.card.move',
                'error': 'kanban: WIP limit reached for column col-3: 5',
              })
              as KanbanWipFullException;
      expect(e.columnId, 'col-3');
      expect(e.limit, 5);
    });

    test(
      '502 unknown-card message -> KanbanNotFoundException with cardId',
      () async {
        final e =
            await callExpecting(502, {
                  'rpc': 'kanban.card.get',
                  'error': 'unknown card: card-77',
                })
                as KanbanNotFoundException;
        expect(e.cardId, 'card-77');
      },
    );

    test(
      '502 plain upstream failure -> KanbanUpstreamException names rpc',
      () async {
        final e =
            await callExpecting(502, {
                  'rpc': 'kanban.board.list',
                  'error': 'daemon: store read failed',
                })
                as KanbanUpstreamException;
        expect(e.rpc, 'kanban.board.list');
      },
    );

    test('transport failure -> KanbanConnectionException', () async {
      final failing = Dio(BaseOptions(baseUrl: 'http://127.0.0.1:8790'))
        ..httpClientAdapter = _ThrowingAdapter();
      final svc = KanbanService(
        host: '127.0.0.1',
        port: 8790,
        bearerToken: 't',
        dio: failing,
      );
      await expectLater(
        svc.listBoards(),
        throwsA(isA<KanbanConnectionException>()),
      );
    });
  });

  group('boardEvents (C8 interim progress-class placeholder)', () {
    test('connects the relay with the bearer subprotocol lane', () async {
      final sockets = <_FakeSocket>[];
      final svc = KanbanService(
        host: '127.0.0.1',
        port: 8790,
        bearerToken: 'tok-1',
        dio: (Dio(BaseOptions(baseUrl: 'http://127.0.0.1:8790'))
          ..httpClientAdapter = adapter),
        eventConnector: (uri, protocols) async {
          final s = _FakeSocket(uri, protocols);
          sockets.add(s);
          return s;
        },
      );
      final events = <KanbanBoardEvent>[];
      final sub = svc.boardEvents().listen(events.add);
      await Future<void>.delayed(Duration.zero);

      expect(sockets.single.uri.path, '/api/v1/events');
      expect(sockets.single.uri.scheme, 'ws');
      expect(sockets.single.protocols, ['bearer.tok-1']);

      // A progress-class frame (C8 interim: board deltas ride this class).
      sockets.single.controller.add(
        jsonEncode({
          'type': 'agent_progress',
          'payload': {'topic': 'kanban.board.changed', 'board_id': 'b1'},
          'board_id': 'b1',
        }),
      );
      await Future<void>.delayed(Duration.zero);
      expect(events, hasLength(1));
      expect(events.single.isProgress, isTrue);
      expect(events.single.wireType, 'agent_progress');
      expect(events.single.boardId, 'b1');

      // chat_message frames must never leak onto this bridge.
      sockets.single.controller.add(
        jsonEncode({'type': 'chat_message', 'content': 'x'}),
      );
      await Future<void>.delayed(Duration.zero);
      expect(events, hasLength(1));

      // Malformed frame surfaces as a linkError, never an exception.
      sockets.single.controller.add('{not json');
      await Future<void>.delayed(Duration.zero);
      expect(events.last.linkError, contains('malformed'));

      await sub.cancel();
      await svc.dispose();
      // Second subscribe after dispose: empty stream, no socket churn.
      expect(await svc.boardEvents().toList(), isEmpty);
    });

    test(
      'socket drop retries with bounded reconnect then reports unavailable',
      () async {
        var attempts = 0;
        final svc = KanbanService(
          host: '127.0.0.1',
          port: 8790,
          bearerToken: 'tok-1',
          dio: (Dio(BaseOptions(baseUrl: 'http://127.0.0.1:8790'))
            ..httpClientAdapter = adapter),
          eventConnector: (uri, protocols) async {
            attempts += 1;
            // Always fail: the connector throws before the first socket.
            throw StateError('connect boom');
          },
        );
        final events = <KanbanBoardEvent>[];
        svc.boardEvents().listen(events.add);
        await Future<void>.delayed(Duration.zero);
        expect(events.single.linkError, contains('reconnecting'));
        expect(attempts, 1);
        await svc.dispose();
      },
    );
  });

  group('interim-rule documentation pins', () {
    test('KanbanBoardEvent progress classification is default', () {
      const ev = KanbanBoardEvent({'type': 'agent_progress'});
      expect(ev.isProgress, isTrue);
      const noType = KanbanBoardEvent({});
      expect(noType.isProgress, isTrue); // absent type => progress class
    });
  });
}

class _ThrowingAdapter implements HttpClientAdapter {
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    throw DioException(
      requestOptions: options,
      type: DioExceptionType.connectionError,
    );
  }

  @override
  void close({bool force = false}) {}
}
