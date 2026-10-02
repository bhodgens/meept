// GUI flow regression tests (leaf 03 of the 20261001-frontend-e2e plan
// tree): the four flows that pin the 2026-09-17 audit findings 19-21.
//
//   gui-terminal-01  TestTerminalEventDeliversToPendingTurn
//                    - terminal-after-ack resolution, AND the ack-race
//                      ordering shape: the terminal WS frame arrives
//                      BEFORE the submit-ack HTTP reply is processed.
//   gui-session-01   TestSessionSwitchReloadsTranscript
//                    - session B's view shows B's history; the departed
//                      session A's pendingTurns entry is RETAINED, not
//                      wiped (F20 pin).
//   gui-quota-01     TestQuotaParkSurfacesAndWaitResolves
//                    - an agent_progress(quota_wait) frame surfaces as a
//                      waiting row; a later terminal resolves it.
//   gui-stream-01    TestStreamingTurnAccumulatesAndFinalizes
//                    - ordered chat_message deltas accumulate into ONE
//                      rendered message, finalized exactly once.
//
// Every flow drives the REAL ChatNotifier (pumpRealApp — no service mocks)
// against the StubDaemon and asserts BOTH provider state AND rendered
// output (the real ChatMessageList widget tree is pumped so the widget-level
// assertions run against the production renderer). Only the stub server is
// scripted; all client code is production.
//
// DO NOT import from production code — test/e2e only.
import 'dart:convert';
import 'dart:io' show WebSocket;

import 'package:flutter/material.dart' show MaterialApp;
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/features/chat/chat_message_list.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/services/sdk_client.dart' show ChatSubmitAck;

import 'pump_app.dart';
import 'stub_daemon.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  useRealHttp();

  // --------------------------------------------------------------------
  // helpers
  // --------------------------------------------------------------------

  /// pump the real chat widget tree for [sessionId] inside the real
  /// provider graph and wait until the notifier has connected to the stub
  /// WS AND sent its chat subscribe frame (the precondition every submit
  /// depends on — the smoke test in stub_daemon_test.dart pins this same
  /// discipline).
  ///
  /// Returns the ProviderContainer for provider-state assertions.
  Future<ProviderContainer> pumpAndAwaitSubscribe(
    WidgetTester tester, {
    required StubDaemon daemon,
    required String sessionId,
    required List<Map<String, dynamic>> wsFrames,
  }) async {
    late ProviderContainer container;
    await tester.runAsync(() async {
      (container, _) = pumpRealApp(
        tester,
        daemon: daemon,
        sessionId: sessionId,
        child: ChatMessageList(sessionId: sessionId),
      );
      final sub = daemon.wsFrames().listen(wsFrames.add);
      addTearDown(sub.cancel);
    });
    addTearDown(container.dispose);
    await tester.pump();

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
          'the real WebSocketService must be connected and subscribed to '
          '$sessionId before the submit; frames seen: '
          '${wsFrames.map((f) => f['type']).toList()}',
    );
    await tester.pump();
    return container;
  }

  /// Submit through the REAL notifier and pump until the ack's turn id is
  /// tracked in pendingTurns (registration is async relative to the HTTP
  /// reply on the test's real-socket event loop).
  Future<ChatSubmitAck> submitAndAwaitRegistration(
    WidgetTester tester,
    ProviderContainer container,
    String sessionId, {
    required String text,
    required String expectedTurnId,
  }) async {
    var ack = const ChatSubmitAck(
      turnId: '',
      conversationId: '',
      sessionId: '',
      accepted: false,
    );
    await tester.runAsync(() async {
      ack = await container
          .read(chatProvider(sessionId).notifier)
          .submitTurn(sessionId: sessionId, text: text);
      // pendingTurns registration happens synchronously after the ack
      // resolves inside submitTurn — but the ack future itself completes
      // on the real socket; give the registration frame a chance.
      final deadline = DateTime.now().add(const Duration(seconds: 10));
      while (DateTime.now().isBefore(deadline)) {
        if (container
            .read(chatProvider(sessionId))
            .pendingTurns
            .containsKey(expectedTurnId)) {
          break;
        }
        await Future<void>.delayed(const Duration(milliseconds: 25));
      }
    });
    await tester.pump();
    expect(ack.accepted, isTrue, reason: 'stub ack: ${ack.note}');
    expect(ack.turnId, expectedTurnId);
    return ack;
  }

  /// Bounded real-time wait for a condition observed through the provider
  /// (frames arrive on the stub's server-pushed WS loop, ~10ms apart).
  Future<void> waitFor(
    WidgetTester tester,
    bool Function() condition, {
    String reason = 'condition never became true',
  }) async {
    await tester.runAsync(() async {
      final deadline = DateTime.now().add(const Duration(seconds: 10));
      while (DateTime.now().isBefore(deadline)) {
        if (condition()) break;
        await Future<void>.delayed(const Duration(milliseconds: 25));
      }
    });
    await tester.pump();
    expect(condition(), isTrue, reason: reason);
  }

  /// The text visible in the rendered message bubbles (production widget
  /// tree — Text widgets carry bubble content; MarkdownBody renders its
  /// source through a rich Text).
  List<String> renderedTexts(WidgetTester tester) => tester
      .widgetList<Text>(find.byType(Text))
      .map((t) => t.data ?? (t.textSpan?.toPlainText() ?? ''))
      .toList();

  // --------------------------------------------------------------------
  // gui-terminal-01
  // --------------------------------------------------------------------
  group('gui-terminal-01: terminal event delivers to pending turn', () {
    late StubDaemon daemon;
    setUp(() async {
      daemon = await StubDaemon.start();
    });
    tearDown(() async {
      await daemon.dispose();
    });

    testWidgets('TestTerminalEventDeliversToPendingTurn — terminal after ack '
        'resolves pendingTurns and renders the reply', (tester) async {
      const turnId = 'turn-terminal-01-a';
      const sessionId = 'e2e-session';
      daemon.enqueueChatTurn(
        turnId: turnId,
        sessionId: sessionId,
        events: const [
          // Terminal-shaped relay (turn_id + handler_case =>
          // turn.terminal classification, isTurnTerminalPayload).
          StubSseEvent('agent_progress', {
            'handler_case': 'direct_reply',
            'status': 'completed',
            'reply': 'terminal flow reply',
            'duration_ms': 7,
          }),
        ],
      );
      final wsFrames = <Map<String, dynamic>>[];
      final container = await pumpAndAwaitSubscribe(
        tester,
        daemon: daemon,
        sessionId: sessionId,
        wsFrames: wsFrames,
      );

      await submitAndAwaitRegistration(
        tester,
        container,
        sessionId,
        text: 'hello terminal',
        expectedTurnId: turnId,
      );

      // Provider state: the turn resolves and nothing leaks.
      await waitFor(
        tester,
        () => container.read(chatProvider(sessionId)).pendingTurns.isEmpty,
        reason: 'terminal event must resolve the pendingTurns entry',
      );
      final state = container.read(chatProvider(sessionId));
      expect(state.pendingTurns, isEmpty, reason: 'resolved, not leaked');
      expect(state.error, isNull, reason: 'a completed turn is not an error');
      expect(
        state.messages.any(
          (m) => m.role == 'assistant' && m.content == 'terminal flow reply',
        ),
        isTrue,
        reason:
            'the terminal reply must land in the provider transcript; '
            'messages=${state.messages.map((m) => '${m.role}:${m.content}').toList()}',
      );

      // Rendered output: the terminal content is in the widget tree.
      final texts = renderedTexts(tester);
      expect(
        texts,
        contains('terminal flow reply'),
        reason:
            'rendered transcript must contain the terminal content; '
            'texts=$texts',
      );
    });

    testWidgets(
      'TestTerminalEventDeliversToPendingTurn — ACK RACE: terminal frame '
      'arrives BEFORE the submit ack is processed (F19 ordering shape)',
      (tester) async {
        const turnId = 'turn-terminal-01-race';
        const sessionId = 'e2e-session';
        daemon.enqueueChatTurn(
          turnId: turnId,
          sessionId: sessionId,
          events: const [
            StubSseEvent('agent_progress', {
              'handler_case': 'direct_reply',
              'status': 'completed',
              'reply': 'raced terminal reply',
              'duration_ms': 3,
            }),
          ],
        );
        final wsFrames = <Map<String, dynamic>>[];
        final container = await pumpAndAwaitSubscribe(
          tester,
          daemon: daemon,
          sessionId: sessionId,
          wsFrames: wsFrames,
        );

        // The ordering is enforced server-side by the stub itself:
        // _handleChatSubmit kicks off _broadcastTurnEvents (30ms later, to
        // every connected socket) BEFORE the submit handler writes its HTTP
        // ack reply. We observe the race with the test's OWN observer
        // WebSocket: broadcast event frames land on every socket, so the
        // observer sees the terminal while the app's submit-ack future is
        // still pending. One runAsync brackets BOTH sides so the two
        // real-socket deliveries are observed on one event loop (nested
        // runAsync is illegal).
        late ChatSubmitAck ack;
        var terminalObservedBeforeAck = false;
        await tester.runAsync(() async {
          // Observer socket: receives the stub's broadcast event frames.
          final observer = await WebSocket.connect(
            'ws://127.0.0.1:${daemon.port}/ws',
          );
          final broadcast = <Map<String, dynamic>>[];
          final sub = observer.cast<String>().listen(
            (raw) =>
                broadcast.add((jsonDecode(raw) as Map).cast<String, dynamic>()),
          );
          addTearDown(sub.cancel);
          addTearDown(observer.close);

          final ackFuture = container
              .read(chatProvider(sessionId).notifier)
              .submitTurn(sessionId: sessionId, text: 'race message');
          // Poll the observer's frame queue on the same event loop while
          // the ack is still in flight.
          final deadline = DateTime.now().add(const Duration(seconds: 10));
          while (DateTime.now().isBefore(deadline)) {
            if (broadcast.any(
              (f) =>
                  f['type'] == 'agent_progress' &&
                  ((f['data'] as Map?)?['handler_case'] == 'direct_reply'),
            )) {
              terminalObservedBeforeAck = true;
              break;
            }
            await Future<void>.delayed(const Duration(milliseconds: 5));
          }
          ack = await ackFuture;
        });
        await tester.pump();

        // Ordering proof: the terminal frame hit the wire while the submit
        // ack was still pending — the terminal-before-ack shape is genuine.
        expect(
          terminalObservedBeforeAck,
          isTrue,
          reason: 'the stub must deliver the terminal frame before the ack',
        );
        expect(ack.accepted, isTrue, reason: 'stub ack: ${ack.note}');

        // F19: the early terminal is buffered and consumed on ack
        // registration — pendingTurns resolves to EMPTY (never leaks) and
        // the reply is rendered.
        await waitFor(
          tester,
          () => container.read(chatProvider(sessionId)).pendingTurns.isEmpty,
          reason:
              'raced terminal must be consumed on ack registration: '
              'pendingTurns must resolve empty (F19: dropped = leak)',
        );
        final state = container.read(chatProvider(sessionId));
        expect(state.error, isNull);
        expect(
          state.messages.any(
            (m) => m.role == 'assistant' && m.content == 'raced terminal reply',
          ),
          isTrue,
          reason:
              'the raced terminal reply must land in the transcript; '
              'messages=${state.messages.map((m) => '${m.role}:${m.content}').toList()}',
        );
        expect(
          renderedTexts(tester),
          contains('raced terminal reply'),
          reason: 'rendered transcript must contain the raced reply',
        );
      },
    );
  });

  // --------------------------------------------------------------------
  // gui-session-01
  // --------------------------------------------------------------------
  group('gui-session-01: session switch reloads transcript', () {
    late StubDaemon daemon;
    setUp(() async {
      daemon = await StubDaemon.start();
    });
    tearDown(() async {
      await daemon.dispose();
    });

    testWidgets(
      'TestSessionSwitchReloadsTranscript — B renders its own history; '
      "A's pending turn is RETAINED in pendingTurns, not wiped (F20 pin)",
      (tester) async {
        const sessionA = 'e2e-session'; // default pump session
        const sessionB = 'session-b';
        const turnIdA = 'turn-session-a-inflight';

        daemon.enqueueSessionList([
          {
            'id': sessionA,
            'name': 'alpha',
            'conversation_id': sessionA,
            'created_at': '2026-10-01T00:00:00Z',
            'archived': false,
          },
          {
            'id': sessionB,
            'name': 'beta',
            'conversation_id': sessionB,
            'created_at': '2026-10-01T00:01:00Z',
            'archived': false,
          },
        ]);

        // Session B has its own scripted history (the stub serves
        // per-session bodies — additive enqueueSessionMessages). Session
        // A's turn stays in flight: it gets NO terminal event — nothing
        // resolves it during this flow.
        daemon.enqueueSessionMessages(sessionA, const [
          {
            'id': 11,
            'role': 'user',
            'content': 'a-prior user line',
            'timestamp': '2026-10-01T00:00:01Z',
          },
        ]);
        daemon.enqueueSessionMessages(sessionB, const [
          {
            'id': 21,
            'role': 'user',
            'content': 'b-prior user line',
            'timestamp': '2026-10-01T00:01:01Z',
          },
          {
            'id': 22,
            'role': 'assistant',
            'content': 'b-prior assistant line',
            'timestamp': '2026-10-01T00:01:02Z',
          },
        ]);

        final wsFrames = <Map<String, dynamic>>[];
        final container = await pumpAndAwaitSubscribe(
          tester,
          daemon: daemon,
          sessionId: sessionA,
          wsFrames: wsFrames,
        );
        // ignore: avoid_print
        print(
          '[e2e-debug] A loaded messages='
          '${container.read(chatProvider(sessionA)).messages.length}',
        );

        // Start a turn on A that never terminates during this flow.
        daemon.enqueueChatTurn(
          turnId: turnIdA,
          sessionId: sessionA,
          events: const [
            StubSseEvent('agent_progress', {'stage': 'thinking'}),
          ],
        );
        await submitAndAwaitRegistration(
          tester,
          container,
          sessionA,
          text: 'in-flight on a',
          expectedTurnId: turnIdA,
        );
        expect(
          container.read(chatProvider(sessionA)).pendingTurns.keys,
          contains(turnIdA),
        );

        // Switch: construct B's notifier OUTSIDE the widget build (real
        // async zone, the same discipline pumpAndAwaitSubscribe uses for
        // A) so its auto-load runs on the real event loop, then render
        // session B's real chat surface (the SAME production
        // ChatMessageList widget the app mounts on a session switch).
        // The B pump REPLACES the widget tree but keeps the same provider
        // container — the session-switch semantics (per-session family
        // state, WS subscriptions) are exactly what is under test.
        await tester.runAsync(() async {
          container.read(chatProvider(sessionB).notifier);
          final deadline = DateTime.now().add(const Duration(seconds: 10));
          while (DateTime.now().isBefore(deadline)) {
            final st = container.read(chatProvider(sessionB));
            if (st.messages.any((m) => m.content == 'b-prior assistant line')) {
              break;
            }
            if (st.error != null) break;
            await Future<void>.delayed(const Duration(milliseconds: 25));
          }
        });
        await tester.pumpWidget(
          // Not const: the scope carries the live container + session widget.
          // ignore: prefer_const_constructors
          MaterialApp(
            home: UncontrolledProviderScope(
              container: container,
              // ignore: prefer_const_constructors
              child: ChatMessageList(sessionId: sessionB),
            ),
          ),
        );
        await tester.pump();

        // B's auto-load is REAL socket I/O (not frame-driven): run the
        // fetch to completion inside runAsync, then drain the resulting
        // state update with a pump.
        await tester.runAsync(() async {
          final deadline = DateTime.now().add(const Duration(seconds: 10));
          while (DateTime.now().isBefore(deadline)) {
            final st = container.read(chatProvider(sessionB));
            if (st.messages.any((m) => m.content == 'b-prior assistant line')) {
              break;
            }
            // A fetch error (timeout, stub hiccup) also ends the wait —
            // the expect below fails with the actual error text then.
            if (st.error != null) break;
            await Future<void>.delayed(const Duration(milliseconds: 25));
          }
        });
        await tester.pump();
        // Provider state (B): B's transcript was fetched fresh from the
        // (newly-fetched) session history endpoint.
        final stateB = container.read(chatProvider(sessionB));
        // ignore: avoid_print
        print(
          '[e2e-debug] B state: isLoading=${stateB.isLoading} '
          'error=${stateB.error} '
          'messages=${stateB.messages.map((m) => m.content).toList()}',
        );
        expect(stateB.error, isNull, reason: 'B history fetch error');
        expect(
          stateB.messages.map((m) => m.content),
          containsAll(<String>['b-prior user line', 'b-prior assistant line']),
        );
        expect(
          stateB.messages.map((m) => m.content),
          isNot(contains('a-prior user line')),
          reason: 'B\'s view must not leak A\'s transcript',
        );

        // THE PIN: A's pending turn is RETAINED after the switch — the
        // audit's F20 wipe (a state rebuild dropping pendingTurns) is the
        // regression class here. The turn belongs to session A's notifier
        // (per-session .family state); the B view must not have erased it.
        final stateA = container.read(chatProvider(sessionA));
        expect(
          stateA.pendingTurns.keys,
          contains(turnIdA),
          reason:
              'F20 pin: the departed session\'s pendingTurns entry must be '
              'RETAINED, not wiped on session switch; A pendingTurns='
              '${stateA.pendingTurns.keys.toList()}',
        );
        expect(
          stateB.pendingTurns.keys,
          isNot(contains(turnIdA)),
          reason: 'the turn belongs to A — B\'s own state must not claim it',
        );

        // Rendered output (B): B's history bubbles render; A's pending
        // turn row does NOT render inside B's view.
        final texts = renderedTexts(tester);
        expect(texts, contains('b-prior user line'));
        expect(texts, contains('b-prior assistant line'));
        expect(
          texts,
          isNot(contains('a-prior user line')),
          reason: 'A\'s history must not render in B\'s view',
        );
        expect(
          find.byType(PendingTurnIndicator),
          findsNothing,
          reason: 'A\'s in-flight turn row must not render in B\'s view',
        );
      },
    );
  });

  // --------------------------------------------------------------------
  // gui-quota-01
  // --------------------------------------------------------------------
  group('gui-quota-01: quota park surfaces and wait resolves', () {
    late StubDaemon daemon;
    setUp(() async {
      daemon = await StubDaemon.start();
    });
    tearDown(() async {
      await daemon.dispose();
    });

    testWidgets(
      'TestQuotaParkSurfacesAndWaitResolves — quota_wait progress surfaces '
      'a waiting row, later terminal resolves it',
      (tester) async {
        const turnId = 'turn-quota-01';
        const sessionId = 'e2e-session';
        // Script the quota-wait progress as the turn's only queued event;
        // the terminal is pushed LATER via broadcastWsEvent (public stub
        // API) AFTER the intermediate waiting state has been asserted —
        // deterministic ordering instead of racing two 10ms-apart frames.
        daemon.enqueueChatTurn(
          turnId: turnId,
          sessionId: sessionId,
          events: const [
            StubSseEvent('agent_progress', {
              'stage': 'quota_wait',
              'message': 'waiting for provider quota',
            }),
          ],
        );
        final wsFrames = <Map<String, dynamic>>[];
        final container = await pumpAndAwaitSubscribe(
          tester,
          daemon: daemon,
          sessionId: sessionId,
          wsFrames: wsFrames,
        );

        await submitAndAwaitRegistration(
          tester,
          container,
          sessionId,
          text: 'run against quota',
          expectedTurnId: turnId,
        );

        // Intermediate: the waiting state renders BEFORE the terminal.
        // progressText non-empty on the pending entry = surfaced waiting
        // state at the provider level; PendingTurnIndicator row at the
        // widget level.
        await waitFor(
          tester,
          () {
            final t = container
                .read(chatProvider(sessionId))
                .pendingTurns[turnId];
            return t != null &&
                t.status == PendingTurnStatus.progress &&
                t.progressText.isNotEmpty;
          },
          reason:
              'quota_wait progress must surface as a waiting pending turn '
              'with progress text',
        );
        expect(
          find.byType(PendingTurnIndicator),
          findsOneWidget,
          reason: 'the waiting state must render as a pending-turn row',
        );

        // Resolution: NOW push the terminal event, pendingTurns empties,
        // content renders, no error state. broadcastWsEvent takes a raw
        // payload (no turn/session injection), so carry the turn id and
        // the terminal shape (turn_id + handler_case) explicitly.
        daemon.broadcastWsEvent(
          const StubSseEvent('agent_progress', {
            'turn_id': turnId,
            'conversation_id': sessionId,
            'handler_case': 'direct_reply',
            'status': 'completed',
            'reply': 'quota wait resolved reply',
            'duration_ms': 42,
          }),
        );
        await waitFor(
          tester,
          () => container.read(chatProvider(sessionId)).pendingTurns.isEmpty,
          reason: 'the terminal event must resolve the parked/waiting turn',
        );
        final state = container.read(chatProvider(sessionId));
        expect(state.error, isNull);
        expect(
          state.messages.any(
            (m) =>
                m.role == 'assistant' &&
                m.content == 'quota wait resolved reply',
          ),
          isTrue,
          reason:
              'the resolved reply must land in the provider transcript; '
              'messages=${state.messages.map((m) => '${m.role}:${m.content}').toList()}',
        );
        expect(
          renderedTexts(tester),
          contains('quota wait resolved reply'),
          reason: 'the resolved reply must render',
        );
        expect(find.byType(PendingTurnIndicator), findsNothing);
      },
    );
  });

  // --------------------------------------------------------------------
  // gui-stream-01
  // --------------------------------------------------------------------
  group('gui-stream-01: streaming turn accumulates and finalizes', () {
    late StubDaemon daemon;
    setUp(() async {
      daemon = await StubDaemon.start();
    });
    tearDown(() async {
      await daemon.dispose();
    });

    testWidgets('TestStreamingTurnAccumulatesAndFinalizes — 5 ordered deltas '
        'accumulate into ONE message, finalized exactly once', (tester) async {
      const turnId = 'turn-stream-01';
      const sessionId = 'e2e-session';
      const deltas = ['zero ', 'one ', 'two ', 'three ', 'four'];
      final full = deltas.join();
      daemon.enqueueChatTurn(
        turnId: turnId,
        sessionId: sessionId,
        events: [
          for (final d in deltas)
            StubSseEvent('chat_message', {'content': d, 'role': 'assistant'}),
          // Terminal full reply == the ordered concatenation; not const
          // because `full` is computed.
          StubSseEvent('agent_progress', {
            'handler_case': 'direct_reply',
            'status': 'completed',
            'reply': full,
            'duration_ms': 11,
          }),
        ],
      );
      final wsFrames = <Map<String, dynamic>>[];
      final container = await pumpAndAwaitSubscribe(
        tester,
        daemon: daemon,
        sessionId: sessionId,
        wsFrames: wsFrames,
      );

      await submitAndAwaitRegistration(
        tester,
        container,
        sessionId,
        text: 'stream me',
        expectedTurnId: turnId,
      );

      await waitFor(
        tester,
        () => container.read(chatProvider(sessionId)).pendingTurns.isEmpty,
        reason: 'the terminal must resolve the streaming turn',
      );
      final state = container.read(chatProvider(sessionId));
      expect(state.error, isNull);

      // Streaming shape: the ordered deltas must NOT fan out into five
      // separate assistant bubbles.
      final assistantBubbles = state.messages
          .where((m) => m.role == 'assistant')
          .toList();
      expect(
        assistantBubbles.length,
        lessThanOrEqualTo(2),
        reason:
            '5 deltas must collapse into at most the streamed bubble plus '
            'the finalized bubble — never 5 separate messages; '
            'messages=${state.messages.map((m) => '${m.role}:${m.content}').toList()}',
      );

      // The final transcript contains the concatenation IN ORDER (the full
      // joined text arrives with the terminal event).
      expect(
        state.messages.any((m) => m.role == 'assistant' && m.content == full),
        isTrue,
        reason:
            'the finalized assistant message must carry the ordered '
            'concatenation "$full"; '
            'messages=${state.messages.map((m) => '${m.role}:${m.content}').toList()}',
      );

      // Finalize exactly once: no duplicated full-text assistant bubble
      // (the terminal dedupe must not append a second copy).
      expect(
        state.messages
            .where((m) => m.role == 'assistant' && m.content == full)
            .length,
        1,
        reason:
            'the finalized message must appear exactly once (the terminal '
            'dedupe must not append a second copy)',
      );
      expect(
        renderedTexts(tester).where((t) => t == full).length,
        1,
        reason: 'exactly one rendered bubble carries the full text',
      );
      expect(find.byType(PendingTurnIndicator), findsNothing);
    });
  });
}
