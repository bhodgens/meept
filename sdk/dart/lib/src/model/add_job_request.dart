//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'add_job_request.g.dart';

/// AddJobRequest
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [schedule] 
/// * [type] 
/// * [agentConfig] 
/// * [shellConfig] 
/// * [enabled] 
@BuiltValue()
abstract class AddJobRequest implements Built<AddJobRequest, AddJobRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'schedule')
  String get schedule;

  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'agent_config')
  JsonObject? get agentConfig;

  @BuiltValueField(wireName: r'shell_config')
  JsonObject? get shellConfig;

  @BuiltValueField(wireName: r'enabled')
  bool? get enabled;

  AddJobRequest._();

  factory AddJobRequest([void updates(AddJobRequestBuilder b)]) = _$AddJobRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AddJobRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AddJobRequest> get serializer => _$AddJobRequestSerializer();
}

class _$AddJobRequestSerializer implements PrimitiveSerializer<AddJobRequest> {
  @override
  final Iterable<Type> types = const [AddJobRequest, _$AddJobRequest];

  @override
  final String wireName = r'AddJobRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AddJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    yield r'schedule';
    yield serializers.serialize(
      object.schedule,
      specifiedType: const FullType(String),
    );
    yield r'type';
    yield serializers.serialize(
      object.type,
      specifiedType: const FullType(String),
    );
    if (object.agentConfig != null) {
      yield r'agent_config';
      yield serializers.serialize(
        object.agentConfig,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.shellConfig != null) {
      yield r'shell_config';
      yield serializers.serialize(
        object.shellConfig,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.enabled != null) {
      yield r'enabled';
      yield serializers.serialize(
        object.enabled,
        specifiedType: const FullType(bool),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    AddJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AddJobRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.name = valueDes;
          break;
        case r'schedule':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.schedule = valueDes;
          break;
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.type = valueDes;
          break;
        case r'agent_config':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.agentConfig = valueDes;
          break;
        case r'shell_config':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.shellConfig = valueDes;
          break;
        case r'enabled':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.enabled = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AddJobRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AddJobRequestBuilder();
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

