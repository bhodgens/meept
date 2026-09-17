import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_list.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

import '../../mocks/mock_websocket_service.dart';

class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);
}

class _StubWebSocket extends MockWebSocketService {}

class _StubTtsNotifier extends StateNotifier<TtsState> implements TtsNotifier {
  _StubTtsNotifier() : super(TtsState.idle);
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

Widget _buildTestApp({required Widget child}) {
  return ProviderScope(
    overrides: [
      chatProvider('test-session').overrideWith(
        (ref) => _TestChatNotifier(
          sdkClient: _StubSdkClient(),
          websocket: _StubWebSocket(),
          ttsNotifier: _StubTtsNotifier(),
          sessionId: 'test-session',
        ),
      ),
    ],
    child: MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(body: child),
    ),
  );
}

Future<void> _pumpBounded(WidgetTester tester, {int frames = 4}) async {
  for (var i = 0; i < frames; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  testWidgets('pending turn renders lowercase "running turn …" row',
      (tester) async {
    late ProviderContainer container;
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          chatProvider('test-session').overrideWith(
            (ref) {
              final notifier = _TestChatNotifier(
                sdkClient: _StubSdkClient(),
                websocket: _StubWebSocket(),
                ttsNotifier: _StubTtsNotifier(),
                sessionId: 'test-session',
              );
              return notifier;
            },
          ),
        ],
        child: MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(
            body: ChatMessageList(sessionId: 'test-session'),
          ),
        ),
      ),
    );
    container = ProviderScope.containerOf(
      tester.element(find.byType(ChatMessageList)),
    );
    final notifier = container.read(chatProvider('test-session').notifier);
    notifier.state = notifier.state.copyWith(
      pendingTurns: {
        'turn-1': PendingTurn(
          turnId: 'turn-1',
          conversationId: 'test-session',
          sessionId: 'test-session',
          startedAt: DateTime.now(),
          lastProgressAt: DateTime.now(),
        ),
      },
      isAgentProcessing: true,
    );

    await _pumpBounded(tester);

    expect(find.byType(PendingTurnIndicator), findsOneWidget);
    expect(find.text('running turn …'), findsOneWidget);
  });

  testWidgets('stalled turn renders the stalled text; parked renders quota '
      'text', (tester) async {
    late ProviderContainer container;
    await tester.pumpWidget(_buildTestApp(
      child: const ChatMessageList(sessionId: 'test-session'),
    ));
    container = ProviderScope.containerOf(
      tester.element(find.byType(ChatMessageList)),
    );
    final notifier = container.read(chatProvider('test-session').notifier);
    notifier.state = notifier.state.copyWith(
      pendingTurns: {
        'turn-s': PendingTurn(
          turnId: 'turn-s',
          conversationId: 'test-session',
          sessionId: 'test-session',
          startedAt: DateTime.now(),
          lastProgressAt: DateTime.now(),
          status: PendingTurnStatus.stalled,
          progressText: 'no progress for 120s — task may still complete',
        ),
        'turn-p': PendingTurn(
          turnId: 'turn-p',
          conversationId: 'test-session',
          sessionId: 'test-session',
          startedAt: DateTime.now(),
          lastProgressAt: DateTime.now(),
          status: PendingTurnStatus.parked,
          progressText:
              'waiting for provider quota — will resume automatically',
        ),
      },
      isAgentProcessing: true,
    );

    await _pumpBounded(tester);

    expect(find.byType(PendingTurnIndicator), findsNWidgets(2));
    expect(
      find.text('no progress for 120s — task may still complete'),
      findsOneWidget,
    );
    expect(
      find.text('waiting for provider quota — will resume automatically'),
      findsOneWidget,
    );
  });
}
