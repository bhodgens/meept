//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'add_worker_request.g.dart';

/// AddWorkerRequest
///
/// Properties:
/// * [id] 
/// * [capabilities] 
@BuiltValue()
abstract class AddWorkerRequest implements Built<AddWorkerRequest, AddWorkerRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'capabilities')
  String? get capabilities;

  AddWorkerRequest._();

  factory AddWorkerRequest([void updates(AddWorkerRequestBuilder b)]) = _$AddWorkerRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AddWorkerRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AddWorkerRequest> get serializer => _$AddWorkerRequestSerializer();
}

class _$AddWorkerRequestSerializer implements PrimitiveSerializer<AddWorkerRequest> {
  @override
  final Iterable<Type> types = const [AddWorkerRequest, _$AddWorkerRequest];

  @override
  final String wireName = r'AddWorkerRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AddWorkerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'capabilities';
    yield object.capabilities == null ? null : serializers.serialize(
      object.capabilities,
      specifiedType: const FullType.nullable(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    AddWorkerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AddWorkerRequestBuilder result,
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
        case r'capabilities':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.capabilities = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AddWorkerRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AddWorkerRequestBuilder();
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

