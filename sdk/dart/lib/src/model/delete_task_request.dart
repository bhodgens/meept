//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'delete_task_request.g.dart';

/// DeleteTaskRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class DeleteTaskRequest implements Built<DeleteTaskRequest, DeleteTaskRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  DeleteTaskRequest._();

  factory DeleteTaskRequest([void updates(DeleteTaskRequestBuilder b)]) = _$DeleteTaskRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(DeleteTaskRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<DeleteTaskRequest> get serializer => _$DeleteTaskRequestSerializer();
}

class _$DeleteTaskRequestSerializer implements PrimitiveSerializer<DeleteTaskRequest> {
  @override
  final Iterable<Type> types = const [DeleteTaskRequest, _$DeleteTaskRequest];

  @override
  final String wireName = r'DeleteTaskRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    DeleteTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    DeleteTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required DeleteTaskRequestBuilder result,
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
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  DeleteTaskRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = DeleteTaskRequestBuilder();
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

