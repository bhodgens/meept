//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'remove_job_request.g.dart';

/// RemoveJobRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class RemoveJobRequest implements Built<RemoveJobRequest, RemoveJobRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  RemoveJobRequest._();

  factory RemoveJobRequest([void updates(RemoveJobRequestBuilder b)]) = _$RemoveJobRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RemoveJobRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RemoveJobRequest> get serializer => _$RemoveJobRequestSerializer();
}

class _$RemoveJobRequestSerializer implements PrimitiveSerializer<RemoveJobRequest> {
  @override
  final Iterable<Type> types = const [RemoveJobRequest, _$RemoveJobRequest];

  @override
  final String wireName = r'RemoveJobRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RemoveJobRequest object, {
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
    RemoveJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RemoveJobRequestBuilder result,
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
  RemoveJobRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RemoveJobRequestBuilder();
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

