//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'remove_worker_request.g.dart';

/// RemoveWorkerRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class RemoveWorkerRequest implements Built<RemoveWorkerRequest, RemoveWorkerRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  RemoveWorkerRequest._();

  factory RemoveWorkerRequest([void updates(RemoveWorkerRequestBuilder b)]) = _$RemoveWorkerRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RemoveWorkerRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RemoveWorkerRequest> get serializer => _$RemoveWorkerRequestSerializer();
}

class _$RemoveWorkerRequestSerializer implements PrimitiveSerializer<RemoveWorkerRequest> {
  @override
  final Iterable<Type> types = const [RemoveWorkerRequest, _$RemoveWorkerRequest];

  @override
  final String wireName = r'RemoveWorkerRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RemoveWorkerRequest object, {
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
    RemoveWorkerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RemoveWorkerRequestBuilder result,
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
  RemoveWorkerRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RemoveWorkerRequestBuilder();
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

