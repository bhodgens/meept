import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart';

/// State for the chat provider
class ChatState {
  final List<ChatMessage> messages;
  final bool isLoading;
  final String? error;

  const ChatState({
    this.messages = const [],
    this.isLoading = false,
    this.error,
  });

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? isLoading,
    String? error,
  }) {
    return ChatState(
      messages: messages ?? this.messages,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

/// StateNotifier that manages chat messages for a session
class ChatNotifier extends StateNotifier<ChatState> {
  ChatNotifier({required this.apiClient, required this.websocket})
      : super(const ChatState());

  final ApiClient apiClient;
  final WebSocketService websocket;

  /// Load chat history for a session
  Future<void> loadMessages(String sessionId) async {
    state = const ChatState(isLoading: true);
    // Chat messages would be loaded via websocket stream
    // For now, initialize empty and rely on stream
    state = const ChatState(isLoading: false);
  }

  /// Send a message and append it to the messages list
  Future<void> sendMessage({
    required String sessionId,
    required String text,
    String? agentId,
  }) async {
    // Append user message immediately
    final userMessage = ChatMessage(
      id: DateTime.now().millisecondsSinceEpoch.toString(),
      role: 'user',
      content: text,
      timestamp: DateTime.now(),
      sessionId: sessionId,
    );

    state = ChatState(
      messages: [...state.messages, userMessage],
      isLoading: true,
      error: null,
    );

    try {
      await apiClient.sendChatMessage(
        message: text,
        sessionId: sessionId,
        agentId: agentId,
      );
      state = ChatState(
        messages: state.messages,
        isLoading: false,
      );
    } catch (e) {
      state = ChatState(
        messages: state.messages,
        isLoading: false,
        error: e.toString(),
      );
    }
  }

  /// Add a chat message from websocket stream
  void addStreamMessage(Map<String, dynamic> data) {
    try {
      final message = ChatMessage(
        id: (data['id'] as String?) ??
            DateTime.now().millisecondsSinceEpoch.toString(),
        role: data['role'] as String? ?? 'assistant',
        content: data['content'] as String? ?? '',
        timestamp: data['timestamp'] != null
            ? DateTime.parse(data['timestamp'] as String)
            : DateTime.now(),
        sessionId: data['session_id'] as String?,
        toolCalls: (data['tool_calls'] as List?)?.cast<String>(),
      );

      // Replace or update existing message by id if it exists
      final existingIndex = state.messages.indexWhere(
        (m) => m.id == message.id,
      );

      List<ChatMessage> newMessages;
      if (existingIndex >= 0) {
        newMessages = [...state.messages];
        newMessages[existingIndex] = message;
      } else {
        newMessages = [...state.messages, message];
      }

      state = ChatState(
        messages: newMessages,
        isLoading: false,
        error: null,
      );
    } catch (e) {
      final errorMessage = ChatMessage(
        id: 'error_${DateTime.now().millisecondsSinceEpoch}',
        role: 'system',
        content: 'Failed to process message: $e',
        timestamp: DateTime.now(),
      );
      state = ChatState(
        messages: [...state.messages, errorMessage],
        isLoading: false,
        error: e.toString(),
      );
    }
  }

  /// Clear all messages
  void clearMessages() {
    state = const ChatState();
  }
}

/// Chat provider
final chatProvider =
    StateNotifierProvider<ChatNotifier, ChatState>((ref) {
  final client = ref.watch(apiClientProvider);
  final websocket = ref.watch(websocketProvider);
  return ChatNotifier(apiClient: client, websocket: websocket);
});

/// Current session ID provider
final currentSessionIdProvider = StateProvider<String?>((ref) => null);
