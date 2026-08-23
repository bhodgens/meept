import 'dart:async';
import 'dart:convert';

/// Timeout for _isSending flag to prevent permanent lockout

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart'; // exports tts_provider.dart

/// Detect a phase-1 destructive-action confirmation request in a WebSocket
/// message map.  Returns the confirmation payload (a Map with
/// requires_confirmation/action/summary/details) or null when the message is
/// not a confirmation request.
///
/// The daemon-side agent loop normally auto-declines phase-1 responses when
/// no interactive UI is available, but when a WS-connected client is present
/// the raw tool result (with requires_confirmation: true) may be forwarded
/// through agent_progress or chat_message events.  We also detect the
/// declined form so the UI can optionally re-prompt the user.
Map<String, dynamic>? _extractConfirmationRequest(Map<String, dynamic> data) {
  // Direct phase-1 confirmation request.
  if (data['requires_confirmation'] == true) {
    return data;
  }

  // Some daemon configurations embed the tool result JSON inside the
  // agent_progress message field or the chat message content.  Try to
  // parse it out.
  final message = data['message'];
  if (message is String) {
    final extracted = _tryParseConfirmationJson(message);
    if (extracted != null) return extracted;
  }

  final content = data['content'];
  if (content is String) {
    final extracted = _tryParseConfirmationJson(content);
    if (extracted != null) return extracted;
  }

  // Check nested result/data fields.
  final result = data['result'];
  if (result is Map<String, dynamic> && result['requires_confirmation'] == true) {
    return result;
  }

  final dataField = data['data'];
  if (dataField is Map<String, dynamic>) {
    if (dataField['requires_confirmation'] == true) {
      return dataField;
    }
    final nestedResult = dataField['result'];
    if (nestedResult is Map<String, dynamic> &&
        nestedResult['requires_confirmation'] == true) {
      return nestedResult;
    }
  }

  return null;
}

/// Try to extract a JSON object containing requires_confirmation from a
/// string that may be a JSON blob or contain embedded JSON.
Map<String, dynamic>? _tryParseConfirmationJson(String text) {
  // Fast path: direct JSON.
  try {
    final decoded = jsonDecode(text);
    if (decoded is Map<String, dynamic> &&
        decoded['requires_confirmation'] == true) {
      return decoded;
    }
  } catch (_) {
    // Not pure JSON — try to find an embedded JSON object.
  }

  // Slow path: look for an embedded JSON blob containing
  // requires_confirmation.  This handles cases where the daemon wraps the
  // tool result inside a larger message.
  final idx = text.indexOf('"requires_confirmation"');
  if (idx < 0) return null;

  // Walk backwards to find the opening brace.
  var braceIdx = idx;
  while (braceIdx > 0 && text[braceIdx] != '{') {
    braceIdx--;
  }
  if (text[braceIdx] != '{') return null;

  // Walk forwards to find the matching closing brace.
  var depth = 0;
  var endIdx = braceIdx;
  for (var i = braceIdx; i < text.length; i++) {
    if (text[i] == '{') {
      depth++;
    } else if (text[i] == '}') {
      depth--;
      if (depth == 0) {
        endIdx = i;
        break;
      }
    }
  }
  if (depth != 0) return null;

  try {
    final decoded = jsonDecode(text.substring(braceIdx, endIdx + 1));
    if (decoded is Map<String, dynamic> &&
        decoded['requires_confirmation'] == true) {
      return decoded;
    }
  } catch (_) {
    // Malformed JSON — give up.
  }

  return null;
}

/// Maximum number of messages to keep in memory
const int _maxMessages = 500;

const _unset = Object();
const _progressUnset = Object();
const _confirmUnset = Object();
const _thinkingUnset = Object();

/// Send endpoint type — distinct route for normal, steer, and follow-up messages.
enum _SendEndpoint { normal, steer, followUp }

/// State for the chat provider
class ChatState {
  final List<ChatMessage> messages;
  /// Whether the session history is being loaded from the server.
  final bool isLoading;
  /// Whether the agent is actively processing (receiving progress events
  /// via WebSocket).  This tracks the real agent lifecycle separately from the
  /// HTTP call lifecycle so the progress indicator stays visible while the
  /// agent works.
  final bool isAgentProcessing;
  final String? error;
  final AgentProgress? currentProgress;
  /// When the agent started thinking (wall clock). Used by the UI to render
  /// an elapsed timer ("thinking 12s..."). Null when the agent is idle.
  final DateTime? thinkingStartedAt;

  /// When non-null, a destructive tool returned a phase-1 confirmation
  /// request and the UI must prompt the user.  The value is the confirmation
  /// payload (action, summary, details, ...) returned by the tool.  The UI
  /// calls [ChatNotifier.resolveConfirmation] to confirm or decline.
  final Map<String, dynamic>? pendingConfirmation;

  const ChatState({
    this.messages = const [],
    this.isLoading = false,
    this.isAgentProcessing = false,
    this.error,
    this.currentProgress,
    this.thinkingStartedAt,
    this.pendingConfirmation,
  });

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? isLoading,
    bool? isAgentProcessing,
    Object? error = _unset,
    Object? currentProgress = _progressUnset,
    Object? pendingConfirmation = _confirmUnset,
    Object? thinkingStartedAt = _thinkingUnset,
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
      isAgentProcessing: isAgentProcessing ?? this.isAgentProcessing,
      error: identical(error, _unset) ? this.error : error as String?,
      currentProgress: identical(currentProgress, _progressUnset)
          ? this.currentProgress
          : currentProgress as AgentProgress?,
      pendingConfirmation: identical(pendingConfirmation, _confirmUnset)
          ? this.pendingConfirmation
          : pendingConfirmation as Map<String, dynamic>?,
      thinkingStartedAt: identical(thinkingStartedAt, _thinkingUnset)
          ? this.thinkingStartedAt
          : thinkingStartedAt as DateTime?,
    );
  }
}

/// StateNotifier that manages chat messages for a session.
/// Each sessionId gets its own isolated ChatNotifier instance via the
/// .family provider — no shared mutable state across sessions.
class ChatNotifier extends StateNotifier<ChatState> {
  ChatNotifier({
    required this.sdkClient,
    required this.websocket,
    required this.ttsNotifier,
    required this.sessionId,
  })  : super(const ChatState()) {
    _initWebSocket();
    // Auto-load messages on creation — the family provider is created
    // on first watch, so this happens when the UI first references the
    // session's chat state.
    _autoLoadMessages();
  }

  final SdkApiClient sdkClient;
  final WebSocketService websocket;
  final TtsNotifier ttsNotifier;
  final String sessionId;
  StreamSubscription<Map<String, dynamic>>? _wsChatSubscription;
  StreamSubscription<Map<String, dynamic>>? _progressSubscription;
  int _loadGeneration = 0;

  /// Prevents duplicate message sends from rapid button taps
  bool _isSending = false;

  /// Guard against setState after dispose
  bool _disposed = false;

  /// Text of the most recent send that failed, kept for the error
  /// banner's retry affordance. Null when there is nothing to retry.
  String? _lastFailedSend;

  /// Timer to reset _isSending flag if it gets stuck (safety mechanism)
  Timer? _sendingTimeoutTimer;

  /// Fallback timer to clear isAgentProcessing if no WS event arrives
  /// after an HTTP response that included a synchronous reply.
  Timer? _processingFallbackTimer;

  /// Maximum time to wait for send to complete before auto-resetting
  static const _sendingTimeout = Duration(seconds: 60);

  /// Initialize WebSocket connection and subscribe to chat messages
  void _initWebSocket() {
    websocket.connect();
  }

  /// Auto-load messages on provider creation. Separated from the
  /// constructor body so it can be async without blocking construction.
  Future<void> _autoLoadMessages() async {
    await loadMessages();
  }

  /// Load chat history for this session and subscribe to updates.
  /// sessionId is fixed at construction time via the .family provider.
  Future<void> loadMessages() async {
    final generation = ++_loadGeneration;

    // Clear messages while loading
    state = const ChatState(
      messages: [],
      isLoading: true,
      error: null,
    );

    // Cancel any existing WS subscription before the HTTP fetch
    _wsChatSubscription?.cancel();
    _wsChatSubscription = null;
    _progressSubscription?.cancel();
    _progressSubscription = null;

    // Fetch messages from the HTTP API
    try {
      final rawMessages = await sdkClient.getMessages(sessionId);
      if (_disposed) return;
      final messages = rawMessages
          .map((m) => ChatMessage.fromBackendMessage(m))
          .toList(growable: false);
      if (_disposed || generation != _loadGeneration) return;
      state = ChatState(
        messages: messages,
        isLoading: false,
      );
    } catch (e) {
      if (_disposed) return;
      state = ChatState(
        messages: [],
        isLoading: false,
        error: e.toString(),
      );
    }

    if (_disposed || generation != _loadGeneration) return;

    // Set up WS subscription AFTER the HTTP fetch completes
    _wsChatSubscription = websocket.subscribeToChat(sessionId).listen((message) {
      addStreamMessage(message);
    });

    // Subscribe to agent progress for this session
    _progressSubscription = websocket.subscribeToAgentProgress(sessionId).listen((message) {
      if (_disposed) return;
      final confirmation = _extractConfirmationRequest(message);
      if (confirmation != null) {
        state = state.copyWith(pendingConfirmation: confirmation);
        return;
      }
      final progress = AgentProgress.fromJson(message);
      state = state.copyWith(currentProgress: progress);
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

  /// Send a multimodal message with structured content parts.
  ///
  /// Mirrors [sendMessage] but routes through
  /// [SdkApiClient.sendChatMessageWithParts] so the backend receives the
  /// `parts` array alongside the text fallback.  Used by the chat input
  /// when the user has attached images.
  Future<void> sendMessageWithParts({
    required String sessionId,
    required String text,
    required List<Map<String, dynamic>> parts,
    String? agentId,
  }) async {
    await _doSend(
      sessionId: sessionId,
      text: text,
      agentId: agentId,
      endpoint: _SendEndpoint.normal,
      parts: parts,
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

  // ---- Steer mode (Ctrl+S, TUI parity) ----
  // Mirrors TUI chat.go steerMode: when armed and the agent is active, the
  // next sent message routes to the steer queue instead of normal chat.

  bool _steerModeArmed = false;

  /// Whether steer mode is currently armed for this session.
  bool get steerModeArmed => _steerModeArmed;

  /// Toggle steer-mode arming. Returns the new state. Arming is only
  /// meaningful while an agent turn is in flight; the UI surfaces the
  /// resulting status either way (TUI shows "steer mode: on/off").
  bool toggleSteerMode() {
    _steerModeArmed = !_steerModeArmed;
    return _steerModeArmed;
  }

  /// Disarm steer mode (e.g. after a steer message is sent or a turn ends).
  void disarmSteerMode() {
    _steerModeArmed = false;
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
    List<Map<String, dynamic>>? parts,
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

    // Remember the send for retry; cleared on success.
    _lastFailedSend = text;
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
      isAgentProcessing: true,
      error: null,
      thinkingStartedAt: DateTime.now(),
    );

    try {
      Map<String, dynamic>? chatResp;
      switch (endpoint) {
        case _SendEndpoint.normal:
          if (parts != null && parts.isNotEmpty) {
            chatResp = await sdkClient.sendChatMessageWithParts(
              message: text,
              conversationId: sessionId,
              agentId: agentId,
              parts: parts,
            );
          } else {
            chatResp = await sdkClient.sendChatMessage(
              message: text,
              conversationId: sessionId,
              agentId: agentId,
            );
          }
        case _SendEndpoint.steer:
          await sdkClient.sendSteerMessage(
            message: text,
            conversationId: sessionId,
            source: 'flutter_ui',
          );
        case _SendEndpoint.followUp:
          await sdkClient.sendFollowUpMessage(
            message: text,
            conversationId: sessionId,
            source: 'flutter_ui',
          );
      }

      if (_disposed) return;

      // Check for agent-side errors in the response body (LLM failures, etc.)
      if (chatResp != null && chatResp['error'] != null) {
        final errorMsg = chatResp['error'].toString();
        // Add error as a system message so it's visible in the chat history
        final errMessage = ChatMessage(
          id: 'error_${DateTime.now().millisecondsSinceEpoch}',
          role: 'system',
          content: errorMsg,
          timestamp: DateTime.now(),
        );
        state = ChatState(
          messages: [...state.messages, errMessage],
          isLoading: false,
          isAgentProcessing: false,
          error: errorMsg,
          thinkingStartedAt: null,
        );
      } else {
        // The HTTP response body may contain a synchronous reply, but we do
        // NOT add it as a chat message here. The daemon publishes the
        // assistant reply via the chat_message bus topic (WS push), which is
        // the single source of truth for assistant messages. Adding the HTTP
        // reply here would duplicate the WS event.
        //
        // The HTTP response is used only for:
        // - Error handling (above)
        // - Transitioning from "loading history" to "waiting for agent"
        //
        // Keep isAgentProcessing=true so the progress indicator stays
        // visible until the WS chat_message event arrives and resolves
        // the turn via addStreamMessage.
        _lastFailedSend = null;
        state = ChatState(
          messages: state.messages,
          isLoading: false,
          isAgentProcessing: true,
          currentProgress: state.currentProgress,
        );
        // Safety net: if no WS chat_message event arrives within a
        // reasonable window (e.g. daemon didn't broadcast), clear the
        // processing state so the UI doesn't stay stuck on "thinking".
        _processingFallbackTimer?.cancel();
        _processingFallbackTimer = Timer(const Duration(seconds: 30), () {
          _processingFallbackTimer = null;
          if (!_disposed && state.isAgentProcessing) {
            state = state.copyWith(
              isAgentProcessing: false,
              thinkingStartedAt: null,
            );
          }
        });
      }
    } catch (e) {
      if (_disposed) return;
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
        isAgentProcessing: false,
        error: errorStr,
        thinkingStartedAt: null,
      );
    } finally {
      _sendingTimeoutTimer?.cancel();
      _sendingTimeoutTimer = null;
      _isSending = false;
    }
  }

  /// Re-send the most recent failed message. No-op when the last send
  /// succeeded (or there was no send). Used by the error banner's
  /// retry affordance.
  Future<void> retryLastSend({String? agentId}) async {
    final text = _lastFailedSend;
    if (text == null || text.isEmpty) return;
    await sendMessage(sessionId: sessionId, text: text, agentId: agentId);
  }

  /// Add a chat message from websocket stream
  void addStreamMessage(Map<String, dynamic> data) {
    try {
      // Defense-in-depth: ignore messages for a different session. With
      // the .family provider each session has its own WS subscription
      // filtered by sessionId, but this guard catches any edge cases.
      final msgSessionId = data['session_id'] as String?;
      if (msgSessionId != null && msgSessionId != sessionId) {
        return;
      }

      // Destructive-tool confirmation requests are detected before the
      // normal chat-message handling so the UI can render
      // DestructiveConfirmationDialog instead of treating the payload as a
      // regular assistant message.
      final confirmation = _extractConfirmationRequest(data);
      if (confirmation != null) {
        state = state.copyWith(pendingConfirmation: confirmation);
        return;
      }

      // Handle system/non-chat messages (token budget, errors, etc.)
      final messageType = data['type'] as String?;
      if (messageType == 'non-chat' || messageType == 'system' || messageType == 'error') {
        final contentText = data['content'] is String
            ? data['content'] as String
            : (data['message'] is String ? data['message'] as String : 'System notification');
        final systemMessage = ChatMessage(
          id: 'system_${DateTime.now().millisecondsSinceEpoch}',
          role: 'system',
          content: contentText,
          timestamp: DateTime.now(),
        );
        _processingFallbackTimer?.cancel();
        _processingFallbackTimer = null;
        state = ChatState(
          messages: [...state.messages, systemMessage],
          isLoading: false,
          isAgentProcessing: false,
          error: systemMessage.content,
          thinkingStartedAt: null,
        );
        return;
      }

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

      // When an assistant message arrives, the agent has finished producing
      // its response — stop the processing indicator.  User/system messages
      // don't signal completion.
      final newIsAgentProcessing =
          (message.role == 'assistant' && message.content.isNotEmpty)
              ? false
              : state.isAgentProcessing;

      // Cancel the fallback timer — the WS event drove the state transition.
      if (!newIsAgentProcessing) {
        _processingFallbackTimer?.cancel();
        _processingFallbackTimer = null;
      }

      state = state.copyWith(
        messages: newMessages,
        isAgentProcessing: newIsAgentProcessing,
        thinkingStartedAt: newIsAgentProcessing ? state.thinkingStartedAt : null,
      );
    } catch (e) {
      final errorMessage = ChatMessage(
        id: 'error_${DateTime.now().millisecondsSinceEpoch}',
        role: 'system',
        content: 'Failed to process message: $e',
        timestamp: DateTime.now(),
      );
      _processingFallbackTimer?.cancel();
      _processingFallbackTimer = null;
      state = ChatState(
        messages: [...state.messages, errorMessage],
        isLoading: false,
        isAgentProcessing: false,
        error: e.toString(),
        thinkingStartedAt: null,
      );
    }
  }

  /// Resolve the current pending confirmation request by re-invoking the
  /// destructive tool with confirmed=true or declined=true.  Routes to the
  /// HTTP endpoint matching the confirmation's `action` field.
  ///
  /// Called by the UI after the user taps confirm/cancel in
  /// DestructiveConfirmationDialog.  Clears `pendingConfirmation`
  /// regardless of outcome so the dialog closes.
  Future<void> resolveConfirmation(bool confirmed) async {
    final payload = state.pendingConfirmation;
    if (payload == null) return;

    // Clear the dialog first so repeated taps don't fire duplicate calls.
    state = state.copyWith(pendingConfirmation: null);

    final action = payload['action'] as String? ?? '';
    final details = (payload['details'] as Map<String, dynamic>?) ?? const {};
    try {
      switch (action) {
        case 'mark_superseded':
          final oldId = details['old_id'] as String? ?? '';
          final newId = details['new_id'] as String? ?? '';
          if (confirmed && oldId.isNotEmpty && newId.isNotEmpty) {
            await sdkClient.markSuperseded(
              oldId: oldId,
              newId: newId,
              confirmed: true,
            );
          } else {
            await sdkClient.markSuperseded(
              oldId: oldId,
              newId: newId,
              confirmed: false,
            );
          }
        case 'mark_resolved':
          final id = details['prediction_id'] as String? ?? '';
          final outcome = details['outcome'] as String? ?? '';
          if (id.isNotEmpty) {
            await sdkClient.markResolved(
              predictionId: id,
              outcome: outcome,
              confirmed: confirmed,
            );
          }
        case 'record_review':
          final id = details['decision_id'] as String? ?? '';
          final outcome = details['actual_outcome'] as String? ?? '';
          if (id.isNotEmpty) {
            await sdkClient.recordDecisionReview(
              decisionId: id,
              actualOutcome: outcome,
              confirmed: confirmed,
            );
          }
        case 'reject_claim':
          final id = details['claim_id'] as String? ?? details['id'] as String? ?? '';
          if (id.isNotEmpty) {
            await sdkClient.rejectClaim(id: id, confirmed: confirmed);
          }
        case 'purge_auto_claims':
          // Bulk destructive action — pass through the filter args.
          final body = Map<String, dynamic>.from(details);
          body['confirmed'] = confirmed;
          await sdkClient.purgeAutoClaims(body: body);
        default:
          debugPrint('resolveConfirmation: unknown action "$action"');
      }
    } catch (e) {
      state = state.copyWith(error: 'confirmation $action failed: $e');
    }
  }

  /// Clear error state without removing messages
  void clearError() {
    state = state.copyWith(error: null);
  }

  /// Clear all messages
  void clearMessages() {
    state = const ChatState();
  }

  @override
  void dispose() {
    _disposed = true;
    _sendingTimeoutTimer?.cancel();
    _sendingTimeoutTimer = null;
    _processingFallbackTimer?.cancel();
    _processingFallbackTimer = null;
    _wsChatSubscription?.cancel();
    _wsChatSubscription = null;
    _progressSubscription?.cancel();
    _progressSubscription = null;
    websocket.unsubscribeFromChat(sessionId);
    super.dispose();
  }
}

/// Chat provider — keyed by sessionId so each session has its own
/// isolated ChatNotifier with its own state, WS subscription, and
/// progress subscription. This eliminates the cross-session race
/// condition where responses from one session appeared in another.
final chatProvider =
    StateNotifierProvider.family<ChatNotifier, ChatState, String>((ref, sessionId) {
  final client = ref.watch(sdkClientProvider);
  final websocket = ref.watch(websocketProvider);
  final ttsNotifier = ref.read(ttsProvider.notifier);
  return ChatNotifier(
    sdkClient: client,
    websocket: websocket,
    ttsNotifier: ttsNotifier,
    sessionId: sessionId,
  );
});

/// Current session ID provider
final currentSessionIdProvider = StateProvider<String?>((ref) => null);
