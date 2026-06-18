//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'cancel_task_request.g.dart';

/// CancelTaskRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class CancelTaskRequest implements Built<CancelTaskRequest, CancelTaskRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  CancelTaskRequest._();

  factory CancelTaskRequest([void updates(CancelTaskRequestBuilder b)]) = _$CancelTaskRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CancelTaskRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CancelTaskRequest> get serializer => _$CancelTaskRequestSerializer();
}

class _$CancelTaskRequestSerializer implements PrimitiveSerializer<CancelTaskRequest> {
  @override
  final Iterable<Type> types = const [CancelTaskRequest, _$CancelTaskRequest];

  @override
  final String wireName = r'CancelTaskRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CancelTaskRequest object, {
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
    CancelTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CancelTaskRequestBuilder result,
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
  CancelTaskRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CancelTaskRequestBuilder();
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

