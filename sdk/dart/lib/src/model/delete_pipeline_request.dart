//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'delete_pipeline_request.g.dart';

/// DeletePipelineRequest
///
/// Properties:
/// * [pipelineId] 
@BuiltValue()
abstract class DeletePipelineRequest implements Built<DeletePipelineRequest, DeletePipelineRequestBuilder> {
  @BuiltValueField(wireName: r'pipeline_id')
  String get pipelineId;

  DeletePipelineRequest._();

  factory DeletePipelineRequest([void updates(DeletePipelineRequestBuilder b)]) = _$DeletePipelineRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(DeletePipelineRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<DeletePipelineRequest> get serializer => _$DeletePipelineRequestSerializer();
}

class _$DeletePipelineRequestSerializer implements PrimitiveSerializer<DeletePipelineRequest> {
  @override
  final Iterable<Type> types = const [DeletePipelineRequest, _$DeletePipelineRequest];

  @override
  final String wireName = r'DeletePipelineRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    DeletePipelineRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'pipeline_id';
    yield serializers.serialize(
      object.pipelineId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    DeletePipelineRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required DeletePipelineRequestBuilder result,
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
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  DeletePipelineRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = DeletePipelineRequestBuilder();
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

