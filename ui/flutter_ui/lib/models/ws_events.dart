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

/// Parsed `bot.result` / `employee.result` relay (issue #140 / #160).
///
/// Arrives as a WS frame classified `agent_progress` whose payload is the
/// shared `agent.ExecutionResultEvent` (internal/agent/execution_events.go —
/// CLOSED field set, NO `kind` field; the topic name is the tier
/// discriminator). Field keys mirror the Go struct's json tags exactly; the
/// Go drift test (`internal/comm/http/ws_schema_drift_test.go`
/// TestWSExecutionResultDrift) fails when the struct and
/// schemas/ws_events.schema.json diverge.
///
/// Hand-rolled parser (not freezed): the field set is small and the wire
/// shape is frozen; no build_runner step is required.
class ExecutionResultEvent {
  static const statusCompleted = 'completed';
  static const statusFailed = 'failed';
  static const statusSkipped = 'skipped';

  /// Relay-injected source topics. The tier is read from this, never from
  /// the payload (there is no in-band kind key by design).
  static const sourceTopicBot = 'bot.result';
  static const sourceTopicEmployee = 'employee.result';

  final String agentId;
  final String status;
  final String output;
  final int tokensUsed;
  final String error;
  final int durationMs;
  final String stopReason;
  final String triggerRef;
  final String sessionId;

  /// Relay-injected classification (`agent_progress`) — always present on
  /// the wire frame, absent on the raw bus payload.
  final String type;

  /// Relay-injected source topic (`bot.result` or `employee.result`).
  final String sourceTopic;

  /// RFC3339 timestamp (relay-injected when the payload carries none).
  final DateTime? timestamp;

  /// Persisted-row fields (GET /api/v1/executions, issue #160 follow-up).
  /// The store carries an explicit `tier` and `created_at`; live WS frames
  /// leave these at their defaults and derive tier from sourceTopic.
  final int id;
  final String storedTier;
  final DateTime? createdAt;

  const ExecutionResultEvent({
    required this.agentId,
    required this.status,
    this.output = '',
    this.tokensUsed = 0,
    this.error = '',
    this.durationMs = 0,
    this.stopReason = '',
    this.triggerRef = '',
    this.sessionId = '',
    this.type = WsEventTypes.agentProgress,
    this.sourceTopic = '',
    this.timestamp,
    this.id = 0,
    this.storedTier = '',
    this.createdAt,
  });

  /// Tier discriminator: persisted rows carry an explicit `tier`; live WS
  /// frames derive it from the relay source topic. '' when neither exists.
  String get tier {
    if (storedTier.isNotEmpty) return storedTier;
    switch (sourceTopic) {
      case sourceTopicBot:
        return 'bot';
      case sourceTopicEmployee:
        return 'employee';
      default:
        return '';
    }
  }

  /// Best-known event time: persisted created_at, else relay timestamp.
  DateTime? get effectiveTime => createdAt ?? timestamp;

  bool get isBot => tier == 'bot';
  bool get isEmployee => tier == 'employee';

  factory ExecutionResultEvent.fromJson(Map<String, dynamic> json) {
    int coerceInt(dynamic v) {
      if (v is num) return v.toInt();
      if (v is String) return int.tryParse(v) ?? 0;
      return 0;
    }

    return ExecutionResultEvent(
      agentId: (json['agent_id'] as String?) ?? '',
      status: (json['status'] as String?) ?? '',
      output: (json['output'] as String?) ?? '',
      tokensUsed: coerceInt(json['tokens_used']),
      error: (json['error'] as String?) ?? '',
      durationMs: coerceInt(json['duration_ms']),
      stopReason: (json['stop_reason'] as String?) ?? '',
      triggerRef: (json['trigger_ref'] as String?) ?? '',
      sessionId: (json['session_id'] as String?) ?? '',
      type: (json['type'] as String?) ?? WsEventTypes.agentProgress,
      sourceTopic: (json['source_topic'] as String?) ?? '',
      timestamp: DateTime.tryParse((json['timestamp'] as String?) ?? ''),
      id: coerceInt(json['id']),
      storedTier: (json['tier'] as String?) ?? '',
      createdAt: DateTime.tryParse((json['created_at'] as String?) ?? ''),
    );
  }
}

/// Whether a (flattened) WS message map is an execution-result relay (bot or
/// employee) rather than an ordinary progress event. Identified by payload
/// shape: `agent_id` AND `status` present and non-empty, and `source_topic`
/// is `bot.result` or `employee.result` (or absent — the relay always
/// stamps it; absent is accepted defensively). A turn.terminal frame also
/// carries `agent_id`/`status`, so those payloads are excluded explicitly.
bool isExecutionResultPayload(Map<String, dynamic> m) {
  final agentId = m['agent_id'];
  final status = m['status'];
  if (agentId is! String ||
      agentId.isEmpty ||
      status is! String ||
      status.isEmpty) {
    return false;
  }
  if (isTurnTerminalPayload(m)) return false;
  final topic = m['source_topic'];
  if (topic == null) return true;
  if (topic is! String) return false;
  return topic == ExecutionResultEvent.sourceTopicBot ||
      topic == ExecutionResultEvent.sourceTopicEmployee;
}
