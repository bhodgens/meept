/// Typed models for the daemon's WebSocket event surface (issue #48 Tier 3).
///
/// The WS relay (`internal/comm/http/server.go` `transformBusEventToWS`)
/// flattens bus payloads into a top-level map merged with `type`,
/// `source_topic` and `timestamp` keys. Only two shapes on that surface are
/// stable enough to type:
///
///   * `chat_message`  — normalized by the relay (session_id fallback from
///     conversation_id, reply→content copy, role/id defaults; publisher:
///     `ChatHandler.publishChatMessage` and the `chat.message.received`
///     broadcast).
///   * `agent_progress` frames carrying the frozen
///     `agent.TurnTerminalEvent` payload (`turn.terminal` topic), identified
///     by payload shape (`turn_id` AND `handler_case` present).
///
/// The remaining event types (`metrics_update`, `job_update`, `plan_update`,
/// generic `event`) carry heterogeneous map payloads and deliberately stay
/// as `Map<String, dynamic>` accessors — see schemas/ws_events.schema.json
/// for the contract this file mirrors and
/// `internal/comm/http/ws_schema_drift_test.go` for the Go-side drift gate.
///
/// Generated parts come from `json_serializable` via build_runner
/// (`dart run build_runner build --delete-conflicting-outputs`).
library;

import 'package:json_annotation/json_annotation.dart';

part 'ws_events.g.dart';

/// Wire values of the daemon's WS `type` field.
///
/// Single Dart-side mapping site — mirrors
/// `internal/comm/wsclass/wsclass.go` `WSClass.String()` exactly. Stream
/// filters must use these constants instead of raw string literals so a
/// daemon-side rename surfaces as a failed drift-gate/test, not a silent
/// empty stream.
class WsEventTypes {
  WsEventTypes._();

  static const chatMessage = 'chat_message';
  static const agentProgress = 'agent_progress';
  static const metricsUpdate = 'metrics_update';
  static const jobUpdate = 'job_update';
  static const planUpdate = 'plan_update';

  /// Generic fallback classification (the old default bucket).
  static const event = 'event';
}

/// Minimal envelope for an arbitrary (flattened) WS frame: its classified
/// [type] plus the full payload map. Typed views live in the generated
/// classes below; this base exists for untyped consumers and tests.
class WsEvent {
  final String type;
  final Map<String, dynamic> payload;

  const WsEvent({required this.type, this.payload = const {}});

  factory WsEvent.fromFlat(Map<String, dynamic> frame) {
    final type = frame['type'];
    return WsEvent(type: type is String ? type : '', payload: frame);
  }
}

/// `type: "chat_message"` frame after the relay's normalization.
///
/// Mirrors the relay guarantees in `transformBusEventToWS` (server.go):
/// `session_id` is present (explicit publisher value or the
/// conversation_id fallback), `content` mirrors the publisher's `reply`,
/// `role` defaults to `assistant`, `id` defaults to the bus message ID.
/// The `chat.message.received` broadcast also classifies into this shape
/// and carries `source_client`.
@JsonSerializable()
class ChatMessageEvent {
  @JsonKey(name: 'session_id', defaultValue: '')
  final String sessionId;

  @JsonKey(name: 'conversation_id', includeIfNull: false, defaultValue: '')
  final String conversationId;

  @JsonKey(defaultValue: 'assistant')
  final String role;

  @JsonKey(defaultValue: '')
  final String content;

  @JsonKey(defaultValue: '')
  final String error;

  @JsonKey(name: 'source_client', includeIfNull: false, defaultValue: '')
  final String sourceClient;

  @JsonKey(defaultValue: '')
  final String id;

  /// RFC3339 timestamp string as injected by the relay (never parsed here —
  /// wire fidelity over convenience).
  final String? timestamp;

  @JsonKey(name: 'source_topic', defaultValue: '')
  final String sourceTopic;

  @JsonKey(defaultValue: '')
  final String type;

  const ChatMessageEvent({
    this.sessionId = '',
    this.conversationId = '',
    this.role = 'assistant',
    this.content = '',
    this.error = '',
    this.sourceClient = '',
    this.id = '',
    this.timestamp,
    this.sourceTopic = '',
    this.type = WsEventTypes.chatMessage,
  });

  factory ChatMessageEvent.fromJson(Map<String, dynamic> json) =>
      _$ChatMessageEventFromJson(json);

  factory ChatMessageEvent.fromParse(Map<String, dynamic> json) =>
      ChatMessageEvent.fromJson(json);

  Map<String, dynamic> toJson() => _$ChatMessageEventToJson(this);
}

/// Parsed `turn.terminal` relay (async-turn-migration leaf 05; issue #48
/// Tier 3 generated-model migration).
///
/// Arrives as a WS frame classified `agent_progress` whose payload is the
/// frozen `agent.TurnTerminalEvent` (internal/agent/handler.go — CLOSED
/// field set). Field keys mirror the Go struct's json tags exactly; the Go
/// drift test (`internal/comm/http/ws_schema_drift_test.go`) fails when the
/// struct and schemas/ws_events.schema.json diverge, and the Dart golden
/// test (test/models/ws_events_test.dart) pins the round-trip.
@JsonSerializable()
class TurnTerminalEvent {
  /// Statuses: completed | failed | timeout | parked (CLOSED set).
  static const statusCompleted = 'completed';
  static const statusFailed = 'failed';
  static const statusTimeout = 'timeout';
  static const statusParked = 'parked';

  @JsonKey(name: 'turn_id', defaultValue: '')
  final String turnId;

  @JsonKey(name: 'conversation_id', defaultValue: '')
  final String conversationId;

  @JsonKey(name: 'session_id', includeIfNull: false, defaultValue: '')
  final String sessionId;

  @JsonKey(defaultValue: '')
  final String status;

  @JsonKey(defaultValue: '')
  final String reply;

  @JsonKey(defaultValue: '')
  final String error;

  @JsonKey(name: 'duration_ms', defaultValue: 0)
  final int durationMs;

  @JsonKey(name: 'handler_case', defaultValue: '')
  final String handlerCase;

  @JsonKey(name: 'task_id', includeIfNull: false, defaultValue: '')
  final String taskId;

  @JsonKey(name: 'intent_type', includeIfNull: false, defaultValue: '')
  final String intentType;

  @JsonKey(name: 'agent_id', includeIfNull: false, defaultValue: '')
  final String agentId;

  @JsonKey(name: 'classified_by', includeIfNull: false, defaultValue: '')
  final String classifiedBy;

  @JsonKey(includeIfNull: false, defaultValue: '')
  final String model;

  /// Relay-injected classification (`agent_progress`) — always present on
  /// the wire frame, absent on the raw bus payload.
  @JsonKey(defaultValue: '')
  final String type;

  @JsonKey(name: 'source_topic', defaultValue: '')
  final String sourceTopic;

  /// RFC3339 timestamp injected by the relay when the payload has none.
  final String? timestamp;

  const TurnTerminalEvent({
    required this.turnId,
    required this.conversationId,
    this.sessionId = '',
    this.status = '',
    this.reply = '',
    this.error = '',
    this.durationMs = 0,
    this.handlerCase = '',
    this.taskId = '',
    this.intentType = '',
    this.agentId = '',
    this.classifiedBy = '',
    this.model = '',
    this.type = WsEventTypes.agentProgress,
    this.sourceTopic = '',
    this.timestamp,
  });

  factory TurnTerminalEvent.fromJson(Map<String, dynamic> json) =>
      _$TurnTerminalEventFromJson(json);

  /// Kept for the existing consumer API (chat_provider.dart and the service
  /// streams) — delegates to the generated parser.
  factory TurnTerminalEvent.fromParse(Map<String, dynamic> json) =>
      TurnTerminalEvent.fromJson(json);

  Map<String, dynamic> toJson() => _$TurnTerminalEventToJson(this);
}

/// Whether a (flattened) WS message map is a turn.terminal relay rather
/// than an ordinary progress event. Terminal frames are identified by
/// payload shape: `turn_id` AND `handler_case` present and non-empty.
/// Ordinary progress events (agent.progress synthesizer) carry neither.
bool isTurnTerminalPayload(Map<String, dynamic> m) {
  final turnId = m['turn_id'];
  final handlerCase = m['handler_case'];
  return turnId is String &&
      turnId.isNotEmpty &&
      handlerCase is String &&
      handlerCase.isNotEmpty;
}
