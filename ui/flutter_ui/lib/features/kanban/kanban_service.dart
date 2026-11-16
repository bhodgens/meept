// KanbanService — the Dart bridge to the kanban sidecar gateway REST/WS
// surface (plan tree: kanban-board 05/04 'flutter-bridge').
//
// Contract sources (frozen; do not silently drift):
//   * Paths/verbs/param shapes: internal/kanbansvc/gateway.go `restRoutes`
//     (C4 map) — reads take query-string params, mutations take a JSON body
//     (`buildParams`, gateway.go:183).
//   * Grants/auth: C5 — Authorization: Bearer <token>; the WS lane uses the
//     Sec-WebSocket-Protocol "bearer.<token>" subprotocol because browsers
//     cannot set WS headers (gateway.go `wsBearer`/`pickSubprotocol`).
//   * Error bodies: {"error": msg} for gateway answers and {"rpc", "error"}
//     for forwarded daemon failures (writeJSONError / writeRPCError). 401 =
//     missing/unknown token, 403 = known token without the named grant,
//     404 = unknown path/method-not-found/upstream "not found", 502 =
//     upstream RPC failure — where kanban domain errors (lease held, WIP
//     full, unknown card) surface as messages.
//   * WS event class: C8 interim — WSBoard does not exist until leaf 05/07,
//     so board deltas ride the progress class. [boardEvents] therefore
//     yields progress-class [KanbanBoardEvent]s only and never decodes
//     WSBoard-typed payloads.
//
// No dart:io anywhere (web + desktop shared code). WebSocket transport is
// injected via [KanbanEventConnector] so tests never touch a real socket.

import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart' show kIsWeb, debugPrint;
import 'package:web_socket_channel/web_socket_channel.dart';

import '../../core/constants.dart';
import 'kanban_errors.dart';
import 'kanban_models.dart';

export 'kanban_errors.dart';

// ---------------------------------------------------------------------------
// WebSocket seam (injectable for tests; kIsWeb-safe)
// ---------------------------------------------------------------------------

/// Minimal view of an event socket the service needs. [WebSocketChannel]
/// satisfies it structurally through [_WebSocketChannelAdapter]; tests can
/// hand in a stream-backed fake without any real socket.
abstract class KanbanEventSocket {
  Stream<dynamic> get messages;
  Future<void> close();
}

typedef KanbanEventConnector =
    Future<KanbanEventSocket> Function(Uri uri, List<String> protocols);

class _WebSocketChannelAdapter implements KanbanEventSocket {
  _WebSocketChannelAdapter(this._channel);
  final WebSocketChannel _channel;

  @override
  Stream<dynamic> get messages => _channel.stream;

  @override
  Future<void> close() async {
    try {
      await _channel.sink.close();
    } catch (e) {
      debugPrint('[kanban] event socket close failed: $e');
    }
  }
}

/// Default connector: opens a real WebSocketChannel with the bearer lane.
/// Browsers negotiate auth through Sec-WebSocket-Protocol only (C5/C7);
/// native passes the header too, matching WebSocketService's precedent.
Future<KanbanEventSocket> _defaultConnect(
  Uri uri,
  List<String> protocols,
) async {
  final channel = WebSocketChannel.connect(
    uri,
    protocols: protocols.isEmpty ? null : protocols,
  );
  // On web the connect() returns before the handshake completes; awaiting
  // ready propagates failures (same posture as websocket_service.dart).
  await channel.ready;
  return _WebSocketChannelAdapter(channel);
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

/// REST/WS bridge to the kanban sidecar. One instance per connect config;
/// the bearer token is injected (pairing/dev-key plumbing lives elsewhere —
/// this service NEVER guesses credentials).
class KanbanService {
  KanbanService({
    required String host,
    int? port,
    String? bearerToken,
    String scheme = 'http',
    Dio? dio,
    KanbanEventConnector? eventConnector,
  }) : baseUri = Uri(
         scheme: scheme,
         host: host,
         port: port ?? defaultSidecarPort,
       ),
       _token = bearerToken,
       _dio =
           dio ??
           Dio(
             BaseOptions(
               baseUrl: Uri(
                 scheme: scheme,
                 host: host,
                 port: port ?? defaultSidecarPort,
               ).toString(),
               connectTimeout: AppConstants.connectionTimeout,
               // Board calls are small local-RPC forwards; the daemon's
               // generous chat timeout is wrong here.
               receiveTimeout: const Duration(seconds: 30),
               headers: {
                 'Content-Type': 'application/json',
                 if (bearerToken != null && bearerToken.isNotEmpty)
                   'Authorization': 'Bearer $bearerToken',
               },
               // The bridge maps statuses itself; never throw on 4xx/5xx.
               validateStatus: (status) => status != null && status < 500,
             ),
           ) {
    _eventConnector = eventConnector ?? _defaultConnect;
  }

  /// C7 default sidecar bind port (127.0.0.1:8790).
  static const int defaultSidecarPort = 8790;

  /// WS relay path on the sidecar (gateway.go ServeHTTP: /api/v1/events).
  static const String eventsPath = '/api/v1/events';

  final Uri baseUri;
  final Dio _dio;
  final String? _token;
  late final KanbanEventConnector _eventConnector;

  bool _eventsDisposed = false;
  StreamController<KanbanBoardEvent>? _eventsController;
  KanbanEventSocket? _eventSocket;
  Timer? _reconnectTimer;
  int _reconnectAttempts = 0;
  static const int _maxReconnectAttempts = 3;

  // ------------------------------------------------------------------
  // Reads (grant: board.read; params ride the query string)
  // ------------------------------------------------------------------

  Future<List<KanbanBoard>> listBoards() async {
    final data = await _get('/api/v1/board/list', const {});
    return _listOf(
      data,
      'boards',
    ).map(KanbanBoard.fromJson).toList(growable: false);
  }

  Future<List<KanbanColumn>> listColumns(String boardId) async {
    final data = await _get('/api/v1/column/list', {'board_id': boardId});
    return _listOf(
      data,
      'columns',
    ).map(KanbanColumn.fromJson).toList(growable: false);
  }

  Future<List<KanbanCard>> listCards({
    String? boardId,
    String? columnId,
    int? limit,
  }) async {
    final query = <String, String>{
      if (boardId != null) 'board_id': boardId,
      if (columnId != null) 'column_id': columnId,
      if (limit != null) 'limit': '$limit',
    };
    final data = await _get('/api/v1/card/list', query);
    return _listOf(
      data,
      'cards',
    ).map(KanbanCard.fromJson).toList(growable: false);
  }

  Future<KanbanCard> getCard(String cardId) async {
    final data = await _get('/api/v1/card/get', {'card_id': cardId});
    return KanbanCard.fromJson(_record(data, ['card', 'Card', 'result']));
  }

  Future<List<KanbanCardEvent>> cardTimeline(String cardId) async {
    final data = await _get('/api/v1/card/timeline', {'card_id': cardId});
    return _listOf(
      data,
      'events',
    ).map(KanbanCardEvent.fromJson).toList(growable: false);
  }

  // ------------------------------------------------------------------
  // Mutations (grant: board.mutate — + board.takeover for takeover;
  // params ride the JSON body)
  // ------------------------------------------------------------------

  Future<KanbanBoard> createBoard(String name) async {
    final data = await _post('/api/v1/board/create', {'name': name});
    return KanbanBoard.fromJson(_record(data, ['board', 'Board', 'result']));
  }

  Future<void> setColumnWip(String columnId, int wip) =>
      _post('/api/v1/column/set_wip', {'column_id': columnId, 'wip': wip});

  Future<KanbanCard> createCard({
    required String boardId,
    required String columnId,
    required String title,
    List<String> labels = const [],
    int order = 0,
  }) async {
    final data = await _post('/api/v1/card/create', {
      'board_id': boardId,
      'column_id': columnId,
      'title': title,
      if (labels.isNotEmpty) 'labels': labels,
      if (order != 0) 'order': order,
    });
    return KanbanCard.fromJson(_record(data, ['card', 'Card', 'result']));
  }

  /// Human card move. Lease rule A: throws [KanbanLeaseHeldException] when
  /// the target column maps to an actively-claimed work item — the UI must
  /// display holder + age from the exception (C10). wip_mode:"block" throws
  /// [KanbanWipFullException].
  Future<void> moveCard(
    String cardId,
    String columnId, {
    int? order,
    String? actor,
  }) => _post('/api/v1/card/move', {
    'card_id': cardId,
    'column_id': columnId,
    if (order != null) 'order': order,
    if (actor != null && actor.isNotEmpty) 'actor': actor,
  });

  /// The only lease bypass (C10): cancels the episode, releases the token,
  /// audits, then moves. Requires board.mutate AND board.takeover (C5).
  Future<void> takeoverCard(
    String cardId, {
    String? toColumnId,
    String? actor,
  }) => _post('/api/v1/card/takeover', {
    'card_id': cardId,
    if (toColumnId != null && toColumnId.isNotEmpty) 'to_column_id': toColumnId,
    if (actor != null && actor.isNotEmpty) 'actor': actor,
  });

  Future<void> assignCard(String cardId, String employeeId) => _post(
    '/api/v1/card/assign',
    {'card_id': cardId, 'employee_id': employeeId},
  );

  Future<KanbanCardEvent> commentCard(String cardId, String text) async {
    final data = await _post('/api/v1/card/comment', {
      'card_id': cardId,
      'body': text,
    });
    return KanbanCardEvent.fromJson(
      _record(data, ['event', 'Event', 'card_event', 'CardEvent']),
    );
  }

  /// Force a rebuild pass (contract: after rebuild, board == source).
  Future<KanbanReconcileStats> reconcileNow() async {
    // An empty body is a legal mutation (gateway buildParams forwards nil
    // params); {} keeps the Content-Type valid without asserting param keys
    // 05/01 does not define.
    final data = await _post('/api/v1/reconcile/now', const {});
    return KanbanReconcileStats.fromJson(_record(data, ['created', 'Created']));
  }

  // ------------------------------------------------------------------
  // Events (C8 interim)
  // ------------------------------------------------------------------

  /// Board delta stream. INTERIM RULE (master.md C8, leaf 05/04): the
  /// WSBoard wire class lands in 05/07; until then every frame on the
  /// sidecar relay is progress-class (never chat_message), so this yields
  /// [KanbanBoardEvent]s whose [KanbanBoardEvent.isProgress] is true and
  /// whose payload is the RAW frame — consumers must not depend on
  /// WSBoard-typed keys. The sidecar's live feed is additionally IdleSource
  /// today (no daemon board-event feed wired), so an empty stream that only
  /// closes on dispose is the expected steady state.
  ///
  /// Auth uses the "bearer.<token>" subprotocol lane (browsers cannot set
  /// WS headers; gateway.go wsBearer). One shared socket per service
  /// instance; late subscribers join the same feed. Transient socket
  /// failures surface as linkError events with bounded reconnect (the
  /// sidecar is loopback — 3 retries like AppConstants.maxRetries).
  Stream<KanbanBoardEvent> boardEvents() {
    if (_eventsDisposed) return const Stream<KanbanBoardEvent>.empty();
    final controller = _eventsController;
    if (controller != null) return controller.stream;

    final fresh = StreamController<KanbanBoardEvent>(
      onCancel: () {
        // The socket is owned by the service lifetime, not one listener:
        // keep it until dispose(). (05/05 is the sole planned consumer.)
      },
    );
    _eventsController = fresh;
    _openEventSocket(fresh);
    return fresh.stream;
  }

  void _openEventSocket(StreamController<KanbanBoardEvent> out) {
    if (_eventsDisposed) return;
    final uri = baseUri.replace(
      scheme: baseUri.scheme == 'https' ? 'wss' : 'ws',
      path: eventsPath,
    );
    // C5/C7: token rides the subprotocol lane; the gateway echoes it back
    // (pickSubprotocol) or the browser aborts the upgrade.
    final protocols = <String>[
      if (_token != null && _token.isNotEmpty) 'bearer.$_token',
    ];
    unawaited(
      _eventConnector(uri, protocols)
          .then((socket) {
            _eventSocket = socket;
            _reconnectAttempts = 0;
            socket.messages.listen(
              (data) => _handleEventFrame(out, data),
              onError: (Object e) => _handleEventDrop(out, e),
              onDone: () => _handleEventDrop(out, const _SocketClosed()),
            );
          })
          .catchError((Object e) {
            _handleEventDrop(out, e);
          }),
    );
  }

  void _handleEventFrame(StreamController<KanbanBoardEvent> out, Object? data) {
    if (out.isClosed) return;
    if (data is! String) {
      // The relay sends JSON text frames only (relayEvents → websocket
      // Message.Send(string)). A binary frame is unexpected: pass it along
      // as an opaque progress frame rather than dropping it silently.
      out.add(const KanbanBoardEvent({'type': 'agent_progress'}));
      return;
    }
    Object? decoded;
    try {
      decoded = jsonDecode(data);
    } on FormatException catch (e) {
      out.add(
        KanbanBoardEvent(
          const {},
          linkError: 'malformed relay frame: ${e.message}',
        ),
      );
      return;
    }
    final map = decoded is Map
        ? Map<String, dynamic>.from(decoded)
        : <String, dynamic>{};
    // C8 interim classification: progress-class placeholder. chat_message
    // NEVER arrives on this relay (the gateway is board-scoped), but if a
    // future frame claims it, drop it — the bridge must not leak chat.
    if (map['type'] == 'chat_message') return;
    out.add(KanbanBoardEvent(map));
  }

  void _handleEventDrop(StreamController<KanbanBoardEvent> out, Object error) {
    if (_eventsDisposed || out.isClosed) return;
    unawaited(_eventSocket?.close());
    _eventSocket = null;
    final message = error is _SocketClosed ? 'event socket closed' : '$error';
    if (_reconnectAttempts >= _maxReconnectAttempts) {
      out.add(
        KanbanBoardEvent(
          const {},
          linkError: 'board events unavailable: $message',
        ),
      );
      return;
    }
    _reconnectAttempts += 1;
    out.add(
      KanbanBoardEvent(
        const {},
        linkError:
            'board events reconnecting (attempt $_reconnectAttempts/$_maxReconnectAttempts): $message',
      ),
    );
    final delay = Duration(seconds: _reconnectAttempts * 2);
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(delay, () => _openEventSocket(out));
  }

  /// Close the event socket and end [boardEvents]. Call from provider
  /// onDispose; safe to call twice.
  Future<void> dispose() async {
    if (_eventsDisposed) return;
    _eventsDisposed = true;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    final controller = _eventsController;
    _eventsController = null;
    await _eventSocket?.close();
    _eventSocket = null;
    await controller?.close();
  }

  /// kIsWeb posture note (leaf doc checklist): this service needs no
  /// platform branch — HTTP rides Dio on both targets and WS auth rides the
  /// subprotocol lane the gateway understands on both. The guard exists so
  /// a future header-based native WS auth change cannot silently break web.
  static bool get supportsWebTransport => true;
  static bool get isWebRuntime => kIsWeb;

  // ------------------------------------------------------------------
  // HTTP plumbing + error mapping
  // ------------------------------------------------------------------

  Future<Map<String, dynamic>> _get(
    String path,
    Map<String, String> query,
  ) async {
    Response<dynamic> resp;
    try {
      resp = await _dio.get<dynamic>(path, queryParameters: query);
    } on DioException catch (e) {
      throw mapDioError(e);
    }
    return _checkedBody(resp, path);
  }

  Future<Map<String, dynamic>> _post(
    String path,
    Map<String, Object?> body,
  ) async {
    Response<dynamic> resp;
    try {
      resp = await _dio.post<dynamic>(path, data: body);
    } on DioException catch (e) {
      throw mapDioError(e);
    }
    return _checkedBody(resp, path);
  }

  Map<String, dynamic> _checkedBody(Response<dynamic> resp, String path) {
    final status = resp.statusCode ?? 0;
    final raw = _asObjectMap(resp.data);
    if (status >= 200 && status < 300) return raw;
    throw mapStatusError(status, raw, path);
  }

  /// The gateway forwards daemon reply bytes verbatim, so a success body is
  /// either a JSON object or (depending on 05/01) a bare array. Normalize to
  /// an object view; _listOf/_record handle the array form via {'items': …}.
  static Map<String, dynamic> _asObjectMap(Object? data) {
    if (data is Map) return Map<String, dynamic>.from(data);
    if (data is List) return {'items': data};
    if (data is String && data.isNotEmpty) {
      try {
        final decoded = jsonDecode(data);
        if (decoded is Map) return Map<String, dynamic>.from(decoded);
        if (decoded is List) return {'items': decoded};
      } on FormatException {
        // fall through
      }
    }
    return <String, dynamic>{};
  }

  /// Maps a transport DioException (no HTTP answer) to a typed exception.
  static KanbanApiException mapDioError(DioException e) {
    if (e.response != null) {
      // validateStatus caps errors at <500, so a DioException WITH a
      // response is a 5xx (502 upstream) or a timeout after headers.
      final body = _bodyOf(e.response!);
      if (e.response!.statusCode == 502) {
        return mapStatusError(502, body, e.requestOptions.path);
      }
      return KanbanConnectionException(
        _messageOf(
          body,
          e.message ?? 'gateway answered ${e.response!.statusCode}',
        ),
        statusCode: e.response!.statusCode,
      );
    }
    final reason = switch (e.type) {
      DioExceptionType.connectionTimeout ||
      DioExceptionType.sendTimeout ||
      DioExceptionType.receiveTimeout => 'sidecar request timed out',
      DioExceptionType.connectionError =>
        'cannot connect to the kanban sidecar',
      _ => e.message ?? 'transport failure',
    };
    return KanbanConnectionException(
      '$reason (${e.requestOptions.uri.host}:${e.requestOptions.uri.port})',
    );
  }

  /// Maps an HTTP error answer to the typed hierarchy. Table mirrors
  /// gateway.go: authorize() (401/403), serveAPI() (404/405/400),
  /// writeRPCError() (404/502 with the "rpc" key). Domain errors that
  /// surface inside a forwarded RPC message (lease held, WIP full, unknown
  /// card) are recognized by their Go Error() phrasing (kanban/types.go).
  static KanbanApiException mapStatusError(
    int status,
    Map<String, dynamic> body,
    String path,
  ) {
    final message = _messageOf(body, 'request failed ($status)');
    final rpc = body['rpc'] is String ? body['rpc'] as String : '';

    // Domain errors ride the forwarded message; recognize before status.
    final lease = _parseLeaseHeld(message);
    if (lease != null) return lease;
    final wip = _parseWipFull(message);
    if (wip != null) return wip;
    // ErrUnknownCard surfaces as 502 from writeRPCError ("not found"
    // matching aside), so bind the cardId wherever the phrasing appears.
    final cardMatch = message.indexOf('unknown card: ');
    if (cardMatch != -1) {
      return KanbanNotFoundException(
        message,
        cardId: message
            .substring(cardMatch + 'unknown card: '.length)
            .trim()
            .split(' ')
            .first,
        body: body,
      );
    }

    switch (status) {
      case 400:
        return KanbanBadRequestException(message, body: body);
      case 401:
        return KanbanUnauthorizedException(message, body: body);
      case 403:
        return KanbanForbiddenException(
          message,
          grant: _grantFromMessage(message),
          body: body,
        );
      case 404:
        return KanbanNotFoundException(
          message,
          cardId: _cardIdFromMessage(message),
          body: body,
        );
      case 405:
        return KanbanBadRequestException(
          'method not allowed for $path',
          body: body,
        );
      case 502:
        return KanbanUpstreamException(message, rpc: rpc, body: body);
      default:
        return KanbanApiException(message, statusCode: status, body: body);
    }
  }

  static Map<String, dynamic> _bodyOf(Response<dynamic> resp) {
    final data = resp.data;
    if (data is Map) return Map<String, dynamic>.from(data);
    if (data is String && data.isNotEmpty) {
      try {
        final decoded = jsonDecode(data);
        if (decoded is Map) return Map<String, dynamic>.from(decoded);
      } on FormatException {
        // fall through
      }
    }
    return <String, dynamic>{};
  }

  static String _messageOf(Map<String, dynamic> body, String fallback) {
    final m = body['error'];
    return m is String && m.isNotEmpty ? m : fallback;
  }

  /// "missing grant board.mutate" → the grant name (authorize() formats).
  static String _grantFromMessage(String message) {
    const prefix = 'missing grant ';
    if (message.startsWith(prefix)) {
      final rest = message.substring(prefix.length).trim();
      final space = rest.indexOf(' ');
      return space == -1 ? rest : rest.substring(0, space);
    }
    return '';
  }

  /// "unknown card: c-123" (ErrUnknownCard.Error()).
  static String _cardIdFromMessage(String message) {
    const prefix = 'unknown card: ';
    if (message.startsWith(prefix)) {
      return message.substring(prefix.length).trim();
    }
    return '';
  }

  /// ErrLeaseHeld.Error() = "lease held by <holder> since <RFC3339>"
  /// (wrapped messages keep that substring).
  static KanbanLeaseHeldException? _parseLeaseHeld(String message) {
    final match = RegExp(
      r'lease held by (\S+) since (\S+)',
    ).firstMatch(message);
    if (match == null) return null;
    return KanbanLeaseHeldException(
      message,
      holder: match.group(1)!,
      since: DateTime.tryParse(match.group(2) ?? ''),
    );
  }

  /// ErrWIPFull.Error() = "WIP limit reached for column <id>: <limit>".
  static KanbanWipFullException? _parseWipFull(String message) {
    final match = RegExp(
      r'WIP limit reached for column (\S+): (\d+)',
    ).firstMatch(message);
    if (match == null) return null;
    return KanbanWipFullException(
      message,
      columnId: match.group(1)!,
      limit: int.tryParse(match.group(2) ?? '') ?? 0,
    );
  }

  // ------------------------------------------------------------------
  // Reply-envelope tolerance
  // ------------------------------------------------------------------

  /// The gateway forwards the daemon RPC reply verbatim; the exact envelope
  /// keys are pinned when leaf 05/01 lands its handlers. Accept the two
  /// shapes the repo uses today ({"boards": [...]}-style map or a bare
  /// JSON array) and both naming conventions.
  static List<Map<String, dynamic>> _listOf(
    Map<String, dynamic> data,
    String key,
  ) {
    for (final candidate in <Object?>[
      data[key],
      data[_snakeToPascal(key)],
      data['items'],
      data['Items'],
      data, // a bare-map single record, upgraded below
    ]) {
      final items = candidate is List ? candidate : null;
      if (items != null) {
        return items
            .whereType<Map>()
            .map((e) => Map<String, dynamic>.from(e))
            .toList(growable: false);
      }
    }
    return const [];
  }

  /// Single-record reply: unwrap {key: {...}} envelopes, else use the map
  /// itself. [prefer] lists known envelope keys to strip when the payload
  /// is nested.
  static Map<String, dynamic> _record(
    Map<String, dynamic> data,
    List<String> prefer,
  ) {
    for (final key in prefer) {
      final inner = data[key];
      if (inner is Map) return Map<String, dynamic>.from(inner);
    }
    for (final inner in data.values) {
      if (inner is Map && data.length == 1)
        return Map<String, dynamic>.from(inner);
    }
    return data;
  }

  static String _snakeToPascal(String key) {
    final parts = key.split('_');
    return parts
        .map((p) => p.isEmpty ? p : p[0].toUpperCase() + p.substring(1))
        .join();
  }
}

class _SocketClosed implements Exception {
  const _SocketClosed();
}
