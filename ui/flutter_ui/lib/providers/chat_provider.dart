import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import 'async_state.dart';
import 'providers.dart';

/// Maximum number of messages to keep in memory
const int _maxMessages = 500;

/// Send endpoint type — distinct route for normal, steer, and follow-up messages.
enum _SendEndpoint { normal, steer, followUp }

/// StateNotifier that manages chat messages for a session
class ChatNotifier extends StateNotifier<AsyncState<List<ChatMessage>>> {
  ChatNotifier({required this.apiClient, required this.websocket})
      : super(const AsyncState.initial()) {
    _initWebSocket();
  }

  final ApiClient apiClient;
  final WebSocketService websocket;
  StreamSubscription<Map<String, dynamic>>? _chatSubscription;
  StreamSubscription<Map<String, dynamic>>? _wsChatSubscription;
  String? _sessionId;
  int _loadGeneration = 0;

  /// Tracks whether a message is currently being sent.
  bool get isSending => _isSending;
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
    state = const AsyncState.loading();

    // Update session scope so the existing subscription filters correctly
    _sessionId = sessionId;

    // Cancel any existing WS subscription before the HTTP fetch to avoid the
    // race where WS messages arrive during the fetch and are then overwritten.
    _wsChatSubscription?.cancel();
    _wsChatSubscription = null;

    // Fetch messages from the HTTP API
    try {
      final messages = await apiClient.getMessages(sessionId);
      state = AsyncState.data(messages);
    } catch (e, st) {
      // Don't show error for default session (no session selected)
      if (sessionId == 'default') {
        state = const AsyncState.data([]);
      } else {
        state = AsyncState.error(e, st);
      }
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
    await _doSend(
      sessionId: sessionId,
      text: text,
      agentId: agentId,
      endpoint: _SendEndpoint.normal,
    );
  }

  /// Send a steering message (double-enter or explicit steer).
  Future<void> sendSteer({
    required String sessionId,
    required String text,
  }) async {
    await _doSend(
      sessionId: sessionId,
      text: text,
      endpoint: _SendEndpoint.steer,
    );
  }

  /// Send a follow-up message.
  Future<void> sendFollowUp({
    required String sessionId,
    required String text,
  }) async {
    await _doSend(
      sessionId: sessionId,
      text: text,
      endpoint: _SendEndpoint.followUp,
    );
  }

  Future<void> _doSend({
    required String sessionId,
    required String text,
    String? agentId,
    required _SendEndpoint endpoint,
  }) async {
    // Guard against duplicate sends from rapid taps
    if (_isSending) return;

    _isSending = true;

    // Append user message immediately
    final userMessage = ChatMessage(
      id: DateTime.now().millisecondsSinceEpoch.toString(),
      role: 'user',
      content: text,
      timestamp: DateTime.now(),
      sessionId: sessionId,
    );

    final currentMessages = state.whenOrNull(data: (msgs) => msgs) ?? [];
    var newMessages = [...currentMessages, userMessage];
    if (newMessages.length > _maxMessages) {
      newMessages = newMessages.sublist(newMessages.length - _maxMessages);
    }
    state = AsyncState.data(newMessages);

    try {
      switch (endpoint) {
        case _SendEndpoint.normal:
          await apiClient.sendChatMessage(
            message: text,
            conversationId: sessionId,
            agentId: agentId,
          );
        case _SendEndpoint.steer:
          await apiClient.sendSteerMessage(
            message: text,
            conversationId: sessionId,
            source: 'flutter_ui',
          );
        case _SendEndpoint.followUp:
          await apiClient.sendFollowUpMessage(
            message: text,
            conversationId: sessionId,
            source: 'flutter_ui',
          );
      }
      _isSending = false;
      state = AsyncState.data(newMessages);
    } catch (e, st) {
      _isSending = false;
      state = AsyncState.error(e, st);
    }
  }

  /// Add a chat message from websocket stream
  void addStreamMessage(Map<String, dynamic> data) {
    try {
      final message = ChatMessage.fromBackendMessage(data);

      final currentMessages = state.whenOrNull(data: (msgs) => msgs) ?? [];

      // Replace or update existing message by id if it exists
      final existingIndex = currentMessages.indexWhere(
        (m) => m.id == message.id,
      );

      List<ChatMessage> updatedMessages;
      if (existingIndex >= 0) {
        updatedMessages = [...currentMessages];
        updatedMessages[existingIndex] = message;
      } else {
        updatedMessages = [...currentMessages, message];
      }

      if (updatedMessages.length > _maxMessages) {
        updatedMessages = updatedMessages.sublist(
            updatedMessages.length - _maxMessages);
      }

      state = AsyncState.data(updatedMessages);
    } catch (e, st) {
      final errorMessage = ChatMessage(
        id: 'error_${DateTime.now().millisecondsSinceEpoch}',
        role: 'system',
        content: 'Failed to process message: $e',
        timestamp: DateTime.now(),
      );
      final currentMessages = state.whenOrNull(data: (msgs) => msgs) ?? [];
      state = AsyncState.data([...currentMessages, errorMessage]);
    }
  }

  /// Clear error state without removing messages
  void clearError() {
    state = const AsyncState.initial();
  }

  /// Clear all messages
  void clearMessages() {
    state = const AsyncState.initial();
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
    StateNotifierProvider<ChatNotifier, AsyncState<List<ChatMessage>>>((ref) {
  final client = ref.watch(apiClientProvider);
  final websocket = ref.watch(websocketProvider);
  return ChatNotifier(apiClient: client, websocket: websocket);
});

/// Current session ID provider
final currentSessionIdProvider = StateProvider<String?>((ref) => null);
