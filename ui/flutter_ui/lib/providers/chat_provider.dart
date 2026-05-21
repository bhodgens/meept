import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';

/// ChatState holds the current chat messages and loading state.
class ChatState {
  final List<ChatMessage> messages;
  final bool isLoading;
  final String? error;

  ChatState({
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

/// Notifier for chat operations.
class ChatNotifier extends StateNotifier<ChatState> {
  final ApiClient _client;

  ChatNotifier(this._client) : super(ChatState());

  /// Send a chat message to the daemon.
  Future<void> sendMessage({
    required String message,
    String? sessionId,
    String? agentId,
  }) async {
    state = state.copyWith(isLoading: true, error: null);

    try {
      // Add user message to local state immediately (optimistic update)
      final userMsg = ChatMessage(
        id: 'user_${DateTime.now().millisecondsSinceEpoch}',
        role: 'user',
        content: message,
        timestamp: DateTime.now(),
      );
      state = state.copyWith(
        messages: [...state.messages, userMsg],
        isLoading: true,
      );

      // Send to daemon
      final response = await _client.sendChatMessage(
        message: message,
        sessionId: sessionId,
        agentId: agentId,
      );

      // TODO: Parse response and add assistant message
      state = state.copyWith(isLoading: false);
    } catch (e) {
      state = state.copyWith(
        error: 'Failed to send: ${e.toString()}',
        isLoading: false,
      );
    }
  }

  /// Add a message received from streaming response.
  void addMessage(ChatMessage message) {
    state = state.copyWith(
      messages: [...state.messages, message],
    );
  }

  /// Clear all messages.
  void clear() {
    state = ChatState();
  }

  /// Clear error state.
  void clearError() {
    state = state.copyWith(error: null);
  }
}

final chatProvider = StateNotifierProvider<ChatNotifier, ChatState>((ref) {
  final client = ref.watch(apiClientProvider);
  return ChatNotifier(client);
});

/// Provider for the current session ID.
final currentSessionIdProvider = StateProvider<String?>((ref) => null);
