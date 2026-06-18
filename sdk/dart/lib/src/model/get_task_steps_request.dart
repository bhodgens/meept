//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'get_task_steps_request.g.dart';

/// GetTaskStepsRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class GetTaskStepsRequest implements Built<GetTaskStepsRequest, GetTaskStepsRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  GetTaskStepsRequest._();

  factory GetTaskStepsRequest([void updates(GetTaskStepsRequestBuilder b)]) = _$GetTaskStepsRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GetTaskStepsRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GetTaskStepsRequest> get serializer => _$GetTaskStepsRequestSerializer();
}

class _$GetTaskStepsRequestSerializer implements PrimitiveSerializer<GetTaskStepsRequest> {
  @override
  final Iterable<Type> types = const [GetTaskStepsRequest, _$GetTaskStepsRequest];

  @override
  final String wireName = r'GetTaskStepsRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GetTaskStepsRequest object, {
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
    GetTaskStepsRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GetTaskStepsRequestBuilder result,
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
  GetTaskStepsRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GetTaskStepsRequestBuilder();
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

