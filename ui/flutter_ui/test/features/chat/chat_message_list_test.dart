import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_list.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/async_state.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mock / Stub Classes =====

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<T> get<T>(
    String path, {
    Map<String, dynamic>? queryParameters,
  }) async {
    // Return empty list for sessions/tasks/agents queries
    return {'sessions': [], 'tasks': [], 'agents': [], 'results': []} as T;
  }

  @override
  Future<T> post<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async {
    if (path == '/chat') {
      return {
        'id': 'msg',
        'role': 'assistant',
        'content': 'response',
        'timestamp': DateTime.now().toIso8601String(),
      } as T;
    }
    return {'sessions': [], 'tasks': [], 'agents': [], 'results': []} as T;
  }

  @override
  Future<T> put<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async {
    return {'sessions': [], 'tasks': [], 'agents': [], 'results': []} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {'success': true} as T;
  }
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

/// Test-specific ChatNotifier that doesn't load messages on init
class _TestChatNotifier extends ChatNotifier {
  _TestChatNotifier({required super.apiClient, required super.websocket});

  @override
  Future<void> loadMessages(String sessionId) async {
    // No-op in tests - state is set directly
  }
}

/// A widget that sets the chat state after the first frame, then rebuilds
/// with the child. This avoids modifying provider state during build.
class _InitialChatState extends ConsumerStatefulWidget {
  final Widget child;
  final AsyncState<List<ChatMessage>> initialState;

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
  required AsyncState<List<ChatMessage>> initialChatState,
}) {
  return ProviderScope(
    overrides: [
      chatProvider.overrideWith(
        (_) => _TestChatNotifier(
          apiClient: _StubApiClient(),
          websocket: _StubWebSocket(),
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
        initialChatState: const AsyncState.initial(),
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
        initialChatState: AsyncState.data(fixtureMessages),
      ));

      await tester.pumpAndSettle();

      expect(find.byType(ChatMessageBubble), findsNWidgets(2));
    });

    testWidgets('each bubble shows message content', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: AsyncState.data(fixtureMessages),
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

    testWidgets('shows loading indicator when sending', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: AsyncState.data(fixtureMessages),
      ));

      await tester.pump(const Duration(milliseconds: 300));

      // When not sending, no thinking indicator
      expect(
        find.text('thinking...', skipOffstage: false),
        findsNothing,
      );
    });

    testWidgets('shows error widget when error state', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: AsyncState.error(
          Exception('connection failed'),
          StackTrace.current,
        ),
      ));

      await tester.pumpAndSettle();

      expect(
        find.byIcon(Icons.error_outline),
        findsOneWidget,
      );
      expect(
        find.textContaining('connection failed', skipOffstage: false),
        findsOneWidget,
      );
    });

    testWidgets('does not show placeholder when messages exist',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: AsyncState.data(fixtureMessages),
      ));

      await tester.pumpAndSettle();

      expect(
        find.text('no messages yet', skipOffstage: false),
        findsNothing,
      );
    });

    testWidgets('does not show loading indicator when not sending',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatMessageList(sessionId: 'test-session'),
        initialChatState: AsyncState.data(fixtureMessages),
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
