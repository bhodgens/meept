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

/// One raw WS client the stub accepted:
///   - the ACCEPT INDEX (the identity the stub's session filter is keyed
///     by — the client-side socket a test holds is a different object from
///     the server-side socket a subscribe frame arms),
///   - the client socket,
///   - the frames received ON that socket (server→client), and
///   - a live count of frames that socket has SENT (client→server), used
///     as a per-connection delivery fence: waiting on the daemon-wide
///     `wsFrames()` queue instead would match another connection's frame.
typedef WsClient = (
  int,
  WebSocket,
  List<Map<String, dynamic>>,
  List<Map<String, dynamic>>,
);

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

      // subscribe -> subscribed ack, session recorded. The ack echoes the
      // channel the client actually sent: handleWSSubscribe reads
      // channel/session_id from the {type,data} envelope FIRST and only
      // then falls back to the WSMessage top-level `channel`/`session_id`
      // flat aliases (internal/comm/http/server.go:2595-2611; those aliases
      // were added by 5073c159 "WS subscribe accepts the Flutter client's
      // flat frame shape"). So the FLAT frame below acks channel 'chat'
      // and arms this connection's session filter — before 5073c159 the
      // daemon ignored flat fields and answered 'all'.
      ws.add(
        jsonEncode({
          'type': 'subscribe',
          'channel': 'chat',
          'session_id': 'e2e-session',
        }),
      );
      final subscribed = await nextFrame();
      expect(subscribed['type'], 'subscribed');
      expect((subscribed['data'] as Map)['channel'], 'chat');
      // The flat frame armed THIS connection's session filter (accept
      // index 0 — the only /ws connection this test opened).
      expect(daemon.sessionFilterOfConnection(0), {'e2e-session'});

      // A wrapped {type, data:{channel,...}} frame parses on the same
      // terms (the Go handler's envelope path) and is idempotent here.
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

    // ------------------------------------------------------------------
    // Per-connection session filter (parity with WebSocketHub).
    //
    // The stub used to fan every event out to every socket, which made
    // cross-session suppression STRUCTURALLY untestable: no gui-flows
    // scenario could ever prove the client ignores another session's
    // turn, because the stub never suppressed anything. The real relay
    // (internal/comm/http/server.go:599-627) keeps a connection only when
    // the event carries no session id or ShouldSendProgress(wc, id) says
    // yes — nil filter = broadcast, otherwise strict membership
    // (server.go:472-481). These tests pin that rule on the stub.
    //
    // Connections are addressed by ACCEPT INDEX, not by socket object:
    // the server-side socket a subscribe frame arms belongs to the
    // stub's accept loop, while a test holds the client's end of the
    // same pipe — two distinct objects that never compare equal.
    // ------------------------------------------------------------------
    group('StubDaemon per-connection session filter (ShouldSendProgress)', () {
      late StubDaemon daemon;

      setUp(() async {
        daemon = await StubDaemon.start();
      });

      tearDown(() async {
        await daemon.dispose();
      });

      /// Open one raw WS client at the next accept index, collecting its
      /// inbound (server→client) frames and its outbound (client→server)
      /// frames.
      Future<WsClient> openClient() async {
        final index = daemon.wsConnectionCount;
        final ws = await WebSocket.connect('ws://127.0.0.1:${daemon.port}/ws');
        addTearDown(ws.close);
        final frames = <Map<String, dynamic>>[];
        final outbound = <Map<String, dynamic>>[];
        final sub = ws.cast<String>().listen(
          (raw) => frames.add(jsonDecode(raw) as Map<String, dynamic>),
          onError: (_) {},
        );
        addTearDown(sub.cancel);
        // Drain the welcome frame so later assertions count only events.
        await _waitFrames(frames, (f) => f['type'] == 'status');
        // The stub mirrors every inbound frame onto wsFrames(); that queue is
        // daemon-wide, so this per-client subscription records only what
        // THIS connection sent.
        final mirror = daemon.wsFrames().listen((f) {
          if (f['type'] != 'pong') outbound.add(f);
        });
        addTearDown(mirror.cancel);
        return (index, ws, frames, outbound);
      }

      /// Subscribe connection [index] to [sessionId] with the client's
      /// FLAT frame shape ({type, channel, session_id}, no data envelope)
      /// and wait for that socket's own `subscribed` ack. The ack is sent
      /// AFTER the filter is armed on both sides (the Go handler arms
      /// before replying), so an ack receipt means "this connection is now
      /// filtered" — and it is per-connection, so a re-subscribe cannot
      /// match the previous ack still sitting in the list.
      Future<void> subscribeFlat(
        int index,
        WebSocket ws,
        List<Map<String, dynamic>> frames,
        List<Map<String, dynamic>> outbound,
        String sessionId, {
        String channel = 'chat',
      }) async {
        final sent = outbound.where((f) => f['type'] == 'subscribe').length;
        ws.add(
          jsonEncode({
            'type': 'subscribe',
            'channel': channel,
            'session_id': sessionId,
          }),
        );
        await _waitCount(outbound, 'subscribe', sent + 1);
        await _waitFrames(frames, (f) => f['type'] == 'subscribed');
        expect(
          daemon.shouldSendProgress(index, 'never-subscribed'),
          isFalse,
          reason: 'precondition: connection $index must be armed',
        );
      }

      test('an UNARMED connection is in broadcast mode (no filter = every '
          'session), like the Go nil-map rule', () async {
        final (index, ws, frames, outbound) = await openClient();

        expect(
          daemon.sessionFilterOfConnection(index),
          isNull,
          reason: 'a connection that never subscribed arms no filter',
        );
        expect(daemon.shouldSendProgress(index, 'any-session'), isTrue);
        expect(daemon.shouldSendProgress(index, 'another-session'), isTrue);

        daemon.broadcastWsEvent(
          const StubSseEvent('agent_progress', {
            'stage': 'thinking',
            'session_id': 'any-session',
          }),
        );
        await _waitFrames(frames, (f) => f['type'] == 'agent_progress');
      });

      test('an ARMED connection receives ONLY its own session; another '
          "session's event is suppressed (the cross-session pin)", () async {
        final (myIndex, myWs, myFrames, myOut) = await openClient();
        final (theirIndex, theirWs, theirFrames, theirOut) = await openClient();

        await subscribeFlat(myIndex, myWs, myFrames, myOut, 'session-mine');
        await subscribeFlat(
          theirIndex,
          theirWs,
          theirFrames,
          theirOut,
          'session-theirs',
        );

        // Both filters armed, each with exactly one session.
        expect(daemon.sessionFilterOfConnection(myIndex), {'session-mine'});
        expect(daemon.sessionFilterOfConnection(theirIndex), {
          'session-theirs',
        });
        expect(daemon.shouldSendProgress(myIndex, 'session-mine'), isTrue);
        expect(
          daemon.shouldSendProgress(myIndex, 'session-theirs'),
          isFalse,
          reason: 'strict membership — my connection is not in their set',
        );

        // Count only EVENT frames from here on: the per-connection frame
        // lists still hold each socket's own `subscribed` ack, which is a
        // control frame, not a relayed event.
        int events(List<Map<String, dynamic>> fs) =>
            fs.where((f) => f['type'] == 'agent_progress').length;

        // An event for THEIR session must reach only them.
        daemon.broadcastWsEvent(
          const StubSseEvent('agent_progress', {
            'stage': 'thinking',
            'session_id': 'session-theirs',
          }),
        );
        await _waitFrames(theirFrames, (f) => f['type'] == 'agent_progress');
        expect(
          events(myFrames),
          0,
          reason:
              'cross-session suppression: a connection that subscribed to '
              'session-mine must NOT receive session-theirs progress '
              '(the real daemon drops it at server.go:623)',
        );

        // The mirror image: MY event reaches me and not them.
        daemon.broadcastWsEvent(
          const StubSseEvent('agent_progress', {
            'stage': 'planning',
            'session_id': 'session-mine',
          }),
        );
        await _waitFrames(myFrames, (f) => f['type'] == 'agent_progress');
        expect(
          events(theirFrames),
          1,
          reason:
              'suppression is symmetric — their connection still holds only '
              'the ONE event for their own session',
        );
      });

      test('conversation_id is the filter key when session_id is absent '
          '(agent.progress / tool.execution.progress shape)', () async {
        final (index, ws, frames, outbound) = await openClient();
        await subscribeFlat(index, ws, frames, outbound, 'conv-only');

        expect(daemon.shouldSendProgress(index, 'conv-only'), isTrue);
        expect(daemon.shouldSendProgress(index, 'other-conv'), isFalse);

        daemon.broadcastWsEvent(
          const StubSseEvent('agent_progress', {
            'stage': 'tool',
            'conversation_id': 'conv-only',
          }),
        );
        await _waitFrames(frames, (f) => f['type'] == 'agent_progress');
      });

      test('an event with NO session id reaches every connection (backward '
          'compat: nothing to filter on)', () async {
        final (myIndex, myWs, myFrames, myOut) = await openClient();
        final (theirIndex, theirWs, theirFrames, theirOut) = await openClient();
        await subscribeFlat(myIndex, myWs, myFrames, myOut, 'session-mine');
        await subscribeFlat(
          theirIndex,
          theirWs,
          theirFrames,
          theirOut,
          'session-theirs',
        );

        daemon.broadcastWsEvent(
          const StubSseEvent('agent_progress', {'stage': 'daemon-wide'}),
        );
        await _waitFrames(myFrames, (f) => f['type'] == 'agent_progress');
        await _waitFrames(theirFrames, (f) => f['type'] == 'agent_progress');
      });

      test('unsubscribing records a CHANNEL-SCOPED opt-out: the session '
          'stays granted, and a channel-less unsubscribe suppresses '
          'every channel (UnsubscribeSession server.go:561-579 + '
          'ShouldSend server.go:612-621)', () async {
        final (index, ws, frames, outbound) = await openClient();
        await subscribeFlat(index, ws, frames, outbound, 'session-mine');
        expect(daemon.shouldSendProgress(index, 'session-mine'), isTrue);

        // The client's own release frame carries BOTH channel and
        // session_id (websocket_service.dart _releaseSessionChannel).
        ws.add(
          jsonEncode({
            'type': 'unsubscribe',
            'channel': 'chat',
            'session_id': 'session-mine',
          }),
        );
        await _waitCount(outbound, 'unsubscribe', 1);
        await Future<void>.delayed(const Duration(milliseconds: 100));

        // The grant is untouched — unsubscribe is an opt-OUT, not a
        // revocation (server.go:578 records only the suppression).
        expect(daemon.sessionFilterOfConnection(index), {'session-mine'});
        // chat_message is chat-scoped, so the chat unsubscribe stops it.
        expect(
          daemon.shouldSend(index, 'chat_message', 'session-mine'),
          isFalse,
        );
        // agent_progress is progress-scoped and keeps delivering.
        expect(daemon.shouldSendProgress(index, 'session-mine'), isTrue);

        // A channel-LESS unsubscribe is the connection-wide opt-out.
        ws.add(
          jsonEncode({'type': 'unsubscribe', 'session_id': 'session-mine'}),
        );
        await _waitCount(outbound, 'unsubscribe', 2);
        await Future<void>.delayed(const Duration(milliseconds: 100));
        expect(
          daemon.shouldSendProgress(index, 'session-mine'),
          isFalse,
          reason: 'the catch-all suppression covers every channel',
        );
        expect(
          daemon.shouldSend(index, 'chat_message', 'session-mine'),
          isFalse,
        );

        // Re-subscribing is an opt-IN that clears the suppression.
        await subscribeFlat(index, ws, frames, outbound, 'session-mine');
        expect(
          daemon.shouldSendProgress(index, 'session-mine'),
          isTrue,
          reason: "SubscribeSession clears that session's suppressions",
        );
      });

      test('an unsubscribe from a connection that NEVER subscribed still '
          'suppresses (the entry is created, not skipped — the '
          'least-surprise opt-out, server.go:568-574)', () async {
        final (index, ws, frames, outbound) = await openClient();

        // Precondition: unarmed = broadcast.
        expect(daemon.shouldSendProgress(index, 'session-mine'), isTrue);

        ws.add(
          jsonEncode({'type': 'unsubscribe', 'session_id': 'session-mine'}),
        );
        await _waitCount(outbound, 'unsubscribe', 1);
        await Future<void>.delayed(const Duration(milliseconds: 100));

        expect(
          daemon.sessionFilterOfConnection(index),
          isEmpty,
          reason:
              'the grant set is empty — suppression alone still suppresses, '
              'because ShouldSend rejects a session outside .sessions before '
              'it ever consults .suppressed',
        );
        expect(daemon.shouldSendProgress(index, 'session-mine'), isFalse);
      });

      test('a scripted turn reaches its own session and is suppressed for a '
          'different subscribed session (end-to-end relay parity)', () async {
        final (myIndex, myWs, myFrames, myOut) = await openClient();
        final (theirIndex, theirWs, theirFrames, theirOut) = await openClient();
        await subscribeFlat(myIndex, myWs, myFrames, myOut, 'session-mine');
        await subscribeFlat(
          theirIndex,
          theirWs,
          theirFrames,
          theirOut,
          'session-theirs',
        );

        daemon.enqueueChatTurn(
          turnId: 'turn-filter-01',
          sessionId: 'session-mine',
          events: const [
            StubSseEvent('agent_progress', {'stage': 'thinking'}),
          ],
        );
        final res = await httpPost(
          '${daemon.baseUrl}/api/v1/chat/submit',
          body: {
            'message': 'hello',
            'conversation_id': 'session-mine',
            'source_client': 'flutter_ui',
          },
        );
        expect(res.statusCode, 200);

        await _waitFrames(myFrames, (f) => f['type'] == 'agent_progress');
        expect(
          theirFrames.where((f) => f['type'] == 'agent_progress'),
          isEmpty,
          reason:
              'the scripted turn belongs to session-mine; a connection '
              'filtered to session-theirs must not see it',
        );
      });
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
        // daemon. That shape arms the stub's per-connection session filter
        // through the same top-level aliases handleWSSubscribe reads
        // (server.go:2595-2611), so a turn's own frames reach this socket
        // while another session's are suppressed.)
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

/// Bounded wait until [frames] holds a frame satisfying [match].
///
/// Raw socket frames arrive on the stub's real event loop, so a bare
/// `expect` right after a `daemon.broadcastWsEvent(...)` would race the
/// write. A miss fails with the frames actually seen rather than timing
/// out silently.
Future<void> _waitFrames(
  List<Map<String, dynamic>> frames,
  bool Function(Map<String, dynamic>) match,
) async {
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (DateTime.now().isBefore(deadline)) {
    if (frames.any(match)) return;
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  fail(
    'timed out waiting for a matching WS frame; frames seen: '
    '${frames.map((f) => f['type']).toList()}',
  );
}

/// Bounded wait until [outbound] holds at least [count] frames of type
/// [type].
///
/// Counting, not matching: a re-subscribe must not be satisfied by the
/// PREVIOUS ack still sitting in the list, and the daemon-wide
/// `wsFrames()` queue would happily match another connection's frame. The
/// caller passes the list it has already seen, so the fence is per-frame.
Future<void> _waitCount(
  List<Map<String, dynamic>> outbound,
  String type,
  int count,
) async {
  int seen() => outbound.where((f) => f['type'] == type).length;
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (DateTime.now().isBefore(deadline)) {
    if (seen() >= count) return;
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  fail(
    'timed out waiting for #$count inbound "$type" frame(s); saw '
    '${outbound.map((f) => f['type']).toList()}',
  );
}

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
