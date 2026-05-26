import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart';

/// Maximum number of messages to keep in memory
const int _maxMessages = 500;

const _unset = Object();

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
    Object? error = _unset,
  }) {
    // Limit messages to prevent memory leaks
    List<ChatMessage> limitedMessages = messages ?? this.messages;
    if (limitedMessages.length > _maxMessages) {
      // Keep only the most recent messages
      limitedMessages = limitedMessages.sublist(limitedMessages.length - _maxMessages);
    }
    return ChatState(
      messages: limitedMessages,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

/// StateNotifier that manages chat messages for a session
class ChatNotifier extends StateNotifier<ChatState> {
  ChatNotifier({required this.apiClient, required this.websocket})
      : super(const ChatState()) {
    _initWebSocket();
  }

  final ApiClient apiClient;
  final WebSocketService websocket;
  StreamSubscription<Map<String, dynamic>>? _chatSubscription;
  StreamSubscription<Map<String, dynamic>>? _wsChatSubscription;
  String? _sessionId;
  int _loadGeneration = 0;

  /// Prevents duplicate message sends from rapid button taps
  bool _isSending = false;

  /// Initialize WebSocket connection and subscribe to chat messages
  void _initWebSocket() {
    websocket.connect();
  }

  /// Load chat history for a session and subscribe to updates
  Future<void> loadMessages(String sessionId) async {
    final generation = ++_loadGeneration;

    // Unsubscribe from previous session's WS channel to prevent accumulation
    if (_sessionId != null && _sessionId != sessionId) {
      websocket.unsubscribeFromChat(_sessionId!);
    }

    // Clear previous messages when loading a new session
    state = ChatState(
      messages: [],
      isLoading: true,
      error: null,
    );

    // Update session scope so the existing subscription filters correctly
    _sessionId = sessionId;

    // Cancel any existing WS subscription before the HTTP fetch to avoid the
    // race where WS messages arrive during the fetch and are then overwritten.
    _wsChatSubscription?.cancel();
    _wsChatSubscription = null;

    // Fetch messages from the HTTP API
    try {
      final messages = await apiClient.getMessages(sessionId);
      state = ChatState(
        messages: messages,
        isLoading: false,
      );
    } catch (e) {
      state = ChatState(
        messages: [],
        isLoading: false,
        error: e.toString(),
      );
    }

    if (generation != _loadGeneration) return;

    // Set up WS subscription AFTER the HTTP fetch completes so that any
    // messages arriving via WS are appended to (not replaced by) the fetch.
    _wsChatSubscription = websocket.subscribeToChat(sessionId).listen((message) {
      addStreamMessage(message);
    });
  }

  /// Send a message and append it to the messages list
  Future<void> sendMessage({
    required String sessionId,
    required String text,
    String? agentId,
  }) async {
    // Guard against duplicate sends from rapid taps
    if (_isSending) {
      return;
    }

    _isSending = true;

    // Append user message immediately
    final userMessage = ChatMessage(
      id: DateTime.now().millisecondsSinceEpoch.toString(),
      role: 'user',
      content: text,
      timestamp: DateTime.now(),
      sessionId: sessionId,
    );

    var newMessages = [...state.messages, userMessage];
    if (newMessages.length > _maxMessages) {
      newMessages = newMessages.sublist(newMessages.length - _maxMessages);
    }
    state = ChatState(
      messages: newMessages,
      isLoading: true,
      error: null,
    );

    try {
      await apiClient.sendChatMessage(
        message: text,
        conversationId: sessionId,
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
    } finally {
      _isSending = false;
    }
  }

  /// Add a chat message from websocket stream
  void addStreamMessage(Map<String, dynamic> data) {
    try {
      final message = ChatMessage.fromBackendMessage(data);

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

      if (newMessages.length > _maxMessages) {
        newMessages = newMessages.sublist(newMessages.length - _maxMessages);
      }

      state = state.copyWith(messages: newMessages);
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

  /// Clear error state without removing messages
  void clearError() {
    state = ChatState(
      messages: state.messages,
      isLoading: state.isLoading,
    );
  }

  /// Clear all messages
  void clearMessages() {
    state = const ChatState();
  }

  @override
  void dispose() {
    _chatSubscription?.cancel();
    _chatSubscription = null;
    _wsChatSubscription?.cancel();
    _wsChatSubscription = null;
    super.dispose();
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
