// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'ws_events.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

ChatMessageEvent _$ChatMessageEventFromJson(Map<String, dynamic> json) =>
    ChatMessageEvent(
      sessionId: json['session_id'] as String? ?? '',
      conversationId: json['conversation_id'] as String? ?? '',
      role: json['role'] as String? ?? 'assistant',
      content: json['content'] as String? ?? '',
      error: json['error'] as String? ?? '',
      sourceClient: json['source_client'] as String? ?? '',
      id: json['id'] as String? ?? '',
      timestamp: json['timestamp'] as String?,
      sourceTopic: json['source_topic'] as String? ?? '',
      type: json['type'] as String? ?? '',
    );

Map<String, dynamic> _$ChatMessageEventToJson(ChatMessageEvent instance) =>
    <String, dynamic>{
      'session_id': instance.sessionId,
      'conversation_id': instance.conversationId,
      'role': instance.role,
      'content': instance.content,
      'error': instance.error,
      'source_client': instance.sourceClient,
      'id': instance.id,
      'timestamp': instance.timestamp,
      'source_topic': instance.sourceTopic,
      'type': instance.type,
    };

TurnTerminalEvent _$TurnTerminalEventFromJson(Map<String, dynamic> json) =>
    TurnTerminalEvent(
      turnId: json['turn_id'] as String? ?? '',
      conversationId: json['conversation_id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      status: json['status'] as String? ?? '',
      reply: json['reply'] as String? ?? '',
      error: json['error'] as String? ?? '',
      durationMs: (json['duration_ms'] as num?)?.toInt() ?? 0,
      handlerCase: json['handler_case'] as String? ?? '',
      taskId: json['task_id'] as String? ?? '',
      intentType: json['intent_type'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      classifiedBy: json['classified_by'] as String? ?? '',
      model: json['model'] as String? ?? '',
      type: json['type'] as String? ?? '',
      sourceTopic: json['source_topic'] as String? ?? '',
      timestamp: json['timestamp'] as String?,
    );

Map<String, dynamic> _$TurnTerminalEventToJson(TurnTerminalEvent instance) =>
    <String, dynamic>{
      'turn_id': instance.turnId,
      'conversation_id': instance.conversationId,
      'session_id': instance.sessionId,
      'status': instance.status,
      'reply': instance.reply,
      'error': instance.error,
      'duration_ms': instance.durationMs,
      'handler_case': instance.handlerCase,
      'task_id': instance.taskId,
      'intent_type': instance.intentType,
      'agent_id': instance.agentId,
      'classified_by': instance.classifiedBy,
      'model': instance.model,
      'type': instance.type,
      'source_topic': instance.sourceTopic,
      'timestamp': instance.timestamp,
    };
