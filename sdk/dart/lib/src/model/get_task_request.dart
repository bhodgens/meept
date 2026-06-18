//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'get_task_request.g.dart';

/// GetTaskRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class GetTaskRequest implements Built<GetTaskRequest, GetTaskRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  GetTaskRequest._();

  factory GetTaskRequest([void updates(GetTaskRequestBuilder b)]) = _$GetTaskRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GetTaskRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GetTaskRequest> get serializer => _$GetTaskRequestSerializer();
}

class _$GetTaskRequestSerializer implements PrimitiveSerializer<GetTaskRequest> {
  @override
  final Iterable<Type> types = const [GetTaskRequest, _$GetTaskRequest];

  @override
  final String wireName = r'GetTaskRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GetTaskRequest object, {
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
    GetTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GetTaskRequestBuilder result,
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
  GetTaskRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GetTaskRequestBuilder();
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

