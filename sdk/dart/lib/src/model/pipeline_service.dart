//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'pipeline_service.g.dart';

/// PipelineService
///
/// Properties:
/// * [mu] 
/// * [pipelines] 
@BuiltValue()
abstract class PipelineService implements Built<PipelineService, PipelineServiceBuilder> {
  @BuiltValueField(wireName: r'mu')
  JsonObject? get mu;

  @BuiltValueField(wireName: r'pipelines')
  String? get pipelines;

  PipelineService._();

  factory PipelineService([void updates(PipelineServiceBuilder b)]) = _$PipelineService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PipelineServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PipelineService> get serializer => _$PipelineServiceSerializer();
}

class _$PipelineServiceSerializer implements PrimitiveSerializer<PipelineService> {
  @override
  final Iterable<Type> types = const [PipelineService, _$PipelineService];

  @override
  final String wireName = r'PipelineService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PipelineService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.mu != null) {
      yield r'mu';
      yield serializers.serialize(
        object.mu,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.pipelines != null) {
      yield r'pipelines';
      yield serializers.serialize(
        object.pipelines,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PipelineService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PipelineServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'mu':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.mu = valueDes;
          break;
        case r'pipelines':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.pipelines = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PipelineService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PipelineServiceBuilder();
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

