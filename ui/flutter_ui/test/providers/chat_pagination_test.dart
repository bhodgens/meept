import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

/// SDK stub serving 450 total messages in pages of `limit`, oldest first.
class _PagedSdkClient extends SdkApiClient {
  _PagedSdkClient() : super(host: 'localhost', port: 8081);

  static const totalMessages = 450;

  List<Map<String, dynamic>> _page(int offset, int limit) => [
        for (var i = offset;
            i < offset + limit && i < totalMessages;
            i++)
          {
            'id': '$i',
            'role': i.isEven ? 'user' : 'assistant',
            'content': 'msg $i',
            'timestamp':
                DateTime.utc(2024, 1, 1).add(Duration(minutes: i)).toIso8601String(),
          },
      ];

  @override
  Future<List<Map<String, dynamic>>> getMessages(String id,
      {int offset = 0, int limit = 1000}) async =>
      _page(offset, limit);

  @override
  Future<({List<Map<String, dynamic>> messages, int total})> getMessagesPage(
      String id,
      {int offset = 0, int limit = 200}) async {
    final msgs = _page(offset, limit);
    return (messages: msgs, total: totalMessages);
  }
}

class _ConnectedWebSocket extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

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
      {required bool interrupt,
      required bool queue,
      int? maxQueueSize}) async {}
  @override Future<void> toggleTts() async {}
  @override bool get enabled => false;
  @override bool get isAvailable => false;
  @override bool get isSpeaking => false;
  double get volume => 1.0;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('initial load takes the most recent window; loadOlder prepends', () async {
    final client = _PagedSdkClient();
    final notifier = ChatNotifier(
      sdkClient: client,
      websocket: _ConnectedWebSocket(),
      ttsNotifier: _NoopTts(),
      sessionId: 's1',
    );

    // Let the auto-load finish.
    await Future<void>.delayed(Duration.zero);

    expect(notifier.state.messages, hasLength(200));
    // Newest window starts at message 250.
    expect(notifier.state.messages.first.content, 'msg 250');
    expect(notifier.state.messages.last.content, 'msg 449');
    expect(notifier.hasMoreHistory, isTrue);

    // Scroll back one page.
    final gotOlder = await notifier.loadOlderMessages();
    expect(gotOlder, isTrue);
    expect(notifier.state.messages, hasLength(400));
    expect(notifier.state.messages.first.content, 'msg 50');
    expect(notifier.hasMoreHistory, isTrue);

    // Another page reaches the beginning and stops pagination.
    await notifier.loadOlderMessages();
    expect(notifier.state.messages, hasLength(450));
    expect(notifier.state.messages.first.content, 'msg 0');
    expect(notifier.hasMoreHistory, isFalse);

    // Further calls are no-ops.
    final again = await notifier.loadOlderMessages();
    expect(again, isFalse);
    expect(notifier.state.messages, hasLength(450));
  });

  test('short session loads fully with no older history', () async {
    // A session whose total fits in one page: reuse the paged stub but the
    // provider only takes the recent-window branch when total > page len,
    // so a small total exercises the simple path. Simulate by a subclass.
    final small = _SmallSessionSdkClient();
    final notifier = ChatNotifier(
      sdkClient: small,
      websocket: _ConnectedWebSocket(),
      ttsNotifier: _NoopTts(),
      sessionId: 's2',
    );
    await Future<void>.delayed(Duration.zero);
    expect(notifier.state.messages, hasLength(5));
    expect(notifier.hasMoreHistory, isFalse);
    expect(await notifier.loadOlderMessages(), isFalse);
  });
}

class _SmallSessionSdkClient extends _PagedSdkClient {
  @override
  Future<({List<Map<String, dynamic>> messages, int total})> getMessagesPage(
      String id,
      {int offset = 0, int limit = 200}) async {
    return (
      messages: [for (var i = 0; i < 5; i++) _msg(i)],
      total: 5,
    );
  }

  Map<String, dynamic> _msg(int i) => {
        'id': '$i',
        'role': i.isEven ? 'user' : 'assistant',
        'content': 'small $i',
        'timestamp': DateTime.utc(2024, 1, 1).toIso8601String(),
      };
}
