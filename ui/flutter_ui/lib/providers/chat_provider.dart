import 'dart:async';

/// Timeout for _isSending flag to prevent permanent lockout

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart'; // exports tts_provider.dart

/// Maximum number of messages to keep in memory
const int _maxMessages = 500;

const _unset = Object();

/// Send endpoint type — distinct route for normal, steer, and follow-up messages.
enum _SendEndpoint { normal, steer, followUp }

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
  ChatNotifier({required this.apiClient, required this.websocket, required this.ttsNotifier})
      : super(const ChatState()) {
    _initWebSocket();
  }

  final ApiClient apiClient;
  final WebSocketService websocket;
  final TtsNotifier ttsNotifier;
  StreamSubscription<Map<String, dynamic>>? _wsChatSubscription;
  String? _sessionId;
  int _loadGeneration = 0;

  /// Prevents duplicate message sends from rapid button taps
  bool _isSending = false;

  /// Timer to reset _isSending flag if it gets stuck (safety mechanism)
  Timer? _sendingTimeoutTimer;

  /// Maximum time to wait for send to complete before auto-resetting
  static const _sendingTimeout = Duration(seconds: 60);

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
    state = const ChatState(
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
      // Don't show error for default session (no session selected)
      if (sessionId == 'default') {
        state = const ChatState(messages: [], isLoading: false);
      } else {
        state = ChatState(
          messages: [],
          isLoading: false,
          error: e.toString(),
        );
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
    if (_isSending) {
      return;
    }

    // Block sending when disconnected — the daemon won't receive the message.
    if (!websocket.isConnected) {
      state = ChatState(
        messages: state.messages,
        isLoading: false,
        error: 'not connected to daemon — check that meept-daemon is running',
      );
      return;
    }

    _isSending = true;

    // Set safety timeout to reset flag if something goes wrong
    _sendingTimeoutTimer?.cancel();
    _sendingTimeoutTimer = Timer(_sendingTimeout, () {
      _isSending = false;
      _sendingTimeoutTimer = null;
    });

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
      state = ChatState(
        messages: state.messages,
        isLoading: false,
      );
    } catch (e) {
      // Extract URL from DioException for better error messages.
      String errorStr;
      if (e is DioException) {
        final code = e.response?.statusCode;
        final url = e.requestOptions.path;
        final method = e.requestOptions.method;
        errorStr = '$method $url -> ${code ?? e.type}';
      } else {
        errorStr = e.toString();
      }
      state = ChatState(
        messages: state.messages,
        isLoading: false,
        error: errorStr,
      );
    } finally {
      _sendingTimeoutTimer?.cancel();
      _sendingTimeoutTimer = null;
      _isSending = false;
    }
  }

  /// Add a chat message from websocket stream
  void addStreamMessage(Map<String, dynamic> data) {
    try {
      final message = ChatMessage.fromBackendMessage(data);

      // Trigger TTS for assistant messages
      if (message.role == 'assistant' && message.content.isNotEmpty) {
        ttsNotifier.speak(message.content);
      }

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
    _sendingTimeoutTimer?.cancel();
    _sendingTimeoutTimer = null;
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
  final ttsNotifier = ref.read(ttsProvider.notifier);
  return ChatNotifier(apiClient: client, websocket: websocket, ttsNotifier: ttsNotifier);
});

/// Current session ID provider
final currentSessionIdProvider = StateProvider<String?>((ref) => null);
