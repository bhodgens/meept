//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'update_status_request.g.dart';

/// UpdateStatusRequest
///
/// Properties:
/// * [pipelineId] 
/// * [status] 
@BuiltValue()
abstract class UpdateStatusRequest implements Built<UpdateStatusRequest, UpdateStatusRequestBuilder> {
  @BuiltValueField(wireName: r'pipeline_id')
  String get pipelineId;

  @BuiltValueField(wireName: r'status')
  String get status;

  UpdateStatusRequest._();

  factory UpdateStatusRequest([void updates(UpdateStatusRequestBuilder b)]) = _$UpdateStatusRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(UpdateStatusRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<UpdateStatusRequest> get serializer => _$UpdateStatusRequestSerializer();
}

class _$UpdateStatusRequestSerializer implements PrimitiveSerializer<UpdateStatusRequest> {
  @override
  final Iterable<Type> types = const [UpdateStatusRequest, _$UpdateStatusRequest];

  @override
  final String wireName = r'UpdateStatusRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    UpdateStatusRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'pipeline_id';
    yield serializers.serialize(
      object.pipelineId,
      specifiedType: const FullType(String),
    );
    yield r'status';
    yield serializers.serialize(
      object.status,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    UpdateStatusRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required UpdateStatusRequestBuilder result,
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
        case r'status':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.status = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  UpdateStatusRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = UpdateStatusRequestBuilder();
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

