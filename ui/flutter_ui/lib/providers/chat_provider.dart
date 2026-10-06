import 'dart:async';
import 'dart:convert';

/// Timeout for _isSending flag to prevent permanent lockout

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../core/client_config_parse.dart';
import '../models/api_models.dart';
import '../models/ws_events.dart' show TurnTerminalEvent;
import '../services/sdk_client.dart';
import '../services/websocket_service.dart' show WebSocketService;
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
  if (result is Map<String, dynamic> &&
      result['requires_confirmation'] == true) {
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

/// Default liveness timeout for pending async turns (async-turn-migration
/// leaf 05): a pending turn with no terminal event for this long is marked
/// stalled. Stalled is NOT terminal — a late turn.terminal event still
/// renders the reply and clears the stalled state.
///
/// Source of truth: `chat.liveness_timeout_seconds` in client.json5
/// (internal/tui/config.go ChatConfig, default 120, explicit 0 disables the
/// stalled check — parity with the TUI at internal/tui/models/turn.go). The
/// configured value is fetched best-effort from GET /api/v1/config/client
/// by [ChatNotifier._loadLivenessTimeout]; this constant is the fallback
/// while the fetch is in flight or when the daemon is unreachable.
const Duration kTurnLivenessTimeout = Duration(seconds: 120);

const _unset = Object();
const _progressUnset = Object();
const _confirmUnset = Object();
const _thinkingUnset = Object();

/// Send endpoint type — distinct route for normal, steer, and follow-up messages.
enum _SendEndpoint { normal, steer, followUp }

/// Lifecycle state of a submitted async turn (leaf 05 Task 3):
/// pending → progress → terminal. `stalled` and `parked` are overlays,
/// not terminal states — a late terminal event still resolves the turn.
enum PendingTurnStatus { pending, progress, stalled, parked, terminal }

/// A submitted turn tracked from submit-ack to terminal event.
class PendingTurn {
  final String turnId;
  final String conversationId;
  final String sessionId;
  final DateTime startedAt;
  final DateTime lastProgressAt;
  final PendingTurnStatus status;

  /// Progress text from the latest agent_progress event (status=progress),
  /// the stalled label (status=stalled), or the parked reason
  /// (status=parked). Empty for pending/terminal.
  final String progressText;

  const PendingTurn({
    required this.turnId,
    required this.conversationId,
    required this.sessionId,
    required this.startedAt,
    required this.lastProgressAt,
    this.status = PendingTurnStatus.pending,
    this.progressText = '',
  });

  PendingTurn copyWith({
    PendingTurnStatus? status,
    String? progressText,
    DateTime? lastProgressAt,
  }) {
    return PendingTurn(
      turnId: turnId,
      conversationId: conversationId,
      sessionId: sessionId,
      startedAt: startedAt,
      lastProgressAt: lastProgressAt ?? this.lastProgressAt,
      status: status ?? this.status,
      progressText: progressText ?? this.progressText,
    );
  }
}

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

  /// Submitted turns still awaiting their terminal event, keyed by turn_id
  /// (async-turn-migration leaf 05). Concurrent turns track independently.
  final Map<String, PendingTurn> pendingTurns;

  /// Terminal events that arrived BEFORE the submit ack registered their
  /// turn (F19): the HTTP submit reply races the WS relay. Buffered
  /// events are consumed on ack registration (see ChatNotifier) and never
  /// dropped. This is ephemeral per-notifier state, not UI state.
  final Map<String, TurnTerminalEvent> earlyTerminals;

  /// Count of late failed turns that landed while the user was looking at a
  /// DIFFERENT conversation (leaf 05 Task 4). The sessions list renders a
  /// badge so the user discovers the failure contextually. Reset when this
  /// session's chat view is opened.
  final int lateFailureCount;

  const ChatState({
    this.messages = const [],
    this.isLoading = false,
    this.isAgentProcessing = false,
    this.error,
    this.currentProgress,
    this.thinkingStartedAt,
    this.pendingConfirmation,
    this.pendingTurns = const {},
    this.earlyTerminals = const {},
    this.lateFailureCount = 0,
  });

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? isLoading,
    bool? isAgentProcessing,
    Object? error = _unset,
    Object? currentProgress = _progressUnset,
    Object? pendingConfirmation = _confirmUnset,
    Object? thinkingStartedAt = _thinkingUnset,
    Map<String, PendingTurn>? pendingTurns,
    int? lateFailureCount,
  }) {
    // Limit messages to prevent memory leaks
    List<ChatMessage> limitedMessages = messages ?? this.messages;
    if (limitedMessages.length > _maxMessages) {
      // Keep only the most recent messages
      limitedMessages = limitedMessages.sublist(
        limitedMessages.length - _maxMessages,
      );
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
      pendingTurns: pendingTurns ?? this.pendingTurns,
      lateFailureCount: lateFailureCount ?? this.lateFailureCount,
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
  }) : super(const ChatState()) {
    _initWebSocket();
    // Fetch the configured stalled-turn window from client.json5 (TUI
    // parity); best-effort — failures keep the built-in default.
    _loadLivenessTimeout();
    // Auto-load messages on creation — the family provider is created
    // on first watch, so this happens when the UI first references the
    // session's chat state.
    _autoLoadMessages();
  }

  /// The stalled-turn window in effect, in seconds. Mirrors
  /// `chat.liveness_timeout_seconds` from client.json5 (fetched via
  /// GET /api/v1/config/client); 0 disables the stalled check (TUI
  /// semantics), negative means "fetch has not resolved yet".
  int _livenessTimeoutSeconds = -1;

  /// Fetch `chat.liveness_timeout_seconds` from GET /api/v1/config/client.
  /// Best-effort: any failure (offline, daemon older than the route,
  /// malformed JSON5) silently keeps [kTurnLivenessTimeout].
  Future<void> _loadLivenessTimeout() async {
    try {
      final raw = await sdkClient.getClientConfig();
      if (_disposed) return;
      final decoded = parseClientConfig(raw);
      final chat = decoded['chat'];
      if (chat is! Map) return;
      final v = chat['liveness_timeout_seconds'];
      if (v is int && v >= 0) {
        _livenessTimeoutSeconds = v;
      }
    } catch (_) {
      // Keep the default — liveness is an optimization, not a gate.
    }
  }

  /// The liveness timeout currently in effect: the configured
  /// `chat.liveness_timeout_seconds` once the client.json5 fetch resolves
  /// (0 = disabled, per TUI semantics), else the [kTurnLivenessTimeout]
  /// fallback.
  Duration get _effectiveLivenessTimeout {
    final s = _livenessTimeoutSeconds;
    if (s < 0) return kTurnLivenessTimeout;
    if (s == 0) return const Duration(days: 365); // effectively disabled
    return Duration(seconds: s);
  }

  /// Arm the per-turn liveness timer using the effective timeout.
  void _armLivenessTimer(String turnId) {
    if (_livenessTimeoutSeconds == 0) return; // disabled by config
    _livenessTimers[turnId] = Timer(_effectiveLivenessTimeout, () {
      _markStalled(turnId);
    });
  }

  final SdkApiClient sdkClient;
  final WebSocketService websocket;
  final TtsNotifier ttsNotifier;
  final String sessionId;
  StreamSubscription<Map<String, dynamic>>? _wsChatSubscription;
  StreamSubscription<Map<String, dynamic>>? _progressSubscription;
  StreamSubscription<Map<String, dynamic>>? _turnTerminalSubscription;

  /// Per-pending-turn liveness timers (leaf 05): fire once per turn after
  /// the effective liveness timeout (see [_armLivenessTimer]) to mark the
  /// turn stalled (NOT terminal).
  final Map<String, Timer> _livenessTimers = {};
  int _loadGeneration = 0;

  /// Prevents duplicate message sends from rapid button taps
  bool _isSending = false;

  /// Guard against setState after dispose
  bool _disposed = false;

  /// Text of the most recent send that failed, kept for the error
  /// banner's retry affordance. Null when there is nothing to retry.
  String? _lastFailedSend;

  /// Offset (in full-session message index space) of the oldest loaded
  /// message. 0 when everything is loaded or the session is short.
  int _oldestLoadedOffset = 0;

  /// Whether older history pages exist above the current window.
  bool _hasMoreHistory = false;
  bool _isLoadingOlder = false;

  /// Whether older messages exist that [loadOlderMessages] can fetch.
  bool get hasMoreHistory => _hasMoreHistory;
  bool get isLoadingOlder => _isLoadingOlder;

  /// Timer to reset _isSending flag if it gets stuck (safety mechanism)
  Timer? _sendingTimeoutTimer;

  /// Bounded fallback that stops the thinking indicator when the turn's
  /// terminal event never arrives.
  ///
  /// ARMED: whenever the indicator is armed with a turn that is expected to
  /// resolve via `turn.terminal` (submit/steer/follow-up ack), a timer is set
  /// for [kProcessingFallbackGrace] — comfortably longer than a healthy turn's
  /// reply, short enough that a dropped socket cannot leave the GUI spinning
  /// forever. This is the only non-terminal path that clears the indicator:
  /// the primary path is `_consumeTurnTerminal`, and the fallback exists for
  /// the case it never fires (parked-then-dropped, dropped socket, daemon
  /// restart). On fire it clears the indicator but keeps `pendingTurns` — a
  /// late terminal still renders the reply (the liveness watchdog already
  /// handles that class), so this degrades the indicator, never the transcript.
  ///
  /// It is CANCELLED by every path that clears the indicator for a real
  /// reason: a consumed terminal, a system/error frame, a rejected submit, a
  /// send failure, and dispose.
  Timer? _processingFallbackTimer;

  /// Grace period before the bounded fallback clears the thinking indicator.
  static const Duration kProcessingFallbackGrace = Duration(seconds: 45);

  /// Arm the bounded fallback for a turn whose terminal event has not landed.
  ///
  /// Re-arming is idempotent: a turn already tracked keeps its original
  /// deadline unless a NEW turn is armed while one is outstanding (that is the
  /// real stall case the fallback exists for).
  void _armProcessingFallback() {
    if (!(_processingFallbackTimer?.isActive ?? false)) {
      _processingFallbackTimer = Timer(kProcessingFallbackGrace, () {
        _processingFallbackTimer = null;
        _clearProcessingIndicator();
      });
    }
  }

  /// Stop the thinking indicator without touching pending turns.
  ///
  /// Used by the bounded fallback and by the error/system paths: the
  /// indicator is a view of the agent's activity, while [ChatState.pendingTurns]
  /// is the truth about which turns are still outstanding. A late terminal
  /// still renders.
  void _clearProcessingIndicator() {
    if (!state.isAgentProcessing && state.thinkingStartedAt == null) return;
    state = state.copyWith(
      isAgentProcessing: false,
      thinkingStartedAt: null,
      currentProgress: null,
    );
  }

  /// Cancel the bounded fallback — a real event drove the transition.
  void _cancelProcessingFallback() {
    _processingFallbackTimer?.cancel();
    _processingFallbackTimer = null;
  }

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

    // Clear messages while loading. copyWith (NOT a fresh ChatState): the
    // F20 wipe class — a reload mid-turn must not drop pendingTurns, per
    // the copyWith contract below (scopes-2 audit MED, 2026-09-18).
    state = state.copyWith(messages: const [], isLoading: true, error: null);

    // Re-arm the WS subscriptions BEFORE the HTTP fetch, not after (L3).
    //
    // Cancelling first and re-subscribing after `await getMessagesPage` left a
    // window — the length of the HTTP round trip — in which mid-turn deltas
    // (chat_message relays and, more importantly, the turn.terminal that ends
    // the turn) were neither delivered nor queued: the old listener was gone
    // and the new one did not exist yet. A turn whose terminal landed in that
    // window false-stalled until the liveness watchdog fired 120 s later.
    //
    // Re-subscribing first means frames that arrive DURING the fetch are
    // delivered normally; the fetch result is applied afterwards, and the
    // generation check below drops a result from a superseded load.
    _ensureSubscriptions();

    // Fetch messages from the HTTP API. The initial load grabs the LAST
    // page (most recent window) so huge sessions don't fetch everything;
    // older pages arrive via [loadOlderMessages] when the user scrolls up.
    const pageSize = 200;
    var totalCount = 0;
    try {
      final page = await sdkClient.getMessagesPage(
        sessionId,
        offset: 0,
        limit: pageSize,
      );
      if (_disposed) return;
      totalCount = page.total;
      List<ChatMessage> messages;
      if (totalCount > page.messages.length) {
        // Session has more history than one page: fetch the most recent
        // window. The API pages from the oldest message (ORDER BY id), so
        // offset = total - pageSize yields the newest pageSize messages.
        final offset = totalCount > pageSize ? totalCount - pageSize : 0;
        final recent = await sdkClient.getMessagesPage(
          sessionId,
          offset: offset,
          limit: pageSize,
        );
        if (_disposed) return;
        messages = recent.messages
            .map((m) => ChatMessage.fromBackendMessage(m))
            .toList(growable: true);
        _oldestLoadedOffset = offset;
      } else {
        messages = page.messages
            .map((m) => ChatMessage.fromBackendMessage(m))
            .toList(growable: true);
        _oldestLoadedOffset = 0;
      }
      _hasMoreHistory = _oldestLoadedOffset > 0;
      if (_disposed || generation != _loadGeneration) return;
      // copyWith: keep pendingTurns — a history reload must not orphan
      // in-flight turns (scopes-2 audit MED, 2026-09-18).
      state = state.copyWith(messages: messages, isLoading: false);
    } catch (e) {
      if (_disposed) return;
      state = state.copyWith(
        messages: [],
        isLoading: false,
        error: e.toString(),
      );
    }

    }

  /// Arm (or keep) this notifier's three per-session WS subscriptions:
  /// `chat_message` relays, `agent_progress`, and the `turn.terminal` frames
  /// that ride the progress channel.
  ///
  /// Idempotent and safe to call on every [loadMessages]: it re-arms a
  /// missing subscription and leaves a live one alone. Calling it BEFORE the
  /// history fetch (rather than after) is what closes the L3 delivery window
  /// — there is never a moment where the chat/progress/terminal streams of a
  /// reloading session are unsubscribed.
  void _ensureSubscriptions() {
    if (_disposed) return;

    _wsChatSubscription ??= websocket.subscribeToChat(sessionId).listen((
      message,
    ) {
      addStreamMessage(message);
    });

    _progressSubscription ??= websocket
        .subscribeToAgentProgress(sessionId)
        .listen((message) {
          if (_disposed) return;
          final confirmation = _extractConfirmationRequest(message);
          if (confirmation != null) {
            state = state.copyWith(pendingConfirmation: confirmation);
            return;
          }
          final progress = AgentProgress.fromJson(message);
          state = state.copyWith(currentProgress: progress);
          // Liveness refresh (scopes-2 audit MED, 2026-09-18): feed every
          // progress frame into the pending-turn trackers — parity with the
          // TUI's DeliverTurnProgress (app.go). Without this, GUI turns
          // false-stall at the fixed 120s timer even while visibly
          // progressing. Match by turn id when the frame carries one, else
          // refresh every pending turn on this session (the frame is
          // session-scoped by the subscription filter).
          final frameTurnId = message['turn_id'] as String?;
          state.pendingTurns.forEach((pendingId, pt) {
            if (frameTurnId == null || pt.turnId == frameTurnId) {
              noteTurnProgress(
                pt.turnId,
                progress.stage.isNotEmpty
                    ? progress.stage
                    : (progress.message.isNotEmpty
                          ? progress.message
                          : 'working'),
              );
            }
          });
        });

    // turn.terminal relays (async-turn-migration leaf 05) arrive classified as
    // agent_progress; the subscription filters them by payload shape. It is a
    // SEPARATE stream from agent progress, so it carries its own hold on the
    // session's progress-channel filter (see WebSocketService).
    _turnTerminalSubscription ??= websocket
        .subscribeToTurnTerminal(sessionId)
        .listen((message) {
          if (_disposed) return;
          _handleTurnTerminal(TurnTerminalEvent.fromParse(message));
        });
  }

  /// Handle a turn.terminal relay for a tracked pending turn (leaf 05
  /// Tasks 3/4): render the reply or error bubble, clear the pending-turn
  /// entry and its liveness timer, and bump the late-failure indicator when
  /// the failure landed on a conversation the user has navigated away from.
  ///
  /// F19: an event for an UNTRACKED turn may arrive before its submit ack
  /// registers the turn (the HTTP submit reply races the WS relay). Such
  /// events are buffered in the notifier's ephemeral early-event map and
  /// consumed on ack registration — never dropped.
  void _handleTurnTerminal(TurnTerminalEvent event) {
    final turn = state.pendingTurns[event.turnId];
    // Foreign/untracked turn: buffer it — the ack may still be in flight, OR
    // the turn was tracked before a `/clear` and its terminal landed after
    // the conversation was wiped (see [clearMessages] / _drainEarlyTerminals).
    if (turn == null) {
      _earlyTerminals[event.turnId] = event;
      _drainEarlyTerminals();
      return;
    }
    _consumeTurnTerminal(event, turn);
  }

  /// Ephemeral early-event buffer (F19): turn.terminal events that raced
  /// their submit ack. Consumed (and drained) on ack registration, and by
  /// [_drainEarlyTerminals] for any turn that became tracked by another route.
  final Map<String, TurnTerminalEvent> _earlyTerminals = {};

  /// Apply every buffered terminal whose turn is now tracked.
  ///
  /// [clearMessages] preserves `pendingTurns` across a `/clear` (TUI parity —
  /// the TUI's ClearConversation never forgets in-flight turns), so a
  /// terminal that lands after the wipe still has a tracked turn and must
  /// render. Before this drain existed, such a terminal sat in [_earlyTerminals]
  /// forever: the submit-ack registration it was buffered for had already
  /// happened, so nothing would ever consume it and the reply was silently
  /// lost.
  void _drainEarlyTerminals() {
    if (_earlyTerminals.isEmpty) return;
    final ready = <String, TurnTerminalEvent>{};
    _earlyTerminals.removeWhere((turnId, event) {
      if (!state.pendingTurns.containsKey(turnId)) return false;
      ready[turnId] = event;
      return true;
    });
    for (final event in ready.values) {
      final turn = state.pendingTurns[event.turnId];
      if (turn == null) continue;
      _consumeTurnTerminal(event, turn);
    }
  }

  /// Consume any early-buffered terminal event for [turnId], if present.
  /// Called on ack registration so a fast daemon's terminal is applied
  /// immediately after the turn is tracked.
  void _consumeEarlyTerminal(String turnId) {
    final early = _earlyTerminals.remove(turnId);
    if (early == null) return;
    final turn = state.pendingTurns[turnId];
    if (turn == null) return;
    _consumeTurnTerminal(early, turn);
  }

  /// Apply a terminal event to its tracked pending turn (the original
  /// leaf 05 handler body, extracted so both the live-relay path and the
  /// F19 early-buffer path share it).
  void _consumeTurnTerminal(TurnTerminalEvent event, PendingTurn turn) {
    final turns = Map<String, PendingTurn>.from(state.pendingTurns);
    _cancelLivenessTimer(event.turnId);

    // Parked (quota) turns are NOT resolved: the daemon will resume the
    // turn automatically and a later terminal event will arrive. Downgrade
    // the pending entry to the parked state with honest, non-error text.
    //
    // The turn is still live, so the thinking indicator stays armed — but the
    // bounded fallback must not fire while the daemon owns the turn: the
    // resume can take arbitrarily long (quota parks for hours). Parked is an
    // explicit daemon state, so disarm the fallback and let the resume's
    // terminal (or the liveness watchdog, which re-arms on progress) drive
    // the next transition.
    if (event.status == TurnTerminalEvent.statusParked) {
      final parked = turn.copyWith(
        status: PendingTurnStatus.parked,
        progressText: 'waiting for provider quota — will resume automatically',
      );
      turns[event.turnId] = parked;
      _cancelProcessingFallback();
      state = state.copyWith(pendingTurns: turns);
      return;
    }

    turns.remove(event.turnId);

    // The terminal is the turn's real end signal, so the bounded fallback has
    // done its job: cancel it here rather than leaving it armed.
    _cancelProcessingFallback();

    switch (event.status) {
      case TurnTerminalEvent.statusFailed:
      case TurnTerminalEvent.statusTimeout:
        // Error bubble with the daemon's error text (timeout falls back to
        // a fixed sentence when the daemon sent no error field).
        final errorText = event.error.isNotEmpty
            ? event.error
            : 'turn failed: ${event.status}';
        _appendErrorBubble(errorText);
        // The failure may have landed minutes later while the user views a
        // different conversation — bump the session's discoverable badge.
        state = state.copyWith(
          lateFailureCount: state.lateFailureCount + 1,
          pendingTurns: turns,
          isAgentProcessing: turns.isEmpty ? false : state.isAgentProcessing,
          thinkingStartedAt: turns.isEmpty ? null : state.thinkingStartedAt,
        );
        return;
      case TurnTerminalEvent.statusCompleted:
        // Render the reply as an assistant bubble unless the reply already
        // arrived via the regular chat_message WS push (dedupe by content
        // against the most recent assistant bubble).
        if (event.reply.isNotEmpty &&
            !_lastAssistantContentEquals(event.reply)) {
          final reply = ChatMessage(
            id: 'turn_${event.turnId}',
            role: 'assistant',
            content: event.reply,
            timestamp: DateTime.now(),
            sessionId: sessionId,
          );
          final newMessages = [...state.messages, reply];
          ttsNotifier.speak(event.reply);
          state = state.copyWith(
            messages: newMessages,
            pendingTurns: turns,
            isAgentProcessing: turns.isEmpty ? false : state.isAgentProcessing,
            thinkingStartedAt: turns.isEmpty ? null : state.thinkingStartedAt,
          );
          return;
        }
        break;
      default:
        break;
    }

    state = state.copyWith(
      pendingTurns: turns,
      isAgentProcessing: turns.isEmpty ? false : state.isAgentProcessing,
      thinkingStartedAt: turns.isEmpty ? null : state.thinkingStartedAt,
    );
  }

  /// True when the most recent assistant message already carries [content]
  /// (the reply landed via the regular chat_message WS push before the
  /// terminal event; rendering the terminal reply too would duplicate it).
  bool _lastAssistantContentEquals(String content) {
    for (final m in state.messages.reversed) {
      if (m.role == 'assistant') return m.content == content;
      if (m.role == 'user') return false;
    }
    return false;
  }

  /// Append a system error bubble and surface the text in the error slot.
  void _appendErrorBubble(String errorText) {
    final errMessage = ChatMessage(
      id: 'error_${DateTime.now().millisecondsSinceEpoch}',
      role: 'system',
      content: errorText,
      timestamp: DateTime.now(),
    );
    state = state.copyWith(
      messages: [...state.messages, errMessage],
      error: errorText,
    );
  }

  /// Submit a turn via the async endpoint (leaf 05 Task 3). The ack returns
  /// immediately; the reply arrives later via the turn.terminal relay.
  /// Returns the ack so callers can surface a rejection note.
  Future<ChatSubmitAck> submitTurn({
    required String sessionId,
    required String text,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async {
    if (_isSending) {
      return const ChatSubmitAck(
        turnId: '',
        conversationId: '',
        sessionId: '',
        accepted: false,
        note: 'a send is already in flight',
      );
    }
    if (!websocket.isConnected) {
      state = state.copyWith(
        error: 'not connected to daemon — check that meept-daemon is running',
      );
      return const ChatSubmitAck(
        turnId: '',
        conversationId: '',
        sessionId: '',
        accepted: false,
        note: 'not connected to daemon',
      );
    }

    _isSending = true;
    try {
      final ack = await sdkClient.submitTurn(
        message: text,
        conversationId: sessionId,
        agentId: agentId,
        parts: parts,
      );
      if (_disposed) return ack;

      if (!ack.accepted || ack.turnId.isEmpty) {
        state = state.copyWith(error: ack.note);
        return ack;
      }

      // Optimistic user bubble (parity with the sync send path).
      final userMessage = ChatMessage(
        id: DateTime.now().millisecondsSinceEpoch.toString(),
        role: 'user',
        content: text,
        timestamp: DateTime.now(),
        sessionId: sessionId,
      );

      final pending = PendingTurn(
        turnId: ack.turnId,
        conversationId: ack.conversationId,
        sessionId: sessionId,
        startedAt: DateTime.now(),
        lastProgressAt: DateTime.now(),
      );
      final turns = Map<String, PendingTurn>.from(state.pendingTurns);
      turns[ack.turnId] = pending;

      state = state.copyWith(
        messages: [...state.messages, userMessage],
        pendingTurns: turns,
        isAgentProcessing: true,
        thinkingStartedAt: state.thinkingStartedAt ?? DateTime.now(),
      );

      // Liveness watchdog: mark the turn stalled after the timeout. A
      // stalled turn is NOT terminal — a late terminal event still renders
      // the reply and clears the stalled state.
      _armLivenessTimer(ack.turnId);
      // Bounded fallback for the case the terminal never arrives at all
      // (dropped socket, daemon restart, parked-then-dropped): the
      // indicator must not spin forever. `_consumeEarlyTerminal` below
      // cancels it again if the reply was already buffered.
      _armProcessingFallback();
      // F19: a terminal event that raced the ack is applied immediately.
      _consumeEarlyTerminal(ack.turnId);
      return ack;
    } catch (e) {
      if (_disposed) {
        return ChatSubmitAck(
          turnId: '',
          conversationId: '',
          sessionId: '',
          accepted: false,
          note: e.toString(),
        );
      }
      String errorStr;
      if (e is DioException) {
        final code = e.response?.statusCode;
        final url = e.requestOptions.path;
        final method = e.requestOptions.method;
        errorStr = '$method $url -> ${code ?? e.type}';
      } else {
        errorStr = e.toString();
      }
      state = state.copyWith(error: errorStr);
      return ChatSubmitAck(
        turnId: '',
        conversationId: '',
        sessionId: '',
        accepted: false,
        note: errorStr,
      );
    } finally {
      _isSending = false;
    }
  }

  /// Cancel and forget a turn's liveness timer.
  void _cancelLivenessTimer(String turnId) {
    _livenessTimers.remove(turnId)?.cancel();
  }

  /// Mark a pending turn stalled after the liveness timeout. The UI keeps
  /// showing an honest "may still complete" line; the turn stays tracked.
  void _markStalled(String turnId) {
    _livenessTimers.remove(turnId);
    final turn = state.pendingTurns[turnId];
    if (turn == null) return;
    if (turn.status == PendingTurnStatus.parked) return;
    final seconds = DateTime.now().difference(turn.lastProgressAt).inSeconds;
    final turns = Map<String, PendingTurn>.from(state.pendingTurns);
    turns[turnId] = turn.copyWith(
      status: PendingTurnStatus.stalled,
      progressText: 'no progress for ${seconds}s — task may still complete',
    );
    state = state.copyWith(pendingTurns: turns);
  }

  /// Advance a pending turn to the progress state on an ordinary
  /// agent_progress event. Called from the progress subscription path so a
  /// stalled turn that resumes emitting progress goes back to progress
  /// (and re-arms its liveness timer).
  void noteTurnProgress(String turnId, String progressText) {
    final turn = state.pendingTurns[turnId];
    if (turn == null) return;
    _livenessTimers.remove(turnId)?.cancel();
    _armLivenessTimer(turnId);
    final turns = Map<String, PendingTurn>.from(state.pendingTurns);
    turns[turnId] = turn.copyWith(
      status: PendingTurnStatus.progress,
      progressText: progressText,
      lastProgressAt: DateTime.now(),
    );
    state = state.copyWith(pendingTurns: turns);
  }

  /// Reset the late-failure indicator when this conversation's view opens.
  void clearLateFailures() {
    if (state.lateFailureCount == 0) return;
    state = state.copyWith(lateFailureCount: 0);
  }

  /// Test/UI seam: route a terminal event through the same handler the WS
  /// subscription uses. Public so widget tests and the subscription share
  /// one code path.
  void debugHandleTurnTerminal(TurnTerminalEvent event) {
    _handleTurnTerminal(event);
  }

  /// Test seam: simulate a late failure badge bump (the sessions-list
  /// routing logic calls this path in production via the WS subscription).
  void debugNoteLateFailure() {
    state = state.copyWith(lateFailureCount: state.lateFailureCount + 1);
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
  /// Mirrors [sendMessage] but carries the `parts` array alongside the text
  /// fallback so the backend receives structured image content. Used by the
  /// chat input when the user has attached images. Routes through the async
  /// submit endpoint (leaf 05) like [sendMessage].
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
      // copyWith: preserve pendingTurns (F20 regression class guard).
      state = state.copyWith(
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
    // F20: state reconstructions on the send path must preserve
    // state.pendingTurns — copyWith keeps them, but a fresh ChatState(...)
    // would silently drop in-flight turns (and their UI rows).
    state = state.copyWith(
      messages: newMessages,
      isLoading: true,
      isAgentProcessing: true,
      error: null,
      thinkingStartedAt: DateTime.now(),
    );

    // The steer/followup endpoints return void; the normal endpoint exits
    // early via the async submit path, so no response map is carried here.
    try {
      switch (endpoint) {
        case _SendEndpoint.normal:
          // Async-turn migration (leaf 05): normal sends go through
          // POST /api/v1/chat/submit and return immediately. The reply
          // arrives later via the turn.terminal relay; pending/stalled/
          // parked states are tracked in [ChatState.pendingTurns]. The
          // legacy blocking /api/v1/chat is no longer called on this path.
          final ack = await sdkClient.submitTurn(
            message: text,
            conversationId: sessionId,
            agentId: agentId,
            parts: parts,
          );
          if (_disposed) return;
          if (!ack.accepted || ack.turnId.isEmpty) {
            // Submit rejected (validation, dedupe-with-note, etc.) —
            // surface the daemon's note, no pending turn is tracked.
            // F20: copyWith preserves pendingTurns (existing in-flight
            // turns survive a rejected send).
            _lastFailedSend = null;
            _cancelProcessingFallback();
            state = state.copyWith(
              isLoading: false,
              isAgentProcessing: false,
              error: ack.note.isEmpty ? 'submit rejected' : ack.note,
              thinkingStartedAt: null,
            );
            return;
          }
          // Track the turn and keep the sending flag until the terminal
          // event arrives (bounded by the sending timeout guard).
          final pending = PendingTurn(
            turnId: ack.turnId,
            conversationId: ack.conversationId,
            sessionId: sessionId,
            startedAt: DateTime.now(),
            lastProgressAt: DateTime.now(),
          );
          final turns = Map<String, PendingTurn>.from(state.pendingTurns);
          turns[ack.turnId] = pending;
          _armLivenessTimer(ack.turnId);
          // Bounded fallback: if the terminal never arrives the indicator
          // stops itself instead of spinning forever. Cancelled again by
          // `_consumeTurnTerminal` when the reply does land.
          _armProcessingFallback();
          _lastFailedSend = null;
          // F20: copyWith preserves pendingTurns — `turns` already carries
          // the pre-send in-flight turns (copied from state.pendingTurns
          // above) plus this new one.
          state = state.copyWith(
            isLoading: false,
            isAgentProcessing: true,
            currentProgress: state.currentProgress,
            pendingTurns: turns,
            thinkingStartedAt: state.thinkingStartedAt ?? DateTime.now(),
          );
          // F19: a terminal event that raced the ack is applied
          // immediately.
          _consumeEarlyTerminal(ack.turnId);
          return;
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

      // Steer / followup land here (void endpoints — no ack body, no
      // pending-turn lifecycle). The normal endpoint returned earlier via
      // the async submit path. Steer/followup are control-plane calls: the
      // daemon either queues them or errors, and both outcomes surface via
      // existing WS events.
      //
      // The send itself is done here — the daemon's steer/follow-up queue
      // resolves it and the REPLY arrives on the turn/chat WS streams, not in
      // this response. So this path owns the indicator's lifetime: arm the
      // bounded fallback (there is no terminal event of its own to cancel it)
      // and keep the indicator armed only while a turn is genuinely still
      // outstanding. `_doSend` unconditionally set isAgentProcessing on entry
      // (for the steer/follow-up bubbles it appended) and the early return
      // below only cleared isLoading, so a steer that produced no tracked turn
      // left the GUI spinning with no event that could ever stop it.
      //
      // F20: copyWith preserves pendingTurns — steering while a turn is
      // in flight must not drop it.
      _lastFailedSend = null;
      if (state.pendingTurns.isEmpty) {
        // No turn is outstanding: nothing will clear the indicator for us, so
        // the steer's own indicator must be released immediately.
        _cancelProcessingFallback();
        state = state.copyWith(
          isLoading: false,
          isAgentProcessing: false,
          thinkingStartedAt: null,
        );
      } else {
        // A turn is still in flight — keep the indicator and bound it.
        _armProcessingFallback();
        state = state.copyWith(isLoading: false);
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
      // F20: copyWith preserves pendingTurns — a failed send must not
      // drop in-flight turns.
      _cancelProcessingFallback();
      state = state.copyWith(
        isLoading: false,
        isAgentProcessing: false,
        thinkingStartedAt: null,
        error: errorStr,
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

  /// Fetch the next page of OLDER history and prepend it to the state.
  /// Called by the chat list when the user scrolls to the top. No-op while
  /// a fetch is in flight or when no older pages exist. Returns true when
  /// new messages were prepended.
  Future<bool> loadOlderMessages() async {
    if (_isLoadingOlder || !_hasMoreHistory || _disposed) return false;
    _isLoadingOlder = true;
    try {
      const pageSize = 200;
      final offset = (_oldestLoadedOffset - pageSize).clamp(
        0,
        _oldestLoadedOffset,
      );
      if (offset == _oldestLoadedOffset) {
        // Already at the start.
        _hasMoreHistory = false;
        return false;
      }
      final page = await sdkClient.getMessagesPage(
        sessionId,
        offset: offset,
        limit: pageSize,
      );
      if (_disposed) return false;
      final allOlder = page.messages
          .map((m) => ChatMessage.fromBackendMessage(m))
          .toList(growable: false);
      _oldestLoadedOffset = offset;
      if (offset == 0) _hasMoreHistory = false;
      if (allOlder.isEmpty) {
        _hasMoreHistory = false;
        return false;
      }
      // The fetched window may overlap what is already loaded when
      // _oldestLoadedOffset was not page-aligned (initial recent-window
      // loads land mid-page). Drop any messages already present.
      final loadedIds = {for (final m in state.messages) m.id};
      final older = allOlder.where((m) => !loadedIds.contains(m.id)).toList();
      if (older.isEmpty) return false;
      // Prepend without touching isLoading/isAgentProcessing/error.
      state = state.copyWith(messages: [...older, ...state.messages]);
      return true;
    } catch (e) {
      // Scroll-back pagination is best-effort; surface but don't clobber
      // an active turn's error slot.
      debugPrint('[chat_provider] loadOlderMessages failed: $e');
      return false;
    } finally {
      _isLoadingOlder = false;
    }
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
      if (messageType == 'non-chat' ||
          messageType == 'system' ||
          messageType == 'error') {
        final contentText = data['content'] is String
            ? data['content'] as String
            : (data['message'] is String
                  ? data['message'] as String
                  : 'System notification');
        final systemMessage = ChatMessage(
          id: 'system_${DateTime.now().millisecondsSinceEpoch}',
          role: 'system',
          content: contentText,
          timestamp: DateTime.now(),
        );
        _cancelProcessingFallback();
        // copyWith (NOT fresh ChatState): a fresh constructor drops
        // state.pendingTurns — the F20 regression class (scopes-2 audit
        // MED, 2026-09-18). An error frame mid-turn must not erase
        // in-flight turn rows and tracking.
        state = state.copyWith(
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

      // Streaming accumulation (gui-stream-01): an id-less assistant delta is
      // a mid-turn accumulation frame and must APPEND to the in-flight stream
      // bubble (the trailing message when it is also an empty-id assistant
      // bubble), preserving order. Distinct-id replacement (finalized
      // replies, history refreshes) is unchanged.
      final isIdlessAssistantDelta =
          message.id.isEmpty && message.role == 'assistant';
      final last = state.messages.isEmpty ? null : state.messages.last;
      final isStreamBubble =
          last != null && last.id.isEmpty && last.role == 'assistant';

      List<ChatMessage> newMessages;
      if (isIdlessAssistantDelta && isStreamBubble) {
        // Accumulate into the one stream bubble, in order.
        newMessages = [...state.messages];
        newMessages[newMessages.length - 1] = last.copyWith(
          content: last.content + message.content,
        );
      } else if (isIdlessAssistantDelta) {
        // First delta of a stream: open the stream bubble.
        newMessages = [...state.messages, message];
      } else {
        // Replace or update existing message by id if it exists
        final existingIndex = state.messages.indexWhere(
          (m) => m.id == message.id,
        );
        if (existingIndex >= 0) {
          newMessages = [...state.messages];
          newMessages[existingIndex] = message;
        } else {
          newMessages = [...state.messages, message];
        }
      }

      if (newMessages.length > _maxMessages) {
        newMessages = newMessages.sublist(newMessages.length - _maxMessages);
      }

      // Does this chat_message END a turn?  (bughunt wave H3)
      //
      // A `chat_message` frame is NEVER a completion signal on its own, and it
      // must not be treated as one.
      //
      // The old gate here was `role == 'assistant' && id.isNotEmpty &&
      // content.isNotEmpty`, added by c4dc25ee to distinguish a finalized
      // reply from a mid-stream delta. In production that arm is DEAD: the WS
      // relay injects `payload["id"] = msg.ID` into EVERY relayed
      // chat_message (internal/comm/http/server.go, transformBusEventToWS) and
      // the only publisher of the topic (internal/agent/handler.go
      // publishChatMessage) never sets `id`, so `id.isEmpty` is never true and
      // the branch could never fire. The GUI's only non-terminal path for
      // clearing the thinking indicator was therefore unreachable: a missed
      // turn.terminal left the spinner running forever.
      //
      // The real terminal signals, and they are both reachable:
      //  * `turn.terminal`, consumed by [_consumeTurnTerminal] — the
      //    authoritative end-of-turn event. It clears the indicator itself.
      //  * the bounded fallback [_armProcessingFallback] — the honest
      //    backstop for a terminal that never arrives (dropped socket, daemon
      //    restart, parked-then-dropped). Bounded, so it cannot hang.
      //
      // So a chat_message leaves the indicator exactly as it found it. It is
      // a content frame: mid-stream deltas must NOT clear it (that is what
      // c4dc25ee was fixing) and a finalized reply must not be the thing that
      // clears it either — the turn's own terminal owns that.
      //
      // Note also that the reply the terminal carries is deduped against this
      // very bubble by [_lastAssistantContentEquals], so no reply is lost by
      // refusing to read content as a completion signal.
      state = state.copyWith(messages: newMessages);
    } catch (e) {
      final errorMessage = ChatMessage(
        id: 'error_${DateTime.now().millisecondsSinceEpoch}',
        role: 'system',
        content: 'Failed to process message: $e',
        timestamp: DateTime.now(),
      );
      _cancelProcessingFallback();
      // copyWith: preserve pendingTurns (F20 regression class).
      state = state.copyWith(
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
          final id =
              details['claim_id'] as String? ?? details['id'] as String? ?? '';
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

  /// Clear the visible conversation (`/clear`, and `/new` via
  /// chat_input.dart) WITHOUT forgetting work that is still in flight.
  ///
  /// TUI parity (bughunt wave L2): the TUI's ClearConversation empties
  /// `messages` and the session message caches and nothing else — it never
  /// drops pending turns, so a `/clear` mid-turn still shows the turn's row
  /// and its reply still lands. This handler used `state = const ChatState()`,
  /// a fresh state that silently wiped `pendingTurns`, `isAgentProcessing`,
  /// `currentProgress` and the late-failure badge: the GUI lost track of the
  /// in-flight turn, its terminal could no longer resolve it (it would be
  /// buffered as "untracked" and never drained), and the indicator vanished
  /// mid-turn.
  ///
  /// So a clear keeps the parts of the state that describe live work and
  /// resets only the transcript plus the error slot. Liveness timers keep
  /// running for the retained turns, and any terminal already buffered for a
  /// tracked turn is drained by [_drainEarlyTerminals] on its arrival.
  void clearMessages() {
    state = state.copyWith(
      messages: const [],
      error: null,
      // Retained deliberately: pendingTurns (TUI parity), isAgentProcessing +
      // thinkingStartedAt (the turn is still running), currentProgress, and
      // lateFailureCount (a badge the user has not acknowledged yet).
    );
    _drainEarlyTerminals();
  }

  @override
  void dispose() {
    _disposed = true;
    _sendingTimeoutTimer?.cancel();
    _sendingTimeoutTimer = null;
    _cancelProcessingFallback();
    _wsChatSubscription?.cancel();
    _wsChatSubscription = null;
    _progressSubscription?.cancel();
    _progressSubscription = null;
    _turnTerminalSubscription?.cancel();
    _turnTerminalSubscription = null;
    for (final timer in _livenessTimers.values) {
      timer.cancel();
    }
    _livenessTimers.clear();
    // Release ALL THREE per-session holds (chat, agent-progress,
    // turn-terminal). The daemon scopes an opt-out per channel, so releasing
    // only the chat one left the session armed for progress/terminal for the
    // lifetime of the connection; releasing only one also leaks a grant
    // against a channel-blind daemon. Each release drops exactly the hold
    // this notifier took, so two notifiers on one session stay independent.
    websocket.unsubscribeFromChat(sessionId);
    websocket.unsubscribeFromAgentProgress(sessionId);
    websocket.unsubscribeFromTurnTerminal(sessionId);
    super.dispose();
  }
}

/// Chat provider — keyed by sessionId so each session has its own
/// isolated ChatNotifier with its own state, WS subscription, and
/// progress subscription. This eliminates the cross-session race
/// condition where responses from one session appeared in another.
final chatProvider =
    StateNotifierProvider.family<ChatNotifier, ChatState, String>((
      ref,
      sessionId,
    ) {
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
