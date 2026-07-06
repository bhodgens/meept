//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'pipeline_step_status.g.dart';

/// PipelineStepStatus
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [status] 
/// * [error] 
/// * [startedAt] 
/// * [endedAt] 
@BuiltValue()
abstract class PipelineStepStatus implements Built<PipelineStepStatus, PipelineStepStatusBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'status')
  String get status;

  @BuiltValueField(wireName: r'error')
  String? get error;

  @BuiltValueField(wireName: r'started_at')
  String? get startedAt;

  @BuiltValueField(wireName: r'ended_at')
  String? get endedAt;

  PipelineStepStatus._();

  factory PipelineStepStatus([void updates(PipelineStepStatusBuilder b)]) = _$PipelineStepStatus;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PipelineStepStatusBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PipelineStepStatus> get serializer => _$PipelineStepStatusSerializer();
}

class _$PipelineStepStatusSerializer implements PrimitiveSerializer<PipelineStepStatus> {
  @override
  final Iterable<Type> types = const [PipelineStepStatus, _$PipelineStepStatus];

  @override
  final String wireName = r'PipelineStepStatus';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PipelineStepStatus object, {
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
    yield r'status';
    yield serializers.serialize(
      object.status,
      specifiedType: const FullType(String),
    );
    if (object.error != null) {
      yield r'error';
      yield serializers.serialize(
        object.error,
        specifiedType: const FullType(String),
      );
    }
    if (object.startedAt != null) {
      yield r'started_at';
      yield serializers.serialize(
        object.startedAt,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.endedAt != null) {
      yield r'ended_at';
      yield serializers.serialize(
        object.endedAt,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PipelineStepStatus object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PipelineStepStatusBuilder result,
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
        case r'status':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.status = valueDes;
          break;
        case r'error':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.error = valueDes;
          break;
        case r'started_at':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.startedAt = valueDes;
          break;
        case r'ended_at':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.endedAt = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PipelineStepStatus deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PipelineStepStatusBuilder();
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

