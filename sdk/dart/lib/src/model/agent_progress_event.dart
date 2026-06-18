//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'agent_progress_event.g.dart';

/// AgentProgressEvent
///
/// Properties:
/// * [type] 
/// * [sessionId] 
/// * [agentId] 
/// * [message] 
/// * [tier] 
/// * [sourceEvent] 
/// * [timestamp] 
@BuiltValue()
abstract class AgentProgressEvent implements Built<AgentProgressEvent, AgentProgressEventBuilder> {
  @BuiltValueField(wireName: r'type')
  AgentProgressEventTypeEnum? get type;
  // enum typeEnum {  agent_progress,  };

  @BuiltValueField(wireName: r'session_id')
  String? get sessionId;

  @BuiltValueField(wireName: r'agent_id')
  String? get agentId;

  @BuiltValueField(wireName: r'message')
  String? get message;

  @BuiltValueField(wireName: r'tier')
  int? get tier;

  @BuiltValueField(wireName: r'source_event')
  String? get sourceEvent;

  @BuiltValueField(wireName: r'timestamp')
  DateTime? get timestamp;

  AgentProgressEvent._();

  factory AgentProgressEvent([void updates(AgentProgressEventBuilder b)]) = _$AgentProgressEvent;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AgentProgressEventBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AgentProgressEvent> get serializer => _$AgentProgressEventSerializer();
}

class _$AgentProgressEventSerializer implements PrimitiveSerializer<AgentProgressEvent> {
  @override
  final Iterable<Type> types = const [AgentProgressEvent, _$AgentProgressEvent];

  @override
  final String wireName = r'AgentProgressEvent';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AgentProgressEvent object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.type != null) {
      yield r'type';
      yield serializers.serialize(
        object.type,
        specifiedType: const FullType(AgentProgressEventTypeEnum),
      );
    }
    if (object.sessionId != null) {
      yield r'session_id';
      yield serializers.serialize(
        object.sessionId,
        specifiedType: const FullType(String),
      );
    }
    if (object.agentId != null) {
      yield r'agent_id';
      yield serializers.serialize(
        object.agentId,
        specifiedType: const FullType(String),
      );
    }
    if (object.message != null) {
      yield r'message';
      yield serializers.serialize(
        object.message,
        specifiedType: const FullType(String),
      );
    }
    if (object.tier != null) {
      yield r'tier';
      yield serializers.serialize(
        object.tier,
        specifiedType: const FullType(int),
      );
    }
    if (object.sourceEvent != null) {
      yield r'source_event';
      yield serializers.serialize(
        object.sourceEvent,
        specifiedType: const FullType(String),
      );
    }
    if (object.timestamp != null) {
      yield r'timestamp';
      yield serializers.serialize(
        object.timestamp,
        specifiedType: const FullType(DateTime),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    AgentProgressEvent object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AgentProgressEventBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(AgentProgressEventTypeEnum),
          ) as AgentProgressEventTypeEnum;
          result.type = valueDes;
          break;
        case r'session_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionId = valueDes;
          break;
        case r'agent_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.agentId = valueDes;
          break;
        case r'message':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.message = valueDes;
          break;
        case r'tier':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.tier = valueDes;
          break;
        case r'source_event':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sourceEvent = valueDes;
          break;
        case r'timestamp':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(DateTime),
          ) as DateTime;
          result.timestamp = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AgentProgressEvent deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AgentProgressEventBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

class AgentProgressEventTypeEnum extends EnumClass {

  @BuiltValueEnumConst(wireName: r'agent_progress')
  static const AgentProgressEventTypeEnum agentProgress = _$agentProgressEventTypeEnum_agentProgress;

  static Serializer<AgentProgressEventTypeEnum> get serializer => _$agentProgressEventTypeEnumSerializer;

  const AgentProgressEventTypeEnum._(String name): super(name);

  static BuiltSet<AgentProgressEventTypeEnum> get values => _$agentProgressEventTypeEnumValues;
  static AgentProgressEventTypeEnum valueOf(String name) => _$agentProgressEventTypeEnumValueOf(name);
}

