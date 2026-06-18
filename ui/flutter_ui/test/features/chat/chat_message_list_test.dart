import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_list.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mock / Stub Classes =====

/// Stub [SdkApiClient] for chat tests that overrides the chat-related
/// endpoint methods so tests don't hit the network.
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
  }) async => {
    'id': 'msg',
    'role': 'assistant',
    'content': 'response',
    'timestamp': DateTime.now().toIso8601String(),
  };

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
  final _messageController = StreamController<Map<String, dynamic>>.broadcast();

  @override
  Stream<Map<String, dynamic>> get messageStream => _messageController.stream;

  @override
  Future<void> connect({String? path}) async {}

  @override
  void disconnect() {}

  @override
  void send(Map<String, dynamic> message) {}

  @override
  bool get isConnected => true;
}

class _StubTtsNotifier extends StateNotifier<TtsState> implements TtsNotifier {
  _StubTtsNotifier() : super(TtsState.idle);
  @override Future<bool> initialize() async => true;
  @override Future<void> speak(String text) async {}
  @override Future<void> stop() async {}
  @override Future<void> setVolume(double volume) async {}
  @override Future<void> setSpeed(double speed) async {}
  @override Future<void> setPitch(double pitch) async {}
  @override Future<void> setVoice(String voiceName) async {}
  @override Future<List<Map<String, dynamic>>> getVoices() async => [];
  @override void setEnabled(bool value) {}
  @override void setBehaviorSettings({required bool interrupt, required bool queue, int? maxQueueSize}) {}
  @override void toggleTts() {}
  @override bool get enabled => false;
  @override bool get isAvailable => false;
  @override bool get isSpeaking => false;
  double get volume => 1.0;
}

class _TestChatNotifier extends ChatNotifier {
  _TestChatNotifier({required super.sdkClient, required super.websocket, required super.ttsNotifier});

  @override
  Future<void> loadMessages(String sessionId) async {
    // No-op in tests - state is set directly
  }
}

/// A widget that sets the chat state after the first frame, then rebuilds
/// with the child. This avoids modifying provider state during build.
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
    // Set initial state immediately (before first frame)
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        ref.read(chatProvider.notifier).state = widget.initialState;
      }
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// Builds an app widget suitable for tests, with the chat provider
/// pre-configured with the given state.
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

// ===== Test fixtures =====

final fixtureMessages = <ChatMessage>[
  ChatMessage(
    id: '1',
    role: 'user',
    content: 'hello',
    timestamp: DateTime.utc(2024, 1, 1, 10, 0),
  ),
  ChatMessage(
    id: '2',
    role: 'assistant',
    content: 'hi there',
    timestamp: DateTime.utc(2024, 1, 1, 10, 1),
  ),
];

void main() {
  group('ChatMessageList', () {
    testWidgets('displays placeholder when no messages', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: const ChatState(),
      ));

      await tester.pumpAndSettle();

      expect(find.text('no messages yet', skipOffstage: false), findsOneWidget);
      expect(
        find.text('start the conversation', skipOffstage: false),
        findsOneWidget,
      );
      expect(find.byType(ChatMessageBubble), findsNothing);
    });

    testWidgets('displays messages from chatProvider', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(messages: fixtureMessages),
      ));

      await tester.pumpAndSettle();

      expect(find.byType(ChatMessageBubble), findsNWidgets(2));
    });

    testWidgets('each bubble shows message content', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(messages: fixtureMessages),
      ));

      await tester.pumpAndSettle();

      expect(
        find.textContaining('hello', skipOffstage: false),
        findsOneWidget,
      );
      expect(
        find.textContaining('hi there', skipOffstage: false),
        findsOneWidget,
      );
    });

    testWidgets('shows loading indicator when isLoading is true',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(
          messages: fixtureMessages,
          isLoading: true,
        ),
      ));

      await tester.pump(const Duration(milliseconds: 300));

      expect(
        find.text('thinking...', skipOffstage: false),
        findsOneWidget,
      );
      expect(
        find.byType(CircularProgressIndicator),
        findsOneWidget,
      );
    });

    testWidgets('shows error banner when error is present', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(
          messages: fixtureMessages,
          error: 'connection failed',
        ),
      ));

      await tester.pumpAndSettle();

      expect(
        find.byIcon(Icons.error_outline),
        findsOneWidget,
      );
      expect(
        find.text('connection failed', skipOffstage: false),
        findsOneWidget,
      );
    });

    testWidgets('does not show placeholder when messages exist',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(messages: fixtureMessages),
      ));

      await tester.pumpAndSettle();

      expect(
        find.text('no messages yet', skipOffstage: false),
        findsNothing,
      );
    });

    testWidgets('shows loading indicator when isAgentProcessing is true',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(
          messages: fixtureMessages,
          isLoading: false,
          isAgentProcessing: true,
        ),
      ));

      await tester.pump(const Duration(milliseconds: 300));

      expect(
        find.text('thinking...', skipOffstage: false),
        findsOneWidget,
      );
      expect(
        find.byType(CircularProgressIndicator),
        findsOneWidget,
      );
    });

    testWidgets('does not show loading indicator when neither flag is true',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: ChatState(messages: fixtureMessages),
      ));

      await tester.pumpAndSettle();

      expect(
        find.text('thinking...', skipOffstage: false),
        findsNothing,
      );
    });
  });

  group('MessagePlaceholder', () {
    testWidgets('renders chat bubble icon', (tester) async {
      await tester.pumpWidget(ProviderScope(
        child: MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: MessagePlaceholder()),
        ),
      ));

      expect(
        find.byIcon(Icons.chat_bubble_outline),
        findsOneWidget,
      );
    });
  });
}
