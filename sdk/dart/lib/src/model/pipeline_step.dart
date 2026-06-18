//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'pipeline_step.g.dart';

/// PipelineStep
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [status] 
/// * [errorCommaOmitempty] 
/// * [startedAtCommaOmitempty] 
/// * [endedAtCommaOmitempty] 
@BuiltValue()
abstract class PipelineStep implements Built<PipelineStep, PipelineStepBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'status')
  String get status;

  @BuiltValueField(wireName: r'error,omitempty')
  String? get errorCommaOmitempty;

  @BuiltValueField(wireName: r'started_at,omitempty')
  String? get startedAtCommaOmitempty;

  @BuiltValueField(wireName: r'ended_at,omitempty')
  String? get endedAtCommaOmitempty;

  PipelineStep._();

  factory PipelineStep([void updates(PipelineStepBuilder b)]) = _$PipelineStep;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PipelineStepBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PipelineStep> get serializer => _$PipelineStepSerializer();
}

class _$PipelineStepSerializer implements PrimitiveSerializer<PipelineStep> {
  @override
  final Iterable<Type> types = const [PipelineStep, _$PipelineStep];

  @override
  final String wireName = r'PipelineStep';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PipelineStep object, {
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
    if (object.errorCommaOmitempty != null) {
      yield r'error,omitempty';
      yield serializers.serialize(
        object.errorCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.startedAtCommaOmitempty != null) {
      yield r'started_at,omitempty';
      yield serializers.serialize(
        object.startedAtCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.endedAtCommaOmitempty != null) {
      yield r'ended_at,omitempty';
      yield serializers.serialize(
        object.endedAtCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PipelineStep object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PipelineStepBuilder result,
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
        case r'error,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.errorCommaOmitempty = valueDes;
          break;
        case r'started_at,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.startedAtCommaOmitempty = valueDes;
          break;
        case r'ended_at,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.endedAtCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PipelineStep deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PipelineStepBuilder();
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

