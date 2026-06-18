//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'vector_store_request.g.dart';

/// VectorStoreRequest
///
/// Properties:
/// * [memoryId] 
/// * [content] 
/// * [metadataCommaOmitempty] 
@BuiltValue()
abstract class VectorStoreRequest implements Built<VectorStoreRequest, VectorStoreRequestBuilder> {
  @BuiltValueField(wireName: r'memory_id')
  String get memoryId;

  @BuiltValueField(wireName: r'content')
  String get content;

  @BuiltValueField(wireName: r'metadata,omitempty')
  String? get metadataCommaOmitempty;

  VectorStoreRequest._();

  factory VectorStoreRequest([void updates(VectorStoreRequestBuilder b)]) = _$VectorStoreRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(VectorStoreRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<VectorStoreRequest> get serializer => _$VectorStoreRequestSerializer();
}

class _$VectorStoreRequestSerializer implements PrimitiveSerializer<VectorStoreRequest> {
  @override
  final Iterable<Type> types = const [VectorStoreRequest, _$VectorStoreRequest];

  @override
  final String wireName = r'VectorStoreRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    VectorStoreRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'memory_id';
    yield serializers.serialize(
      object.memoryId,
      specifiedType: const FullType(String),
    );
    yield r'content';
    yield serializers.serialize(
      object.content,
      specifiedType: const FullType(String),
    );
    if (object.metadataCommaOmitempty != null) {
      yield r'metadata,omitempty';
      yield serializers.serialize(
        object.metadataCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    VectorStoreRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required VectorStoreRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'memory_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.memoryId = valueDes;
          break;
        case r'content':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.content = valueDes;
          break;
        case r'metadata,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.metadataCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  VectorStoreRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = VectorStoreRequestBuilder();
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

