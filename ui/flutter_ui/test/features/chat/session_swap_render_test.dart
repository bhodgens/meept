import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_list.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mocks (adapted from chat_message_list_test.dart) =====

class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> getMessages(String id,
      {int offset = 0, int limit = 1000}) async => [];

  @override
  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async => {};

  @override
  Future<Map<String, dynamic>> sendSteerMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async => {};

  @override
  Future<Map<String, dynamic>> sendFollowUpMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async => {};
}

class _StubWebSocket extends WebSocketService {
  _StubWebSocket() : super(host: 'localhost', port: 8081);

  @override
  Future<void> connect({String? path}) async {}

  @override
  void disconnect() {}

  @override
  bool get isConnected => true;

  @override
  void send(Map<String, dynamic> message) {}

  @override
  Stream<Map<String, dynamic>> subscribeToChat(String sessionId) =>
      const Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToAgentProgress(String sessionId) =>
      const Stream.empty();

  @override
  void unsubscribeFromChat(String sessionId) {}
}

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
  Future<void> setBehaviorSettings(
      {required bool interrupt, required bool queue, int? maxQueueSize}) async {}
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

/// A [ChatNotifier] whose [loadMessages] is a no-op.  Tests drive state
/// directly by setting `notifier.state` from a post-frame callback.
class _TestChatNotifier extends ChatNotifier {
  _TestChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
  });

  @override
  Future<void> loadMessages(String sessionId) async {
    // No-op — state is set directly by tests.
  }
}

/// Helper widget that applies an initial chat state after the first frame,
/// mirroring the pattern in [chat_message_list_test.dart].
class _InitialChatState extends ConsumerStatefulWidget {
  final Widget child;
  final ChatState initialState;

  const _InitialChatState({
    required this.child,
    required this.initialState,
  });

  @override
  ConsumerState<_InitialChatState> createState() => _InitialChatStateState();
}

class _InitialChatStateState extends ConsumerState<_InitialChatState> {
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        ref.read(chatProvider.notifier).state = widget.initialState;
      }
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

Widget _buildTestApp({
  required Widget child,
  required ChatState initialChatState,
}) {
  return ProviderScope(
    overrides: [
      chatProvider.overrideWith(
        (_) => _TestChatNotifier(
          sdkClient: _StubSdkClient(),
          websocket: _StubWebSocket(),
          ttsNotifier: _StubTtsNotifier(),
        ),
      ),
    ],
    child: MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(
        body: _InitialChatState(
          initialState: initialChatState,
          child: child,
        ),
      ),
    ),
  );
}

void main() {
  group('session swap loading state (Gap 6)', () {
    testWidgets(
        'loading indicator is visible when messages empty and isLoading is '
        'true (session swap window)', (tester) async {
      // This test reproduces the exact state shape that ChatNotifier sets
      // during loadMessages when switching sessions:
      //   state = ChatState(messages: [], isLoading: true)
      //
      // Before the fix: ChatMessageList renders MessagePlaceholder ("no
      // messages yet") because it checks messages.isEmpty without checking
      // isLoading, making the session swap look like an empty session.
      //
      // After the fix: a loading spinner is shown instead, distinguishing
      // the transient loading window from a genuinely empty session.
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test'),
        initialChatState: const ChatState(
          messages: [],
          isLoading: true,
        ),
      ));
      // Use pump (not pumpAndSettle) because the loading spinner animates
      // indefinitely, which would block pumpAndSettle forever.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      // EXPECTATION (after fix): a loading indicator is visible.
      expect(
        find.byType(CircularProgressIndicator),
        findsWidgets,
        reason: 'During a session swap, ChatNotifier sets messages: [], '
            'isLoading: true. The UI should show a loading spinner, not the '
            '"no messages yet" placeholder.',
      );
      // The "no messages yet" placeholder must NOT appear during loading.
      expect(
        find.text('no messages yet', skipOffstage: false),
        findsNothing,
        reason: 'The empty-session placeholder must not render while '
            'isLoading is true and messages are being fetched.',
      );
      expect(
        find.text('start the conversation', skipOffstage: false),
        findsNothing,
      );
    });

    testWidgets(
        'placeholder appears when messages empty and not loading', (tester) async {
      // Guards against the fix accidentally showing the spinner on
      // genuinely empty sessions.
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test'),
        initialChatState: const ChatState(
          messages: [],
          isLoading: false,
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.text('no messages yet', skipOffstage: false), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });

    testWidgets('messages render normally when loaded', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test'),
        initialChatState: ChatState(
          messages: [
            ChatMessage(
              id: '1',
              role: 'user',
              content: 'hello world',
              timestamp: DateTime.utc(2024, 1, 1),
            ),
          ],
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.textContaining('hello world'), findsOneWidget);
      expect(find.text('no messages yet', skipOffstage: false), findsNothing);
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(find.byType(ChatMessageBubble), findsOneWidget);
    });
  });
}
