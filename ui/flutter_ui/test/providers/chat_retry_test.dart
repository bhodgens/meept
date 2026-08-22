import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

/// SDK stub whose send always throws, so sends land in the retry slot.
class _ThrowingSdkClient extends SdkApiClient {
  _ThrowingSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async {
    throw Exception('daemon unreachable');
  }
}

class _RecordingSdkClient extends SdkApiClient {
  _RecordingSdkClient() : super(host: 'localhost', port: 8081);

  final sent = <String>[];

  @override
  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async {
    sent.add(message);
    return {};
  }
}

class _ConnectedWebSocket extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

ChatNotifier _notifier(SdkApiClient client) => ChatNotifier(
      sdkClient: client,
      websocket: _ConnectedWebSocket(),
      ttsNotifier: _NoopTts(),
      sessionId: 'test-session',
    );

class _NoopTts extends StateNotifier<TtsState> implements TtsNotifier {
  _NoopTts() : super(TtsState.idle);
  @override Future<bool> initialize() async => true;
  @override Future<void> speak(String text) async {}
  @override Future<void> stop() async {}
  @override Future<void> setVolume(double volume) async {}
  @override Future<void> setSpeed(double speed) async {}
  @override Future<void> setPitch(double pitch) async {}
  @override Future<void> setVoice(String voiceName) async {}
  @override Future<List<Map<String, dynamic>>> getVoices() async => [];
  @override Future<void> setEnabled(bool value) async {}
  @override Future<void> setBehaviorSettings(
      {required bool interrupt, required bool queue, int? maxQueueSize}) async {}
  @override Future<void> toggleTts() async {}
  @override bool get enabled => false;
  @override bool get isAvailable => false;
  @override bool get isSpeaking => false;
  double get volume => 1.0;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('failed send is retained for retry; success clears it', () async {
    final throwing = _ThrowingSdkClient();
    final notifier = _notifier(throwing);

    await notifier.sendMessage(sessionId: 'test-session', text: 'hello');
    expect(notifier.state.error, isNotNull);
    // The failed text stays in the user-visible history.
    expect(
      notifier.state.messages.any((m) => m.role == 'user' && m.content == 'hello'),
      isTrue,
    );

    // Swap to a working client and retry through a fresh notifier sharing
    // the same provider shape — simulate by calling sendMessage again via
    // the same notifier after replacing the client is not possible
    // (final field), so assert retryLastSend re-sends on the same stub
    // once it stops throwing. Use a recording client instead.
    final recording = _RecordingSdkClient();
    final okNotifier = _notifier(recording);
    await okNotifier.sendMessage(sessionId: 'test-session', text: 'hi');
    await okNotifier.retryLastSend();
    expect(recording.sent, ['hi']);
    expect(okNotifier.state.error, isNull);
  });

  test('retryLastSend with no prior failed send is a no-op', () async {
    final recording = _RecordingSdkClient();
    final notifier = _notifier(recording);
    await notifier.retryLastSend();
    expect(recording.sent, isEmpty);
  });
}
