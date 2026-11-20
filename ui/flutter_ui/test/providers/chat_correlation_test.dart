// 05-navigation TEST-GATE T3 (leaf 00T-gate.md, matrix F12b; master C1).
//
// Pins that the GUI keeps the C1 terminal correlation fields on the
// reply ChatMessage: TurnTerminalEvent already parses taskId/intentType/
// handlerCase/classifiedBy (ws_events.dart), but _consumeTurnTerminal
// builds the bubble WITHOUT them (chat_provider.dart:719-725) and the
// ChatMessage model has no such fields (api_models.dart:399-409).
//
// RED today: the bubble's toJson() carries no task_id/intent_type/
// handler_case/classified_by keys. Assertions read the JSON map (not
// typed getters) so this file compiles at HEAD and turns GREEN with zero
// edits when the freezed fields land (spec-first gate rule: never a
// compile-error RED). The mode chip is pinned at the widget seam.
//
// Includes the late-terminal (F19 early-buffer) and copyWith regression
// pins required by the leaf review checklist.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/models/ws_events.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

/// SDK stub with a canned accepted ack (same shape as the leaf-05 suite).
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 65432);

  @override
  Future<ChatSubmitAck> submitTurn({
    required String message,
    String? conversationId,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async {
    return const ChatSubmitAck(
      turnId: 'turn-1',
      conversationId: 'test-session',
      sessionId: 'test-session',
      accepted: true,
      note: 'accepted; result arrives via turn.terminal',
    );
  }

  @override
  Future<String> getClientConfig() async => '';
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
  double get volume => 1.0;
}

/// ChatNotifier with a no-op history load (clean state, no HTTP).
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

ChatNotifier _notifier() {
  return _TestChatNotifier(
    sdkClient: _StubSdkClient(),
    websocket: _ConnectedWebSocket(),
    ttsNotifier: _NoopTts(),
    sessionId: 'test-session',
  );
}

/// The frozen C1 terminal frame (master.md C1): correlation subset.
TurnTerminalEvent _c1Terminal({String turnId = 'turn-1'}) {
  return TurnTerminalEvent(
    turnId: turnId,
    conversationId: 'test-session',
    sessionId: 'test-session',
    status: TurnTerminalEvent.statusCompleted,
    reply: 'quartz reply text',
    durationMs: 42,
    taskId: 'task-42',
    intentType: 'code',
    handlerCase: 'task_dispatch',
    classifiedBy: 'llm',
    agentId: 'executor',
    model: 'fake-model',
  );
}

/// Asserts the four C1 correlation keys on a message's JSON shape.
void _expectC1Keys(ChatMessage msg, String label) {
  final json = msg.toJson();
  const want = {
    'task_id': 'task-42',
    'intent_type': 'code',
    'handler_case': 'task_dispatch',
    'classified_by': 'llm',
  };
  want.forEach((key, value) {
    expect(
      json[key],
      value,
      reason:
          '$label: C1 key $key dropped (master C1; '
          'chat_provider bubble build / ChatMessage model)',
    );
  });
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GUI C1 correlation (05-navigation T3)', () {
    test('TestChatMessageTaskID: terminal reply keeps C1 fields', () async {
      final notifier = _notifier();
      addTearDown(notifier.dispose);
      await notifier.submitTurn(sessionId: 'test-session', text: 'hello');

      notifier.debugHandleTurnTerminal(_c1Terminal());

      final replies = notifier.state.messages
          .where(
            (m) => m.role == 'assistant' && m.content == 'quartz reply text',
          )
          .toList();
      expect(replies, hasLength(1), reason: 'terminal reply must render');
      _expectC1Keys(replies.single, 'ChatMessage');
      expect(notifier.state.pendingTurns, isEmpty);
    });

    test('late terminal (F19 early buffer) keeps C1 fields', () async {
      final notifier = _notifier();
      addTearDown(notifier.dispose);

      // Terminal races the ack: buffered, then consumed on registration.
      notifier.debugHandleTurnTerminal(_c1Terminal());
      await notifier.submitTurn(sessionId: 'test-session', text: 'hello');

      final replies = notifier.state.messages
          .where(
            (m) => m.role == 'assistant' && m.content == 'quartz reply text',
          )
          .toList();
      expect(replies, hasLength(1), reason: 'early-buffer reply must render');
      _expectC1Keys(replies.single, 'ChatMessage (early-buffer path)');
    });

    test('copyWith discipline: terminal keeps concurrent pending turn', () async {
      final notifier = _notifier();
      addTearDown(notifier.dispose);
      await notifier.submitTurn(sessionId: 'test-session', text: 'hello');

      // A second tracked turn (direct state injection mirrors the
      // concurrent-submit arm of the Go gate).
      final second = PendingTurn(
        turnId: 'turn-2',
        conversationId: 'test-session',
        sessionId: 'test-session',
        startedAt: DateTime.now(),
        lastProgressAt: DateTime.now(),
      );
      notifier.state = notifier.state.copyWith(
        pendingTurns: {...notifier.state.pendingTurns, 'turn-2': second},
      );

      notifier.debugHandleTurnTerminal(_c1Terminal());

      expect(
        notifier.state.pendingTurns.containsKey('turn-2'),
        isTrue,
        reason:
            'resolving turn-1 orphaned turn-2 (fresh ChatState mid-turn; F20 class)',
      );
      expect(notifier.state.isAgentProcessing, isTrue);
      final replies = notifier.state.messages
          .where((m) => m.role == 'assistant')
          .toList();
      _expectC1Keys(replies.single, 'ChatMessage (concurrent resolution)');
    });

    test('history roundtrip: fromBackendMessage keeps C1 keys', () {
      // C1 fields persisted on the backend message must survive the
      // history parse (the chip renders on restored transcripts too).
      final msg = ChatMessage.fromBackendMessage({
        'id': 7,
        'role': 'assistant',
        'content': 'quartz reply text',
        'timestamp': '2026-11-19T10:00:00Z',
        'session_id': 'test-session',
        'task_id': 'task-42',
        'intent_type': 'code',
        'handler_case': 'task_dispatch',
        'classified_by': 'llm',
      });
      _expectC1Keys(msg, 'ChatMessage.fromBackendMessage');
    });

    testWidgets('mode chip renders from intent_type/handler_case', (
      tester,
    ) async {
      // The bubble's mode-chip TODO (chat_message_bubble.dart:130-149):
      // a correlated reply must surface its route as a chip, not rely on
      // the markdown **mode:** line. JSON-built message so the test
      // compiles before the typed fields land.
      final msg = ChatMessage.fromJson({
        'id': 'turn_turn-1',
        'role': 'assistant',
        'content': 'quartz reply text',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'test-session',
        'task_id': 'task-42',
        'intent_type': 'code',
        'handler_case': 'task_dispatch',
        'classified_by': 'llm',
      });
      await tester.pumpWidget(
        ProviderScope(
          child: MaterialApp(
            home: Scaffold(
              body: SingleChildScrollView(
                child: ChatMessageBubble(message: msg),
              ),
            ),
          ),
        ),
      );
      await tester.pump();

      expect(
        find.textContaining('task-42'),
        findsWidgets,
        reason:
            'reply bubble renders no task chip carrying task_id (W14 GUI half)',
      );
      expect(
        find.textContaining('task_dispatch'),
        findsWidgets,
        reason: 'mode chip missing: handler_case not surfaced (mode-chip TODO)',
      );
    });
  });
}
