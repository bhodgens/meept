// 04-clutter TEST-GATE T7 (F11b): GUI dedupe robustness pin.
//
// Frozen defect: notes-server 2.4 double-render, GUI half. The daemon emits
// the SAME full reply as BOTH a chat_message WS push AND the turn.terminal
// relay. ChatNotifier dedupes by _lastAssistantContentEquals - a
// LAST-ASSISTANT-BUBBLE-ONLY content comparison. Any interleaved message
// (a system note, a second turn's reply, a stream delta) between the
// chat_message bubble and the terminal defeats it and the reply renders
// twice. Content equality is also wrong in the other direction: two
// DISTINCT turns with identical answer text ("4") would be deduped if one
// bubble is the last assistant message.
//
// DEDUPE CONTRACT (pinned in BOTH client halves, master C1): a task reply
// renders ONCE keyed by (turn_id, task_id) from the terminal payload -
// NOT by last-bubble content. Whichever delivery arrives first wins; the
// second is suppressed. A late terminal after a stalled/parked notice
// still renders (dedupe never suppresses a real terminal). Until
// 05-navigation/01 lands taskId on ChatMessage, key on turn_id alone
// (defensive); both shapes are pinned here.
//
// RED today: interleaved system message defeats the content dedupe
// (double render); identical-content distinct turns over-dedupe.
// GREEN after 04-clutter/02 T3 (Dart half).

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/models/ws_events.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

/// SDK stub: submitTurn returns a canned ack for the requested turn id.
class _DedupeStubSdk extends SdkApiClient {
  _DedupeStubSdk() : super(host: 'localhost', port: 8081);

  String nextTurnId = 'turn-dedupe-1';

  @override
  Future<ChatSubmitAck> submitTurn({
    required String message,
    String? conversationId,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async {
    return ChatSubmitAck(
      turnId: nextTurnId,
      conversationId: 'test-session',
      sessionId: 'test-session',
      accepted: true,
      note: 'accepted; result arrives via turn.terminal',
    );
  }
}

class _DedupeConnectedWebSocket extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

class _DedupeNoopTts extends StateNotifier<TtsState> implements TtsNotifier {
  _DedupeNoopTts() : super(TtsState.idle);
  @override
  Future<bool> initialize() async => true;
  @override
  Future<void> speak(String text) async {}
  @override
  Future<void> stop() async {}
  @override
  Future<void> setVolume(double volume) async {}
  @override
  Future<void> setSpeed(double speed) async {}
  @override
  Future<void> setPitch(double pitch) async {}
  @override
  Future<void> setVoice(String voiceName) async {}
  @override
  Future<List<Map<String, dynamic>>> getVoices() async => [];
  @override
  Future<void> setEnabled(bool value) async {}
  @override
  Future<void> setBehaviorSettings({
    required bool interrupt,
    required bool queue,
    int? maxQueueSize,
  }) async {}
  @override
  Future<void> toggleTts() async {}
  @override
  bool get enabled => false;
  @override
  bool get isAvailable => false;
  @override
  bool get isSpeaking => false;
  @override
  double get volume => 1.0;
}

/// ChatNotifier with a no-op loadMessages (clean state, no HTTP).
class _DedupeChatNotifier extends ChatNotifier {
  _DedupeChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  @override
  Future<void> loadMessages() async {}
}

_DedupeChatNotifier _notifier(_DedupeStubSdk sdk) {
  return _DedupeChatNotifier(
    sdkClient: sdk,
    websocket: _DedupeConnectedWebSocket(),
    ttsNotifier: _DedupeNoopTts(),
    sessionId: 'test-session',
  );
}

/// Feed the chat_message WS push (the first delivery of the reply).
void _chatMessage(ChatNotifier n, String content, {String id = 'msg-1'}) {
  n.addStreamMessage({
    'id': id,
    'role': 'assistant',
    'content': content,
    'session_id': 'test-session',
    'conversation_id': 'test-session',
    'timestamp': DateTime.now().toIso8601String(),
  });
}

/// Feed the turn.terminal relay (the second delivery of the same reply).
void _terminal(
  ChatNotifier n, {
  required String turnId,
  required String reply,
  String taskId = '',
  String status = 'completed',
}) {
  n.debugHandleTurnTerminal(
    TurnTerminalEvent(
      turnId: turnId,
      conversationId: 'test-session',
      sessionId: 'test-session',
      status: status,
      reply: reply,
      taskId: taskId,
      handlerCase: 'task_completed_relay',
      durationMs: 10,
    ),
  );
}

int _assistantCount(ChatNotifier n, String content) => n.state.messages
    .where((m) => m.role == 'assistant' && m.content == content)
    .length;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GUI dedupe robustness (04-clutter T7, C1 key = turn_id/task_id)', () {
    test(
      'chat_message then terminal with interleaved assistant message renders once',
      () async {
        final sdk = _DedupeStubSdk();
        final n = _notifier(sdk);
        await n.submitTurn(sessionId: 'test-session', text: 'what is 2+2?');

        const reply = 'the answer is 4.';
        _chatMessage(n, reply);
        // An interleaved ASSISTANT message (a concurrent turn's reply or a
        // stream delta) becomes the last assistant bubble, so the
        // last-bubble content comparison no longer sees this turn's reply
        // and the terminal renders it a second time.
        _chatMessage(n, 'a different concurrent reply', id: 'msg-interleaved');
        _terminal(
          n,
          turnId: 'turn-dedupe-1',
          reply: reply,
          taskId: 'task-dedupe-1',
        );

        expect(
          _assistantCount(n, reply),
          1,
          reason:
              'F11b: the reply rendered twice - an interleaved assistant message '
              'defeats last-bubble content dedupe; key on (turn_id, task_id)',
        );
      },
    );

    test(
      'interleaved system note does not defeat dedupe (regression)',
      () async {
        // GREEN today, must survive the fix: a system note between the
        // chat_message bubble and the terminal must not cause a double
        // render (the id-keyed dedupe is robust to ANY interleaving).
        final sdk = _DedupeStubSdk();
        final n = _notifier(sdk);
        await n.submitTurn(sessionId: 'test-session', text: 'what is 2+2?');

        const reply = 'the answer is 4.';
        _chatMessage(n, reply);
        n.addStreamMessage({
          'type': 'system',
          'content': 'context was compacted',
          'session_id': 'test-session',
        });
        _terminal(
          n,
          turnId: 'turn-dedupe-1',
          reply: reply,
          taskId: 'task-dedupe-1',
        );

        expect(_assistantCount(n, reply), 1);
      },
    );

    test('terminal then chat_message redelivery renders once', () async {
      final sdk = _DedupeStubSdk();
      final n = _notifier(sdk);
      await n.submitTurn(sessionId: 'test-session', text: 'what is 2+2?');

      const reply = 'the answer is 4.';
      _terminal(
        n,
        turnId: 'turn-dedupe-1',
        reply: reply,
        taskId: 'task-dedupe-1',
      );
      // The chat_message push lands after the terminal (raced order). It
      // carries the turn identity defensively (05-navigation/01 adds it to
      // the wire; 02 reads it when present). With the identity, the
      // id-keyed dedupe recognizes the redelivery and suppresses it.
      n.addStreamMessage({
        'id': 'msg-redelivery',
        'role': 'assistant',
        'content': reply,
        'session_id': 'test-session',
        'conversation_id': 'test-session',
        'turn_id': 'turn-dedupe-1',
        'task_id': 'task-dedupe-1',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(
        _assistantCount(n, reply),
        1,
        reason:
            'whichever delivery arrives first wins; the second is suppressed '
            'by (turn_id, task_id), not by content',
      );
    });

    test('distinct turns with identical content both render', () async {
      final sdk = _DedupeStubSdk();
      final n = _notifier(sdk);

      const reply = '4';
      sdk.nextTurnId = 'turn-dedupe-a';
      await n.submitTurn(sessionId: 'test-session', text: 'what is 2+2?');
      _terminal(n, turnId: 'turn-dedupe-a', reply: reply, taskId: 'task-a');

      sdk.nextTurnId = 'turn-dedupe-b';
      await n.submitTurn(sessionId: 'test-session', text: 'what is 1+3?');
      _terminal(n, turnId: 'turn-dedupe-b', reply: reply, taskId: 'task-b');

      expect(
        _assistantCount(n, reply),
        2,
        reason:
            'dedupe keys on (turn_id, task_id), NOT content - two distinct '
            'turns with the same answer text must both render',
      );
    });

    test('late terminal after parked notice still renders', () async {
      // REGRESSION PIN: parked/stalled notices are not terminals; the real
      // terminal that lands later must render exactly once.
      final sdk = _DedupeStubSdk();
      final n = _notifier(sdk);
      await n.submitTurn(sessionId: 'test-session', text: 'what is 2+2?');

      _terminal(n, turnId: 'turn-dedupe-1', reply: '', status: 'parked');
      const reply = 'the answer is 4.';
      _terminal(
        n,
        turnId: 'turn-dedupe-1',
        reply: reply,
        taskId: 'task-dedupe-1',
      );

      expect(
        _assistantCount(n, reply),
        1,
        reason: 'dedupe must never suppress a late real terminal',
      );
      expect(n.state.pendingTurns, isEmpty);
    });

    test('chat_message carries turn identity for the dedupe key', () {
      // C1: the dedupe key is (turn_id, task_id). The chat_message bubble
      // must carry the turn identity so the terminal can match it; until
      // 05-navigation/01 lands taskId on ChatMessage the key is turn_id
      // alone (defensive). Pin the model surface: RED until ChatMessage
      // can carry the identity (toJson round-trip survives it).
      final msg = ChatMessage(
        id: 'turn_turn-dedupe-key',
        role: 'assistant',
        content: 'the answer is 4.',
        timestamp: DateTime.now(),
        sessionId: 'test-session',
      );
      final json = msg.toJson();
      final hasTurnId = json.containsKey('turn_id');
      final hasTaskId = json.containsKey('task_id');
      expect(
        hasTurnId || hasTaskId,
        isTrue,
        reason:
            'ChatMessage carries no turn/task identity - the GUI cannot form '
            'the C1 (turn_id, task_id) dedupe key; 05-navigation/01 adds taskId, '
            '02-trivial-lane-dedupe reads it defensively',
      );
    });

    test(
      'plain terminal without prior chat_message renders once (regression)',
      () async {
        // GREEN today, must survive the fix: the normal async path.
        final sdk = _DedupeStubSdk();
        final n = _notifier(sdk);
        await n.submitTurn(sessionId: 'test-session', text: 'hello');
        _terminal(n, turnId: 'turn-dedupe-1', reply: 'hi there');
        expect(_assistantCount(n, 'hi there'), 1);
        expect(n.state.pendingTurns, isEmpty);
      },
    );
  });
}
