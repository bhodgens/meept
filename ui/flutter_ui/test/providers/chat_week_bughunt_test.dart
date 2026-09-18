import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

/// SDK stub with a SEQUENCE of acks: the i-th submitTurn call returns
/// acks[i] (the last one repeats). Records submitted messages.
class _SequencedSdkClient extends SdkApiClient {
  _SequencedSdkClient(this.acks) : super(host: 'localhost', port: 8081);

  final List<ChatSubmitAck> acks;
  final submitted = <String>[];
  int _calls = 0;

  @override
  Future<ChatSubmitAck> submitTurn({
    required String message,
    String? conversationId,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async {
    final i = _calls < acks.length ? _calls : acks.length - 1;
    _calls++;
    submitted.add(message);
    return acks[i];
  }
}

class _ConnectedWebSocket extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

class _NoopTts extends StateNotifier<TtsState> implements TtsNotifier {
  _NoopTts() : super(TtsState.idle);
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

/// ChatNotifier with a no-op loadMessages and a public debug surface for
/// the early-terminal buffer (F19).
class _TestChatNotifier extends ChatNotifier {
  _TestChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  @override
  Future<void> loadMessages() async {}
}

ChatNotifier _notifier(SdkApiClient client, {String sessionId = 'test-session'}) {
  return _TestChatNotifier(
    sdkClient: client,
    websocket: _ConnectedWebSocket(),
    ttsNotifier: _NoopTts(),
    sessionId: sessionId,
  );
}

ChatSubmitAck _ack(String turnId) => ChatSubmitAck(
      turnId: turnId,
      conversationId: 'test-session',
      sessionId: 'test-session',
      accepted: true,
      note: 'accepted',
    );

/// Feed a terminal-shaped frame into the notifier through the same public
/// handler the WS subscription uses.
void _terminal(
  ChatNotifier notifier, {
  required String turnId,
  required String status,
  String reply = '',
  String error = '',
  String sessionId = 'test-session',
}) {
  notifier.debugHandleTurnTerminal(TurnTerminalEvent(
    turnId: turnId,
    conversationId: sessionId,
    sessionId: sessionId,
    status: status,
    reply: reply,
    error: error,
    durationMs: 10,
  ));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('week bughunt F20: send path preserves pendingTurns', () {
    test('two concurrent sends through the REAL _doSend path keep both '
        'turns tracked (single notifier)', () async {
      final client = _SequencedSdkClient([
        _ack('turn-1'),
        _ack('turn-2'),
      ]);
      final notifier = _notifier(client);

      // Two sends through the real sendMessage -> _doSend path (not
      // submitTurn directly). The sends are awaited sequentially here, but
      // each _doSend call RECONSTRUCTS ChatState; the second send's state
      // assignment must retain the first turn.
      await notifier.sendMessage(sessionId: 'test-session', text: 'first');
      expect(notifier.state.pendingTurns.keys, contains('turn-1'),
          reason: 'first send must register turn-1');

      await notifier.sendMessage(sessionId: 'test-session', text: 'second');
      expect(
        notifier.state.pendingTurns.keys,
        containsAll(<String>['turn-1', 'turn-2']),
        reason:
            'F20: the second send dropped the first send\'s pendingTurns',
      );

      // Resolving turn-1 leaves turn-2 tracked (independence preserved).
      _terminal(notifier, turnId: 'turn-1', status: 'completed', reply: 'r1');
      expect(notifier.state.pendingTurns.keys, contains('turn-2'));
      expect(notifier.state.pendingTurns.keys, isNot(contains('turn-1')));

      // Resolving turn-2 clears everything.
      _terminal(notifier, turnId: 'turn-2', status: 'completed', reply: 'r2');
      expect(notifier.state.pendingTurns, isEmpty);
    });

    test('steer send path preserves an in-flight pending turn', () async {
      final client = _SequencedSdkClient([_ack('turn-1')]);
      final notifier = _notifier(client);

      await notifier.sendMessage(sessionId: 'test-session', text: 'work');
      expect(notifier.state.pendingTurns.keys, contains('turn-1'));

      // Steer (control-plane, no ack): must NOT drop turn-1 (F20).
      await notifier.sendSteer(sessionId: 'test-session', text: 'steer it');
      expect(
        notifier.state.pendingTurns.keys,
        contains('turn-1'),
        reason: 'F20: steer-path ChatState reconstruction dropped pendingTurns',
      );

      _terminal(notifier, turnId: 'turn-1', status: 'completed', reply: 'done');
      expect(notifier.state.pendingTurns, isEmpty);
    });
  });

  group('week bughunt F19: early terminal events are buffered', () {
    test('terminal arriving BEFORE the ack renders after registration',
        () async {
      final client = _SequencedSdkClient([_ack('turn-early')]);
      final notifier = _notifier(client);

      // Terminal relay fires while the submit is still in flight (no
      // tracked turn yet) — the event must be buffered, not dropped.
      _terminal(
        notifier,
        turnId: 'turn-early',
        status: 'completed',
        reply: 'fast reply',
      );
      expect(notifier.state.pendingTurns, isEmpty);

      // The ack registers the turn; the buffered terminal must be
      // consumed: turn resolved + reply rendered.
      await notifier.submitTurn(sessionId: 'test-session', text: 'go');
      expect(notifier.state.pendingTurns, isEmpty,
          reason: 'F19: buffered terminal consumed on ack registration');
      expect(
        notifier.state.messages.any(
          (m) => m.role == 'assistant' && m.content == 'fast reply',
        ),
        isTrue,
        reason: 'F19: early terminal reply rendered after registration',
      );
    });
  });
}
