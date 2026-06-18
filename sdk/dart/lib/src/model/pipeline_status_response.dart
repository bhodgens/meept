//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'pipeline_status_response.g.dart';

/// PipelineStatusResponse
///
/// Properties:
/// * [pipelineId] 
/// * [name] 
/// * [status] 
/// * [steps] 
/// * [createdAt] 
/// * [updatedAt] 
@BuiltValue()
abstract class PipelineStatusResponse implements Built<PipelineStatusResponse, PipelineStatusResponseBuilder> {
  @BuiltValueField(wireName: r'pipeline_id')
  String get pipelineId;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'status')
  String get status;

  @BuiltValueField(wireName: r'steps')
  BuiltList<String>? get steps;

  @BuiltValueField(wireName: r'created_at')
  String get createdAt;

  @BuiltValueField(wireName: r'updated_at')
  String get updatedAt;

  PipelineStatusResponse._();

  factory PipelineStatusResponse([void updates(PipelineStatusResponseBuilder b)]) = _$PipelineStatusResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PipelineStatusResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PipelineStatusResponse> get serializer => _$PipelineStatusResponseSerializer();
}

class _$PipelineStatusResponseSerializer implements PrimitiveSerializer<PipelineStatusResponse> {
  @override
  final Iterable<Type> types = const [PipelineStatusResponse, _$PipelineStatusResponse];

  @override
  final String wireName = r'PipelineStatusResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PipelineStatusResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'pipeline_id';
    yield serializers.serialize(
      object.pipelineId,
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
    yield r'steps';
    yield object.steps == null ? null : serializers.serialize(
      object.steps,
      specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
    );
    yield r'created_at';
    yield serializers.serialize(
      object.createdAt,
      specifiedType: const FullType(String),
    );
    yield r'updated_at';
    yield serializers.serialize(
      object.updatedAt,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    PipelineStatusResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PipelineStatusResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'pipeline_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.pipelineId = valueDes;
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
        case r'steps':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.steps.replace(valueDes);
          break;
        case r'created_at':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.createdAt = valueDes;
          break;
        case r'updated_at':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.updatedAt = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PipelineStatusResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PipelineStatusResponseBuilder();
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

