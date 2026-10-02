// E2E wire-shape proofs + smoke test.
//
// Task 1 proofs: the StubDaemon's responses carry the Go handlers' exact
// field names and SSE framing (copied from internal/comm/http — read-only
// reference). Any daemon wire change must update both together.
//
// Task 3 smoke proof: the REAL ChatProvider (no service mocks) submits
// against the stub and populates pendingTurns keyed by the stub's turn_id,
// with submitCount == 1.
//
// DO NOT import from production code — test/e2e only.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/services/sdk_client.dart' show ChatSubmitAck;

import 'pump_app.dart';
import 'stub_daemon.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  useRealHttp();

  // ------------------------------------------------------------------
  // Task 1: StubDaemon HTTP core — wire shapes pinned to the Go handlers
  // ------------------------------------------------------------------

  group('StubDaemon wire shapes (pinned to internal/comm/http)', () {
    late StubDaemon daemon;

    setUp(() async {
      daemon = await StubDaemon.start();
    });

    tearDown(() async {
      await daemon.dispose();
    });

    test(
      'GET /api/v1/sessions returns {"sessions": [...], "count": N}',
      () async {
        // Field names from handleSessionList (api_handlers.go): "sessions"
        // array + KeyCount ("count") = len.
        daemon.enqueueSessionList([
          {
            'id': 'session-e2e-1',
            'name': 'e2e session',
            'conversation_id': 'conv-e2e-1',
            'created_at': '2026-10-01T00:00:00Z',
            'archived': false,
          },
          {
            'id': 'session-e2e-2',
            'name': 'second',
            'conversation_id': 'conv-e2e-2',
            'created_at': '2026-10-01T00:01:00Z',
            'archived': false,
          },
        ]);

        final res = await httpGet('${daemon.baseUrl}/api/v1/sessions');
        expect(res.statusCode, 200);
        final body = jsonDecode(res.body) as Map<String, dynamic>;
        expect(body.keys, containsAll(<String>['sessions', 'count']));
        expect(body['count'], 2);
        final sessions = body['sessions'] as List;
        expect(sessions, hasLength(2));
        // Field names identical to the Go session service's JSON keys.
        final first = sessions.first as Map<String, dynamic>;
        expect(
          first.keys,
          containsAll(<String>[
            'id',
            'name',
            'conversation_id',
            'created_at',
            'archived',
          ]),
        );
      },
    );

    test('POST /api/v1/chat/submit returns the frozen ack envelope', () async {
      // Envelope from SubmitHandler.buildAck (rpc/chat_submit.go): frozen
      // key set turn_id, conversation_id, session_id, accepted, note.
      const turnId = 'turn-e2e-1';
      daemon.enqueueChatTurn(
        turnId: turnId,
        sessionId: 'e2e-session',
        events: const [
          StubSseEvent('agent_progress', {'stage': 'thinking'}),
          StubSseEvent('chat_message', {
            'content': 'final reply',
            'role': 'assistant',
          }),
        ],
      );

      final res = await httpPost(
        '${daemon.baseUrl}/api/v1/chat/submit',
        body: {
          'message': 'hello',
          'conversation_id': 'e2e-session',
          'source_client': 'flutter_ui',
        },
      );
      expect(res.statusCode, 200);
      final ack = jsonDecode(res.body) as Map<String, dynamic>;
      expect(
        ack.keys,
        containsAll(<String>[
          'turn_id',
          'conversation_id',
          'session_id',
          'accepted',
          'note',
        ]),
      );
      expect(ack['turn_id'], turnId); // stub mints the scripted turn id
      expect(ack['conversation_id'], 'e2e-session');
      expect(ack['session_id'], 'e2e-session');
      expect(ack['accepted'], true);
      expect(ack['note'], 'accepted; result arrives via turn.terminal');
      expect(daemon.submitCount, 1);
    });

    test(
      'POST /api/v1/chat/submit rejects an empty message like buildAck',
      () async {
        final res = await httpPost(
          '${daemon.baseUrl}/api/v1/chat/submit',
          body: {'message': '   ', 'conversation_id': 'e2e-session'},
        );
        expect(res.statusCode, 200);
        final ack = jsonDecode(res.body) as Map<String, dynamic>;
        expect(ack['turn_id'], '');
        expect(ack['accepted'], false);
        expect(ack['note'], 'message is required');
      },
    );

    test('GET /api/v1/chat/stream frames SSE like the Go SSEWriter', () async {
      // Framing from internal/comm/http/sse.go SendEvent: exactly
      // "event: <name>\ndata: <json>\n\n". The first frame is
      // `connected {"status":"ok"}` (handleChatStream). There is NO
      // terminal sentinel on this endpoint — the Go handler streams until
      // the client disconnects.
      const turnId = 'turn-e2e-2';
      daemon.enqueueChatTurn(
        turnId: turnId,
        sessionId: 'e2e-session',
        events: const [
          StubSseEvent('agent_progress', {'stage': 'thinking'}),
          StubSseEvent('chat_message', {
            'content': 'streamed reply',
            'role': 'assistant',
          }),
        ],
      );

      final socket = await Socket.connect(
        InternetAddress.loopbackIPv4,
        daemon.port,
      );
      const request =
          'GET /api/v1/chat/stream?turn_id=$turnId HTTP/1.1\r\n'
          'Host: 127.0.0.1\r\n'
          'Accept: text/event-stream\r\n'
          '\r\n';
      socket.add(ascii.encode(request));

      final completer = Completer<String>();
      final buffer = <String>[];
      late StreamSubscription<List<int>> sub;
      sub = socket.listen((chunk) {
        buffer.add(utf8.decode(chunk));
        final joined = buffer.join();
        // Stop as soon as both scripted events have fully arrived.
        if (joined.contains('streamed reply') && joined.contains('\n\n')) {
          if (!completer.isCompleted) completer.complete(joined);
          sub.cancel();
          socket.destroy();
        }
      });
      final raw = await completer.future.timeout(const Duration(seconds: 10));

      // Headers: the Go SSEWriter sets these (Connection is hop-by-hop and
      // owned by dart:io's server, so it is not asserted).
      expect(raw, contains('HTTP/1.1 200'));
      expect(raw.toLowerCase(), contains('content-type: text/event-stream'));
      expect(raw.toLowerCase(), contains('cache-control: no-cache'));

      // Body: per-frame BYTES must match the Go SSEWriter exactly. The
      // stub sends the scripted frames with an explicit Content-Length and
      // closes the response (documented deviation — see stub_daemon.dart);
      // the frame bytes are the pinned contract.
      final bodyStart = raw.indexOf('\r\n\r\n');
      expect(bodyStart, greaterThan(0));
      final body = raw.substring(bodyStart + 4);
      const turnIdJson = '"turn-e2e-2"';
      final expectedFrames = StringBuffer()
        ..write('event: connected\ndata: {"status":"ok"}\n\n')
        ..write(
          'event: agent_progress\ndata: {"stage":"thinking","turn_id":$turnIdJson,"session_id":"e2e-session"}\n\n',
        )
        ..write(
          'event: chat_message\ndata: {"content":"streamed reply","role":"assistant","turn_id":$turnIdJson,"session_id":"e2e-session"}\n\n',
        );
      expect(
        body,
        expectedFrames.toString(),
        reason:
            'SSE frame bytes must match the Go SSEWriter framing '
            '(event: <name>\\ndata: <json>\\n\\n) with the scripted '
            'turn_id/session_id injected; got: $body',
      );
    });

    test('GET /ws upgrades and speaks the {"type", "data"} envelope', () async {
      final ws = await WebSocket.connect('ws://127.0.0.1:${daemon.port}/ws');
      addTearDown(ws.close);

      // Server→client frames are read from the test's OWN socket stream
      // with ONE persistent listener collecting a queue (repeated
      // WebSocket.first would detach the single-subscription stream).
      final frames = <Map<String, dynamic>>[];
      final sub = ws.cast<String>().listen(
        (raw) => frames.add(jsonDecode(raw) as Map<String, dynamic>),
      );
      addTearDown(sub.cancel);

      Future<Map<String, dynamic>> nextFrame() async {
        final deadline = DateTime.now().add(const Duration(seconds: 5));
        while (frames.isEmpty) {
          if (DateTime.now().isAfter(deadline)) {
            fail('timed out waiting for the next WS frame');
          }
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }
        return frames.removeAt(0);
      }

      // First frame: handleWebSocket's welcome frame.
      final welcome = await nextFrame();
      expect(welcome['type'], 'status');
      expect((welcome['data'] as Map)['connected'], true);

      // ping -> pong (handleWSMessage).
      ws.add(jsonEncode({'type': 'ping'}));
      final pong = await nextFrame();
      expect(pong['type'], 'pong');

      // subscribe -> subscribed ack, session recorded. The ack's channel
      // is 'all' for the client's FLAT subscribe frame — byte-parity with
      // the real daemon's handleWSSubscribe, which parses only the
      // {type,data} envelope and defaults an unparsed channel to "all"
      // (the client-side stream `where` clauses own session routing).
      ws.add(
        jsonEncode({
          'type': 'subscribe',
          'channel': 'chat',
          'session_id': 'e2e-session',
        }),
      );
      final subscribed = await nextFrame();
      expect(subscribed['type'], 'subscribed');
      expect((subscribed['data'] as Map)['channel'], 'all');

      // A wrapped {type, data:{channel,...}} frame DOES parse (the Go
      // handler's envelope path).
      ws.add(
        jsonEncode({
          'type': 'subscribe',
          'data': {'channel': 'chat', 'session_id': 'e2e-session'},
        }),
      );
      final wrappedAck = await nextFrame();
      expect(wrappedAck['type'], 'subscribed');
      expect((wrappedAck['data'] as Map)['channel'], 'chat');
      expect(daemon.wsChatSubscriptions, contains('e2e-session'));

      // Event broadcast uses the transformBusEventToWS envelope.
      daemon.broadcastWsEvent(
        const StubSseEvent('agent_progress', {
          'stage': 'thinking',
          'session_id': 'e2e-session',
        }),
      );
      final eventFrame = await nextFrame();
      expect(eventFrame['type'], 'agent_progress');
      final dataField = eventFrame['data'] as Map;
      expect(dataField['stage'], 'thinking');
      expect(dataField['type'], 'agent_progress');
    });

    test('GET /api/v1/sessions/{id}/messages returns messages+total', () async {
      // handleSessionMessages shape: guaranteed non-null "messages" array
      // and "total" = the session's full count.
      final res = await httpGet(
        '${daemon.baseUrl}/api/v1/sessions/e2e-session/messages?offset=0&limit=200',
      );
      expect(res.statusCode, 200);
      final body = jsonDecode(res.body) as Map<String, dynamic>;
      expect(body.keys, containsAll(<String>['messages', 'total']));
      expect(body['messages'], isA<List>());
      expect(body['total'], 0);
    });

    test('GET /api/v1/config/client returns {"content": ...}', () async {
      // handleGetClientConfig shape.
      final res = await httpGet('${daemon.baseUrl}/api/v1/config/client');
      expect(res.statusCode, 200);
      final body = jsonDecode(res.body) as Map<String, dynamic>;
      expect(body.keys, contains('content'));
      expect(body['content'], isA<String>());
    });

    test('binds loopback on an ephemeral port and disposes cleanly', () async {
      expect(daemon.port, greaterThanOrEqualTo(49152));
      expect(daemon.baseUrl, startsWith('http://127.0.0.1:'));
      // Disposing twice is safe; the port is released afterwards.
      await daemon.dispose();
      await daemon.dispose();
    });
  });

  // ------------------------------------------------------------------
  // Task 3: smoke proof — real provider against the stub
  // ------------------------------------------------------------------

  group('real ChatProvider smoke (pumpRealApp)', () {
    late StubDaemon daemon;

    setUp(() async {
      daemon = await StubDaemon.start();
    });

    tearDown(() async {
      await daemon.dispose();
    });

    testWidgets(
      'a real submit populates pendingTurns keyed by the stub turn_id '
      'with submitCount == 1',
      (tester) async {
        const turnId = 'turn-e2e-smoke';
        const sessionId = 'e2e-session';
        daemon.enqueueChatTurn(
          turnId: turnId,
          sessionId: sessionId,
          events: const [
            StubSseEvent('agent_progress', {'stage': 'thinking'}),
            StubSseEvent('agent_progress', {
              'conversation_id': sessionId,
              'handler_case': 'direct_reply',
              'status': 'completed',
              'reply': 'smoke reply',
              'duration_ms': 12,
            }),
          ],
        );

        late ProviderContainer container;
        final wsFrames = <Map<String, dynamic>>[];
        await tester.runAsync(() async {
          (container, _) = pumpRealApp(
            tester,
            daemon: daemon,
            sessionId: sessionId,
          );
          final sub = daemon.wsFrames().listen(wsFrames.add);
          addTearDown(sub.cancel);
        });
        addTearDown(container.dispose);
        await tester.pump();

        // The real ChatNotifier connected to the stub's WebSocket and sent
        // its subscribe frame for this session before the submit. (The
        // client's subscribe frames are FLAT — {type, channel, session_id}
        // without a data envelope, identical to what it sends the real
        // daemon; the daemon's session filter for these lives in the
        // client-side stream `where` clauses, not handleWSSubscribe.)
        bool subscribedSeen() => wsFrames.any(
          (f) =>
              f['type'] == 'subscribe' &&
              f['channel'] == 'chat' &&
              f['session_id'] == sessionId,
        );
        await tester.runAsync(() async {
          final deadline = DateTime.now().add(const Duration(seconds: 10));
          while (DateTime.now().isBefore(deadline)) {
            if (subscribedSeen()) break;
            await Future<void>.delayed(const Duration(milliseconds: 25));
          }
        });
        expect(
          subscribedSeen(),
          isTrue,
          reason:
              'the real WebSocketService must be connected to the stub '
              'and subscribed to the session before the submit; frames seen: '
              '${wsFrames.map((f) => f['type']).toList()}',
        );

        // Submit through the REAL ChatNotifier (async submit endpoint).
        ChatSubmitAck ack = const ChatSubmitAck(
          turnId: '',
          conversationId: '',
          sessionId: '',
          accepted: false,
        );
        await tester.runAsync(() async {
          ack = await container
              .read(chatProvider(sessionId).notifier)
              .submitTurn(sessionId: sessionId, text: 'smoke message');
        });
        await tester.pump();

        expect(ack.accepted, isTrue, reason: 'stub ack: ${ack.note}');
        expect(ack.turnId, turnId);

        // pendingTurns gains an entry keyed by the STUB's turn_id.
        expect(
          container.read(chatProvider(sessionId)).pendingTurns.keys,
          contains(turnId),
        );
        final pending = container
            .read(chatProvider(sessionId))
            .pendingTurns[turnId]!;
        expect(
          pending.status,
          anyOf(PendingTurnStatus.pending, PendingTurnStatus.progress),
        );
        expect(daemon.submitCount, 1);
      },
    );
  });
}

// --------------------------------------------------------------------
// tiny http helpers (dart:io, no package:http dependency)
// --------------------------------------------------------------------

/// Plain http result (status line + drained body), returned by the test
/// helpers below.
class HttpStubResponse {
  final int statusCode;
  final String body;
  const HttpStubResponse(this.statusCode, this.body);
}

Future<HttpStubResponse> httpGet(String url) async {
  final client = HttpClient();
  try {
    final req = await client.getUrl(Uri.parse(url));
    final res = await req.close();
    final body = await utf8.decodeStream(res);
    return HttpStubResponse(res.statusCode, body);
  } finally {
    client.close(force: true);
  }
}

Future<HttpStubResponse> httpPost(
  String url, {
  required Map<String, dynamic> body,
}) async {
  final client = HttpClient();
  try {
    final req = await client.postUrl(Uri.parse(url));
    req.headers.contentType = ContentType.json;
    req.write(jsonEncode(body));
    final res = await req.close();
    final bodyText = await utf8.decodeStream(res);
    return HttpStubResponse(res.statusCode, bodyText);
  } finally {
    client.close(force: true);
  }
}
