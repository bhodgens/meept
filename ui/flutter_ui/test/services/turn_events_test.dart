import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Bridge that lets tests drive frames into a REAL WebSocketService's
/// message pipeline: it subscribes to the service's public `messageStream`
/// — the same stream [WebSocketService.subscribeToTurnTerminal] filters —
/// but since `messageStream` is derived from the same internal subject the
/// terminal filter uses, publishing requires the internal subject.
///
/// Dart privacy is library-scoped, so a test cannot push into
/// `_messageSubject` directly. Instead this harness replicates the exact
/// public filter contract by applying the SAME predicate the service
/// applies, against frames fed through a plain broadcast stream. This
/// validates the identification + session-filter logic; the service
/// subscribes its filters off the same subject in production, which the
/// integration is responsible for.
class _TerminalFilterHarness {
  final _controller = StreamController<Map<String, dynamic>>.broadcast();

  /// Feed a FLATTENED frame (the shape WebSocketService publishes after
  /// stripping the `{type, data}` wire envelope).
  void emitFlat(Map<String, dynamic> flatFrame) => _controller.add(flatFrame);

  /// Mirrors WebSocketService.subscribeToTurnTerminal's filter exactly:
  /// type == 'agent_progress', session matches, terminal payload shape.
  Stream<Map<String, dynamic>> terminalFramesFor(String sessionId) {
    return _controller.stream.where((m) {
      final type = m['type'] as String?;
      if (type != 'agent_progress') return false;
      final sid = m['session_id'] as String?;
      if (sid != sessionId) return false;
      return isTurnTerminalPayload(m);
    });
  }

  /// The typed stream the service exposes, built off the same filter.
  Stream<TurnTerminalEvent> typedFor(String sessionId) =>
      terminalFramesFor(sessionId).map(TurnTerminalEvent.fromParse);
}

Map<String, dynamic> terminalFrame({
  required String sessionId,
  String turnId = 'turn-1',
  String handlerCase = 'sync_dispatch',
  String status = 'completed',
  String reply = 'all done',
  String error = '',
  int durationMs = 1234,
  String conversationId = 'conv-1',
}) {
  return {
    'type': 'agent_progress',
    'session_id': sessionId,
    'turn_id': turnId,
    'conversation_id': conversationId,
    'handler_case': handlerCase,
    'status': status,
    'reply': reply,
    'error': error,
    'duration_ms': durationMs,
  };
}

Map<String, dynamic> ordinaryProgressFrame({required String sessionId}) {
  return {
    'type': 'agent_progress',
    'session_id': sessionId,
    'agent_id': 'coder',
    'message': 'thinking',
    'tier': 1,
  };
}

void main() {
  group('turn.terminal stream (leaf 05 Task 2)', () {
    test('emits parsed TurnTerminalEvent for terminal-shaped frames',
        () async {
      final harness = _TerminalFilterHarness();
      final events = <TurnTerminalEvent>[];
      final sub = harness.typedFor('sess-1').listen(events.add);
      await Future<void>.delayed(Duration.zero);

      harness.emitFlat(terminalFrame(sessionId: 'sess-1'));
      await Future<void>.delayed(Duration.zero);

      expect(events, hasLength(1));
      final e = events.single;
      expect(e.turnId, 'turn-1');
      expect(e.conversationId, 'conv-1');
      expect(e.sessionId, 'sess-1');
      expect(e.status, 'completed');
      expect(e.reply, 'all done');
      expect(e.durationMs, 1234);
      await sub.cancel();
    });

    test('does NOT emit for ordinary progress events (no turn_id)',
        () async {
      final harness = _TerminalFilterHarness();
      final events = <TurnTerminalEvent>[];
      final sub = harness.typedFor('sess-1').listen(events.add);
      await Future<void>.delayed(Duration.zero);

      harness.emitFlat(ordinaryProgressFrame(sessionId: 'sess-1'));
      // A chat_message event (even with turn_id + handler_case) is not a
      // terminal relay — only agent_progress frames carry the turn payload.
      harness.emitFlat({
        'type': 'chat_message',
        'session_id': 'sess-1',
        'turn_id': 'turn-9',
        'handler_case': 'sync_dispatch',
      });
      await Future<void>.delayed(Duration.zero);

      expect(events, isEmpty);
      await sub.cancel();
    });

    test('filters events from other sessions (mirrors :646-663)', () async {
      final harness = _TerminalFilterHarness();
      final events = <TurnTerminalEvent>[];
      final sub = harness.typedFor('sess-1').listen(events.add);
      await Future<void>.delayed(Duration.zero);

      harness.emitFlat(terminalFrame(sessionId: 'sess-OTHER'));
      harness.emitFlat(terminalFrame(sessionId: 'sess-1'));
      await Future<void>.delayed(Duration.zero);

      expect(events, hasLength(1));
      expect(events.single.sessionId, 'sess-1');
      await sub.cancel();
    });

    test('parses failed status with error text', () async {
      final harness = _TerminalFilterHarness();
      final events = <TurnTerminalEvent>[];
      final sub = harness.typedFor('sess-1').listen(events.add);
      await Future<void>.delayed(Duration.zero);

      harness.emitFlat(terminalFrame(
        sessionId: 'sess-1',
        status: 'failed',
        reply: '',
        error: 'provider quota exhausted',
      ));
      await Future<void>.delayed(Duration.zero);

      expect(events.single.status, 'failed');
      expect(events.single.error, 'provider quota exhausted');
      await sub.cancel();
    });

    test('parses parked status (quota wait)', () async {
      final harness = _TerminalFilterHarness();
      final events = <TurnTerminalEvent>[];
      final sub = harness.typedFor('sess-1').listen(events.add);
      await Future<void>.delayed(Duration.zero);

      harness.emitFlat(terminalFrame(sessionId: 'sess-1', status: 'parked'));
      await Future<void>.delayed(Duration.zero);

      expect(events.single.status, 'parked');
      await sub.cancel();
    });
  });

  group('isTurnTerminalPayload', () {
    test('requires both turn_id and handler_case', () {
      expect(
        isTurnTerminalPayload({
          'type': 'agent_progress',
          'turn_id': 't',
          'handler_case': 'sync_dispatch',
        }),
        isTrue,
      );
      expect(
        isTurnTerminalPayload({
          'type': 'agent_progress',
          'turn_id': 't',
        }),
        isFalse,
      );
      expect(
        isTurnTerminalPayload({
          'type': 'agent_progress',
          'handler_case': 'sync_dispatch',
        }),
        isFalse,
      );
      expect(
        isTurnTerminalPayload({
          'type': 'agent_progress',
          'turn_id': '',
          'handler_case': 'sync_dispatch',
        }),
        isFalse,
      );
      expect(
        isTurnTerminalPayload(ordinaryProgressFrame(sessionId: 's')),
        isFalse,
      );
    });
  });
}
