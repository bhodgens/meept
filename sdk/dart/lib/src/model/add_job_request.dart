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
/// * [agentConfigCommaOmitempty] 
/// * [shellConfigCommaOmitempty] 
/// * [enabledCommaOmitempty] 
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

  @BuiltValueField(wireName: r'agent_config,omitempty')
  JsonObject? get agentConfigCommaOmitempty;

  @BuiltValueField(wireName: r'shell_config,omitempty')
  JsonObject? get shellConfigCommaOmitempty;

  @BuiltValueField(wireName: r'enabled,omitempty')
  bool? get enabledCommaOmitempty;

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
    if (object.agentConfigCommaOmitempty != null) {
      yield r'agent_config,omitempty';
      yield serializers.serialize(
        object.agentConfigCommaOmitempty,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.shellConfigCommaOmitempty != null) {
      yield r'shell_config,omitempty';
      yield serializers.serialize(
        object.shellConfigCommaOmitempty,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.enabledCommaOmitempty != null) {
      yield r'enabled,omitempty';
      yield serializers.serialize(
        object.enabledCommaOmitempty,
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
        case r'agent_config,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.agentConfigCommaOmitempty = valueDes;
          break;
        case r'shell_config,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.shellConfigCommaOmitempty = valueDes;
          break;
        case r'enabled,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.enabledCommaOmitempty = valueDes;
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

