// E2E-only stub daemon: an in-process HTTP(+WS) server that speaks the
// meept daemon's wire shapes. Field names and framing are copied from the
// Go handlers in internal/comm/http (read-only reference) — that parity is
// the entire point of this stub:
//
//   POST /api/v1/chat/submit   -> ack envelope from SubmitHandler.buildAck
//                                 (internal/rpc/chat_submit.go): frozen key
//                                 set turn_id, conversation_id, session_id,
//                                 accepted, note.
//   GET  /api/v1/sessions      -> {"sessions": [...], "count": N}
//                                 (handleSessionList; KeyCount = "count")
//   GET  /api/v1/sessions/{id}/messages -> {"messages": [], "total": N}
//                                 (handleSessionMessages; total = session
//                                 full count, not page length)
//   GET  /api/v1/config/client -> {"content": "..."} (handleGetClientConfig)
//   GET  /api/v1/status        -> {"status": "ok"}
//   GET  /api/v1/chat/stream   -> SSE, framing from internal/comm/http/sse.go:
//                                 "event: <name>\ndata: <json>\n\n", an
//                                 initial `connected {"status":"ok"}` frame
//                                 and `: heartbeat\n\n` comments. The Go
//                                 handler sends NO terminal sentinel on this
//                                 endpoint — it streams until the client
//                                 disconnects — so the stub does the same:
//                                 after the scripted frames the connection
//                                 stays open (the test client closes it).
//   GET  /ws                   -> WebSocket upgrade (handleWebSocket): a
//                                 `status {"connected":true}` welcome frame,
//                                 `pong` replies, `subscribed` acks, and
//                                 `{type, data}` event frames shaped like
//                                 transformBusEventToWS relays. Frames are
//                                 broadcast to every socket; the client's own
//                                 subscription filters do the session routing
//                                 exactly as they do against the real daemon.
//
// DO NOT import from production code — test/e2e only.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// Restore real socket I/O under the flutter_test binding.
///
/// TestWidgetsFlutterBinding installs `HttpOverrides.global =
/// _MockHttpOverrides()` (every HttpClient returns an empty 400 and
/// WebSocket upgrades fail with "Mocked response"). The override is
/// process-global, not zone-local, so tester.runAsync alone cannot bypass
/// it. Each `flutter test` file runs in its own isolate, so clearing the
/// global here affects ONLY this e2e isolate — the rest of the suite keeps
/// the mock. Call once at the top of every e2e main(), after
/// TestWidgetsFlutterBinding.ensureInitialized().
void useRealHttp() {
  HttpOverrides.global = null;
}

/// One scripted server-pushed event frame.
///
/// [type] is the wire event name: the SSE `event:` line name, or the WS
/// envelope `type` (transformBusEventToWS classification: `chat_message` or
/// `agent_progress`). [payload] is the event body — the daemon's bus payload
/// keys (turn_id, session_id / conversation_id, content, status, reply,
/// handler_case, ...). Per the leaf contract the payload may carry its own
/// `type` key; it must match [type] and the WS serve path normalizes it.
class StubSseEvent {
  final String type;
  final Map<String, dynamic> payload;

  const StubSseEvent(this.type, this.payload);
}

class _ScriptedTurn {
  final String turnId;
  final String sessionId;
  final List<StubSseEvent> events;

  _ScriptedTurn({
    required this.turnId,
    required this.sessionId,
    required this.events,
  });
}

/// In-process stub of the meept daemon's HTTP/WS surface.
///
/// Binds 127.0.0.1 on an ephemeral port; serves ONLY the endpoints the gui
/// flows need. Wire shapes are pinned in stub_daemon_test.dart.
class StubDaemon {
  HttpServer? _server;
  final List<_ScriptedTurn> _queuedTurns = [];
  final Map<String, int> _submitCountsByTurn = {};
  final List<Map<String, dynamic>> _submittedBodies = [];
  List<Map<String, dynamic>> _sessions = const [];

  /// Per-session scripted bodies for GET /api/v1/sessions/{id}/messages.
  /// Unscripted sessions fall back to the empty-history shape
  /// ({messages: [], total: 0}) pinned in stub_daemon_test.dart.
  final Map<String, ({List<Map<String, dynamic>> messages, int total})>
  _sessionMessages = {};
  int _submitCount = 0;
  final Set<WebSocket> _wsSockets = {};
  final Set<String> _wsChatSubscriptions = {};
  bool _disposed = false;

  StubDaemon._();

  /// Start the stub on 127.0.0.1 with an ephemeral port.
  static Future<StubDaemon> start() async {
    final daemon = StubDaemon._();
    daemon._server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    unawaited(daemon._serve());
    return daemon;
  }

  /// http://127.0.0.1:<ephemeral>
  String get baseUrl {
    final s = _server;
    if (s == null) {
      throw StateError('StubDaemon.start() was never called');
    }
    return 'http://${s.address.host}:${s.port}';
  }

  int get port => _server?.port ?? 0;

  /// Number of POST /api/v1/chat/submit calls received.
  int get submitCount => _submitCount;

  /// Bodies received on the submit endpoint (parsed JSON).
  List<Map<String, dynamic>> get submittedBodies =>
      List.unmodifiable(_submittedBodies);

  /// Session ids seen in `subscribe` frames on the /ws channel.
  Set<String> get wsChatSubscriptions => Set.unmodifiable(_wsChatSubscriptions);

  /// Script one turn: the submit endpoint will mint exactly [turnId], and the
  /// stream/socket endpoints will push [events] for it.
  void enqueueChatTurn({
    required String turnId,
    String sessionId = 'e2e-session',
    required List<StubSseEvent> events,
  }) {
    _queuedTurns.add(
      _ScriptedTurn(turnId: turnId, sessionId: sessionId, events: events),
    );
  }

  /// Script the GET /api/v1/sessions list body (array of raw session maps —
  /// field names must already match the Go session service's JSON keys).
  void enqueueSessionList(List<Map<String, dynamic>> sessions) {
    _sessions = List.of(sessions);
  }

  /// Script the GET /api/v1/sessions/{id}/messages body for one session.
  ///
  /// [messages] is the array of raw session.Message maps (field names must
  /// already match the Go session service's JSON keys); [total] mirrors
  /// handleSessionMessages' semantics — the session's FULL count, not the
  /// page length — and defaults to the array length.
  void enqueueSessionMessages(
    String sessionId,
    List<Map<String, dynamic>> messages, {
    int? total,
  }) {
    _sessionMessages[sessionId] = (
      messages: List.of(messages),
      total: total ?? messages.length,
    );
  }

  /// Push [event] to every connected WebSocket client immediately
  /// (transformBusEventToWS envelope: {"type": <type>, "data": <payload>}).
  void broadcastWsEvent(StubSseEvent event) {
    final data = Map<String, dynamic>.from(event.payload);
    data['type'] = event.type; // client flatten() re-promotes it to the top
    final frame = jsonEncode({'type': event.type, 'data': data});
    for (final socket in List.of(_wsSockets)) {
      try {
        socket.add(frame);
      } catch (_) {
        _wsSockets.remove(socket);
      }
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    for (final socket in _wsSockets) {
      try {
        await socket.close();
      } catch (_) {}
    }
    _wsSockets.clear();
    for (final inbound in _wsInbound) {
      if (!inbound.isClosed) await inbound.close();
    }
    _wsInbound.clear();
    await _server?.close(force: true);
    _server = null;
  }

  // ------------------------------------------------------------------
  // Routing
  // ------------------------------------------------------------------

  Future<void> _serve() async {
    final server = _server;
    if (server == null) return;
    try {
      await for (final req in server) {
        if (_disposed) break;
        unawaited(_route(req));
      }
    } catch (_) {
      // Server force-closed during dispose — expected.
    }
  }

  Future<void> _route(HttpRequest req) async {
    final path = req.uri.path;
    try {
      if (req.method == 'GET' && path == '/ws') {
        return await _handleWs(req);
      }
      if (req.method == 'GET' && path == '/api/v1/sessions') {
        return _writeJson(req, 200, {
          'sessions': _sessions,
          'count': _sessions.length, // KeyCount
        });
      }
      if (req.method == 'POST' && path == '/api/v1/chat/submit') {
        return await _handleChatSubmit(req);
      }
      if (req.method == 'GET' && path == '/api/v1/chat/stream') {
        return await _handleChatStream(req);
      }
      final messagesMatch = RegExp(
        r'^/api/v1/sessions/([^/]+)/messages$',
      ).firstMatch(path);
      if (req.method == 'GET' && messagesMatch != null) {
        // handleSessionMessages shape: messages guaranteed non-null array,
        // total = the session's FULL count (not the page length).
        final scripted = _sessionMessages[messagesMatch.group(1)];
        return _writeJson(req, 200, {
          'messages': scripted?.messages ?? <Map<String, dynamic>>[],
          'total': scripted?.total ?? 0,
        });
      }
      if (req.method == 'GET' && path == '/api/v1/config/client') {
        // handleGetClientConfig: {"content": <json5 text>}
        return _writeJson(req, 200, {'content': ''});
      }
      if (req.method == 'GET' && path == '/api/v1/status') {
        return _writeJson(req, 200, {'status': 'ok'});
      }
      _writeJson(req, 404, {
        'error': 'stub daemon: unhandled $req.method $path',
      });
    } catch (e) {
      try {
        _writeJson(req, 500, {'error': '$e'});
      } catch (_) {}
    }
  }

  void _writeJson(HttpRequest req, int status, Object body) {
    req.response.statusCode = status;
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode(body));
    req.response.close();
  }

  // ------------------------------------------------------------------
  // POST /api/v1/chat/submit — SubmitHandler.buildAck envelope
  // ------------------------------------------------------------------

  Future<void> _handleChatSubmit(HttpRequest req) async {
    final raw = await utf8.decoder.bind(req).join();
    Map<String, dynamic> body = {};
    if (raw.isNotEmpty) {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        body = decoded.map((k, v) => MapEntry('$k', v));
      }
    }
    _submitCount++;
    _submittedBodies.add(body);

    final message = (body['message'] as String?) ?? '';
    if (message.trim().isEmpty) {
      // buildAck validation path: empty turn_id + rejection note.
      return _writeJson(req, 200, {
        'turn_id': '',
        'conversation_id': body['conversation_id'] ?? '',
        'session_id': body['session_id'] ?? '',
        'accepted': false,
        'note': 'message is required',
      });
    }

    final turn = _queuedTurns.isNotEmpty ? _queuedTurns.removeAt(0) : null;
    final turnId = turn?.turnId ?? (body['turn_id'] as String? ?? '');
    if (turnId.isEmpty) {
      return _writeJson(req, 200, {
        'turn_id': '',
        'conversation_id': body['conversation_id'] ?? '',
        'session_id': body['session_id'] ?? '',
        'accepted': false,
        'note': 'stub daemon: no scripted turn queued',
      });
    }
    _submitCountsByTurn[turnId] = (_submitCountsByTurn[turnId] ?? 0) + 1;
    final fallbackSessionId = turn?.sessionId ?? 'e2e-session';

    // Conversation-id resolution: the daemon resolves the session's own
    // conversation id when only session_id is passed (buildAck). The stub
    // echoes the client's conversation_id, falling back to session_id.
    final conversationId =
        (body['conversation_id'] as String?)?.isNotEmpty == true
        ? body['conversation_id'] as String
        : ((body['session_id'] as String?) ?? fallbackSessionId);

    // Frozen ack key set (leaf 04, internal/rpc/chat_submit.go buildAck):
    // turn_id, conversation_id, session_id, accepted, note.
    if (turn != null) {
      unawaited(_broadcastTurnEvents(turn, overrideTurnId: turnId));
    }
    return _writeJson(req, 200, {
      'turn_id': turnId,
      'conversation_id': conversationId,
      'session_id': body['session_id'] ?? conversationId,
      'accepted': true,
      'note': 'accepted; result arrives via turn.terminal',
    });
  }

  /// Push a scripted turn's events to every connected WS client, shaped like
  /// the daemon's bus->WS relay.
  Future<void> _broadcastTurnEvents(
    _ScriptedTurn turn, {
    String? overrideTurnId,
  }) async {
    // Give the submit reply a chance to reach the client before the event
    // frames land (the real daemon's terminal arrives well after the ack).
    await Future<void>.delayed(const Duration(milliseconds: 30));
    final turnId = overrideTurnId ?? turn.turnId;
    for (final event in turn.events) {
      final payload = Map<String, dynamic>.from(event.payload);
      payload['turn_id'] = turnId;
      payload['session_id'] = turn.sessionId;
      payload['type'] = event.type;
      broadcastWsEvent(StubSseEvent(event.type, payload));
      await Future<void>.delayed(const Duration(milliseconds: 10));
    }
  }

  // ------------------------------------------------------------------
  // GET /api/v1/chat/stream — SSE (internal/comm/http/sse.go framing)
  // ------------------------------------------------------------------

  Future<void> _handleChatStream(HttpRequest req) async {
    final turnId = req.uri.queryParameters['turn_id'];

    // Build the full body first, then send it with an explicit
    // Content-Length. DEVIATION from the Go handler (documented): with
    // chunked transfer encoding dart:io's HttpResponse does not push
    // body bytes on flush() — frames only reach the client when the
    // response closes. The Go SSEWriter streams live via http.Flusher
    // (small writes flush immediately); this stub serves the scripted
    // frames as one self-terminating stream instead. Per-frame BYTES are
    // Go-identical: "event: <name>\ndata: <json>\n\n" (sse.go SendEvent),
    // leading `connected {"status":"ok"}` frame (handleChatStream). No
    // app code reads this endpoint (the GUI consumes the WS relay), so
    // the framing bytes are what the shape contract pins.
    final body = StringBuffer();
    void sendEvent(String event, Object data) {
      // SendEvent: "event: <name>\ndata: <json>\n\n"
      body.write('event: $event\ndata: ${jsonEncode(data)}\n\n');
    }

    // Initial connection event (handleChatStream).
    sendEvent('connected', {'status': 'ok'});

    final turns = _queuedTurns.toList();
    if (turnId != null) {
      turns.retainWhere((t) => t.turnId == turnId);
    }
    for (final turn in turns) {
      for (final event in turn.events) {
        final payload = Map<String, dynamic>.from(event.payload);
        payload['turn_id'] = turn.turnId;
        payload['session_id'] = turn.sessionId;
        sendEvent(event.type, payload);
      }
    }

    final payload = body.toString();
    req.response.headers.set('Content-Type', 'text/event-stream');
    req.response.headers.set('Cache-Control', 'no-cache');
    req.response.statusCode = 200;
    req.response.headers.set(
      'Content-Length',
      utf8.encode(payload).length.toString(),
    );
    req.response.write(payload);
    await req.response.close();
  }

  // ------------------------------------------------------------------
  // GET /ws — WebSocket upgrade (handleWebSocket)
  // ------------------------------------------------------------------

  Future<void> _handleWs(HttpRequest req) async {
    final socket = await WebSocketTransformer.upgrade(req);
    _wsSockets.add(socket);
    // Broadcast mirror of inbound frames (see wsInboundMessages).
    final inbound = StreamController<Map<String, dynamic>>.broadcast();
    _wsInbound.add(inbound);
    inbound.stream.listen(_wsFrameQueue.add);
    // handleWebSocket welcome frame: status {"connected":true}.
    socket.add(
      jsonEncode({
        'type': 'status',
        'data': {'connected': true},
      }),
    );
    socket.listen(
      (data) {
        if (data is! String) return;
        Map<String, dynamic> msg = {};
        try {
          final decoded = jsonDecode(data);
          if (decoded is Map) {
            msg = decoded.map((k, v) => MapEntry('$k', v));
          }
        } catch (_) {
          return;
        }
        if (!inbound.isClosed) inbound.add(msg);
        final dataField = msg['data'];
        final payload = dataField is Map
            ? dataField.map((k, v) => MapEntry('$k', v))
            : <String, dynamic>{};
        switch (msg['type']) {
          case 'ping':
            socket.add(jsonEncode({'type': 'pong'}));
          case 'subscribe':
            final channel = payload['channel'] ?? 'all';
            socket.add(
              jsonEncode({
                'type': 'subscribed',
                'data': {'channel': channel},
              }),
            );
            final sid = payload['session_id'];
            if (sid is String && sid.isNotEmpty) {
              _wsChatSubscriptions.add(sid);
            }
          case 'unsubscribe':
            final sid = payload['session_id'];
            if (sid is String && sid.isNotEmpty) {
              _wsChatSubscriptions.remove(sid);
            }
          default:
            break;
        }
      },
      onDone: () {
        _wsSockets.remove(socket);
        inbound.close();
        _wsInbound.remove(inbound);
      },
      onError: (_) {
        _wsSockets.remove(socket);
        inbound.close();
        _wsInbound.remove(inbound);
      },
      cancelOnError: true,
    );
  }

  /// Broadcast mirrors of every connected socket's inbound frames.
  final Set<StreamController<Map<String, dynamic>>> _wsInbound = {};

  final StreamController<Map<String, dynamic>> _wsFrameQueue =
      StreamController<Map<String, dynamic>>.broadcast();

  /// Ordered stream of every inbound WS frame (welcome acks, client-sent
  /// frames, broadcast echoes). The raw socket stream is single-subscription
  /// and server-owned; tests read this re-listenable queue instead
  /// ([Stream.first] on the socket itself would detach the server-side
  /// listener).
  Stream<Map<String, dynamic>> wsFrames() => _wsFrameQueue.stream;
}
